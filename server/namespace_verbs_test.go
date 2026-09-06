package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/server/auth"
	"go.uber.org/zap"
)

// A park with two namespaces, both defined: pond holding attestations and
// playground holding none. What a drain moves between.
func park(t *testing.T) (*QNTXServer, *fakeNamespaces, *fakeOpener) {
	t.Helper()
	defined := func(name string) storage.Namespace {
		return storage.Namespace{
			Name: name,
			Definition: &storage.NamespaceDefinition{
				Owner:     "https://mastodon.example/@tim",
				Enabled:   true,
				CreatedAt: "2026-09-01T09:00:00Z",
			},
			Kinds: []string{},
		}
	}
	namespaces := &fakeNamespaces{held: []storage.Namespace{defined("playground"), defined("pond")}}
	opener := &fakeOpener{stores: map[string]*countingStore{}}
	s := &QNTXServer{
		namespaces:      namespaces,
		namespaceOpener: opener,
		atsStore:        &countingStore{},
		systemStore:     &countingStore{},
		logger:          zap.NewNop().Sugar(),
	}
	return s, namespaces, opener
}

// said is an attestation as it goes into a namespace before any drain.
func said(id, subject string) *types.As {
	when := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	return &types.As{
		ID:         id,
		Subjects:   []string{subject},
		Predicates: []string{"noticed"},
		Contexts:   []string{"pond"},
		Actors:     []string{"did:key:tim"},
		Timestamp:  when,
		Source:     "cli",
		Attributes: map[string]any{"detail": "a duck"},
		CreatedAt:  when,
	}
}

// fill puts attestations into a namespace's store the way anything else would.
func fill(t *testing.T, s *QNTXServer, namespace string, held ...*types.As) {
	t.Helper()
	store, err := s.openedIn(namespace)
	if err != nil {
		t.Fatalf("could not open %s: %v", namespace, err)
	}
	for _, as := range held {
		if err := store.CreateAttestation(as); err != nil {
			t.Fatalf("could not write %s into %s: %v", as.ID, namespace, err)
		}
	}
}

func drainRequest(t *testing.T, name, into string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/namespaces/"+name+"/drain",
		strings.NewReader(`{"into":"`+into+`"}`))
	return admittedAt(req, auth.LevelSuper)
}

func deleteRequest(t *testing.T, name string) *http.Request {
	t.Helper()
	return admittedAt(httptest.NewRequest(http.MethodDelete, "/api/namespaces/"+name, nil), auth.LevelSuper)
}

func drained(t *testing.T, w *httptest.ResponseRecorder) drainedNamespaceResponse {
	t.Helper()
	var said drainedNamespaceResponse
	if err := json.Unmarshal(w.Body.Bytes(), &said); err != nil {
		t.Fatalf("the answer does not read as a drain: %v: %s", err, w.Body.String())
	}
	return said
}

// Every attestation is written into the target as it stands, with the source
// recorded on the copy so the supersession is visible on each one.
func TestDrainCopiesEveryAttestationWithTheSourceOnEachCopy(t *testing.T) {
	s, _, opener := park(t)
	fill(t, s, "pond", said("AS-ONE", "duck"), said("AS-TWO", "heron"))

	w := httptest.NewRecorder()
	s.HandleNamespaceDrain(w, drainRequest(t, "pond", "playground"))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if got := drained(t, w); got.Copied != 2 || got.Carried != 2 {
		t.Fatalf("copied %d and carried %d, want 2 and 2", got.Copied, got.Carried)
	}

	into := opener.stores["playground"].held
	if len(into) != 2 {
		t.Fatalf("playground holds %d, want 2", len(into))
	}
	for _, made := range into {
		if made.Attributes[attrDrainedFrom] != "pond" {
			t.Errorf("%s does not say where it came from: %v", made.ID, made.Attributes)
		}
		if made.Attributes[attrDrainedASUID] == "" {
			t.Errorf("%s does not say which attestation it was made from", made.ID)
		}
		if made.Attributes["detail"] != "a duck" {
			t.Errorf("%s lost the attributes it was made from: %v", made.ID, made.Attributes)
		}
	}

	// The same claim, by the same actor, at the same moment. A drain does not
	// change when something happened or who said it.
	first := into[0]
	if first.Actors[0] != "did:key:tim" || first.Predicates[0] != "noticed" {
		t.Errorf("the copy is not the same claim: %+v", first)
	}
	if !first.Timestamp.Equal(said("AS-ONE", "duck").Timestamp) {
		t.Errorf("timestamp = %v, want the one it was made from", first.Timestamp)
	}
	if first.ID == "AS-ONE" || first.ID == "AS-TWO" {
		t.Errorf("the copy kept the id it was made from: %s", first.ID)
	}
}

// Nothing left in the source. Draining supersedes rather than moves, and the
// bytes it came out of are exactly where they were.
func TestDrainLeavesTheSourceWhereItIs(t *testing.T) {
	s, _, opener := park(t)
	fill(t, s, "pond", said("AS-ONE", "duck"))

	w := httptest.NewRecorder()
	s.HandleNamespaceDrain(w, drainRequest(t, "pond", "playground"))

	if held := opener.stores["pond"].held; len(held) != 1 || held[0].ID != "AS-ONE" {
		t.Fatalf("pond holds %d attestations after a drain, want the one it always had", len(held))
	}
	if got := drained(t, w); got.Remains == "" {
		t.Error("the answer does not say what is still at the location")
	}
}

// Draining twice is allowed and copies nothing the second time. The copy
// carries the id it was made from, which is what settles it.
func TestDrainingTwiceDoesNotDuplicate(t *testing.T) {
	s, _, opener := park(t)
	fill(t, s, "pond", said("AS-ONE", "duck"), said("AS-TWO", "heron"))

	first := httptest.NewRecorder()
	s.HandleNamespaceDrain(first, drainRequest(t, "pond", "playground"))
	if first.Code != http.StatusOK {
		t.Fatalf("the first drain failed: %s", first.Body.String())
	}

	second := httptest.NewRecorder()
	s.HandleNamespaceDrain(second, drainRequest(t, "pond", "playground"))
	if second.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", second.Code, http.StatusOK, second.Body.String())
	}

	got := drained(t, second)
	if got.Copied != 0 {
		t.Errorf("the second drain copied %d, want 0", got.Copied)
	}
	if got.Carried != 2 {
		t.Errorf("carried = %d, want the 2 the first drain took across", got.Carried)
	}
	if held := len(opener.stores["playground"].held); held != 2 {
		t.Errorf("playground holds %d, want 2 — the second drain duplicated", held)
	}
}

// After a drain, nobody writes into a namespace that has been emptied, and
// nobody reads one whose attestations now live somewhere else. The refusal
// names where they went.
func TestADrainedSourceRefusesReadsAndWritesNamingTheTarget(t *testing.T) {
	s, _, _ := park(t)
	fill(t, s, "pond", said("AS-ONE", "duck"))

	w := httptest.NewRecorder()
	s.HandleNamespaceDrain(w, drainRequest(t, "pond", "playground"))
	if w.Code != http.StatusOK {
		t.Fatalf("the drain failed: %s", w.Body.String())
	}

	_, err := s.storeIn("pond")
	if err == nil {
		t.Fatal("a drained namespace still hands out a store")
	}
	if !strings.Contains(err.Error(), "playground") {
		t.Errorf("the refusal does not name where it went: %v", err)
	}
	if !strings.Contains(err.Error(), "pond") {
		t.Errorf("the refusal does not name what was asked for: %v", err)
	}
}

// Neither was created, so neither is drained (ADR-026).
func TestSystemAndDefaultCannotBeDrained(t *testing.T) {
	for _, permanent := range []string{auth.NamespaceSystem, auth.NamespaceDefault} {
		s, namespaces, _ := park(t)
		w := httptest.NewRecorder()

		s.HandleNamespaceDrain(w, drainRequest(t, permanent, "playground"))

		if w.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want %d", permanent, w.Code, http.StatusForbidden)
		}
		if namespaces.amended != "" {
			t.Errorf("%s: the store was told to supersede %q", permanent, namespaces.amended)
		}
	}
}

// A namespace drained into itself would copy every attestation beside itself
// and then refuse reads of both the copies and the originals.
func TestDrainingIntoItselfIsRefused(t *testing.T) {
	s, namespaces, _ := park(t)
	w := httptest.NewRecorder()

	s.HandleNamespaceDrain(w, drainRequest(t, "pond", "pond"))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if namespaces.amended != "" {
		t.Errorf("the store was told to supersede %q", namespaces.amended)
	}
}

// A target this node does not serve is a drain into nothing.
func TestDrainingIntoANamespaceThatIsNotThereIsRefused(t *testing.T) {
	s, _, opener := park(t)
	fill(t, s, "pond", said("AS-ONE", "duck"))
	w := httptest.NewRecorder()

	s.HandleNamespaceDrain(w, drainRequest(t, "pond", "nowhere"))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if _, opened := opener.stores["nowhere"]; opened {
		t.Error("a namespace nobody created was opened by a drain")
	}
}

// A namespace needs to be empty when deleted, and the refusal says what still
// fills it — each kind, by name and count, so the caller can go and empty it.
func TestDeletingWhatIsNotEmptyIsRefusedListingWhatFillsIt(t *testing.T) {
	for _, fills := range []struct {
		what  string
		fill  namespaceFill
		names string
	}{
		{"attestations", namespaceFill{Attestations: 3}, "3 attestations were not drained"},
		{"a door", namespaceFill{Doors: []string{"pond"}}, "1 door open onto it: pond"},
		{"a User", namespaceFill{Users: []string{"US-TIM-7K4M"}}, "1 User is registered at it: US-TIM-7K4M"},
		{"a session", namespaceFill{Sessions: 2}, "2 live sessions name it"},
		{"tokens", namespaceFill{Tokens: []string{"tk-a", "tk-b"}}, "2 tokens name it: tk-a, tk-b"},
		{"another kind", namespaceFill{Kinds: []string{"watchers"}}, "it holds watchers"},
	} {
		if fills.fill.Empty() {
			t.Errorf("%s: a namespace holding %s reads as empty", fills.what, fills.what)
		}
		said := fills.fill.Said("pond")
		if !strings.Contains(said, fills.names) {
			t.Errorf("%s: the refusal is %q, which does not say %q", fills.what, said, fills.names)
		}
		if !strings.HasPrefix(said, "pond is not empty") {
			t.Errorf("%s: the refusal does not name the namespace: %q", fills.what, said)
		}
	}
}

// The whole of what fills it, asked of the things that would know.
func TestWhatFillsItAsksEveryThingThatCouldHoldItOpen(t *testing.T) {
	s, namespaces, _ := park(t)
	namespaces.held[1].Kinds = []string{"attestations", "watchers"}
	fill(t, s, "pond", said("AS-ONE", "duck"), said("AS-TWO", "heron"))

	cfg := &appcfg.Config{}
	cfg.Auth.Door = map[string]appcfg.DoorConfig{"pond": {RPID: "pond.test"}}

	fills, err := s.whatFillsIt(cfg, namespaces.held, "pond")
	if err != nil {
		t.Fatalf("could not tell what fills pond: %v", err)
	}
	if fills.Attestations != 2 {
		t.Errorf("attestations = %d, want the 2 nothing drained", fills.Attestations)
	}
	if len(fills.Kinds) != 1 || fills.Kinds[0] != "watchers" {
		t.Errorf("kinds = %v, want the watchers a drain does not carry", fills.Kinds)
	}
	if len(fills.Doors) != 1 || fills.Doors[0] != "pond" {
		t.Errorf("doors = %v, want the one am.toml names", fills.Doors)
	}
}

// A drain records how many it carried across, and what the store holds beyond
// that number is an attestation no drain took anywhere.
func TestADrainedNamespaceHoldsNoAttestationsThatWereNotDrained(t *testing.T) {
	s, namespaces, _ := park(t)
	fill(t, s, "pond", said("AS-ONE", "duck"), said("AS-TWO", "heron"))

	w := httptest.NewRecorder()
	s.HandleNamespaceDrain(w, drainRequest(t, "pond", "playground"))
	if w.Code != http.StatusOK {
		t.Fatalf("the drain failed: %s", w.Body.String())
	}

	fills, err := s.whatFillsIt(&appcfg.Config{}, namespaces.held, "pond")
	if err != nil {
		t.Fatalf("could not tell what fills pond: %v", err)
	}
	if !fills.Empty() {
		t.Fatalf("a drained namespace is not empty: %s", fills.Said("pond"))
	}
}

// Deleting a drained and empty namespace: the handle comes off, the flush loop
// behind it stops, the name goes out of the list, and anything reaching for it
// afterwards is told it was deleted rather than that it never was.
func TestDeletingADrainedAndEmptyNamespaceTakesItOutOfService(t *testing.T) {
	s, namespaces, opener := park(t)
	fill(t, s, "pond", said("AS-ONE", "duck"))

	drainedIt := httptest.NewRecorder()
	s.HandleNamespaceDrain(drainedIt, drainRequest(t, "pond", "playground"))
	if drainedIt.Code != http.StatusOK {
		t.Fatalf("the drain failed: %s", drainedIt.Body.String())
	}

	w := httptest.NewRecorder()
	s.HandleNamespace(w, deleteRequest(t, "pond"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var gone deletedNamespaceResponse
	if err := json.Unmarshal(w.Body.Bytes(), &gone); err != nil {
		t.Fatalf("the answer does not read as a deletion: %v: %s", err, w.Body.String())
	}
	if gone.DeletedAt == "" || gone.DeletedBy != "https://mastodon.example/@tim" {
		t.Errorf("the record does not say when and by whom: %+v", gone)
	}
	// Nothing is removed at the location, and the answer says what is there.
	if gone.Remains == "" {
		t.Error("the answer does not say what prefix remains")
	}

	// The handle came off, and the flush loop behind it was told to stop. Twice
	// over the two verbs: the drain closes it so the refusal reaches requests,
	// and deleting opens it again to count what is left and closes it for good.
	if _, held := s.stores.open["pond"]; held {
		t.Error("the handle is still in the cache")
	}
	if len(opener.closed) == 0 || opener.closed[len(opener.closed)-1] != "pond" {
		t.Errorf("closed %v, want pond last — the flush loop was left running", opener.closed)
	}

	// Out of the list anybody asking what this node has is shown.
	listed := httptest.NewRecorder()
	s.HandleNamespaces(listed, admittedAt(httptest.NewRequest(http.MethodGet, "/api/namespaces", nil), auth.LevelSuper))
	if strings.Contains(listed.Body.String(), "pond") {
		t.Errorf("a deleted namespace is still listed: %s", listed.Body.String())
	}

	// And anything reaching for it is told it was deleted, not that it never
	// existed — the ns.toml saying so is still at the location.
	_, err := s.storeIn("pond")
	if err == nil {
		t.Fatal("a deleted namespace still hands out a store")
	}
	if !strings.Contains(err.Error(), "deleted") {
		t.Errorf("the refusal does not say it was deleted: %v", err)
	}
	if namespaces.superOf.DeletedAt == "" {
		t.Error("the ns.toml does not record the deletion")
	}
}

// Neither was created, so neither is deleted (ADR-026).
func TestSystemAndDefaultCannotBeDeleted(t *testing.T) {
	for _, permanent := range []string{auth.NamespaceSystem, auth.NamespaceDefault} {
		s, namespaces, _ := park(t)
		w := httptest.NewRecorder()

		s.HandleNamespace(w, deleteRequest(t, permanent))

		if w.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want %d", permanent, w.Code, http.StatusForbidden)
		}
		if namespaces.amended != "" {
			t.Errorf("%s: the store was told to supersede %q", permanent, namespaces.amended)
		}
	}
}

// Session-only, the way minting is (ADR-025). What these two do outlives the
// request, and a token that could delete the namespace it acts in is a
// credential that can remove the ground it stands on.
//
// Which levels reach the route at all is server/reach's:
// TestTheTableSaysWhoReachesTheNamespaceVerbs, which is what keeps a public
// registration out of both of them.
func TestATokenReachesNeitherVerb(t *testing.T) {
	for _, verb := range []struct {
		what string
		call func(*QNTXServer, http.ResponseWriter, *http.Request)
		req  func() *http.Request
	}{
		{"drain", (*QNTXServer).HandleNamespaceDrain, func() *http.Request {
			return httptest.NewRequest(http.MethodPost, "/api/namespaces/pond/drain",
				strings.NewReader(`{"into":"playground"}`))
		}},
		{"delete", (*QNTXServer).HandleNamespace, func() *http.Request {
			return httptest.NewRequest(http.MethodDelete, "/api/namespaces/pond", nil)
		}},
	} {
		s, namespaces, _ := park(t)
		w := httptest.NewRecorder()

		// A token at the level the line does name. What is refused here is the
		// credential and not the rung: a bearer reaches neither verb however
		// far in it is.
		admitted := auth.Admitted(auth.LevelSuper)
		admitted.Identity = "https://mastodon.example/@stranger"
		admitted.Grant = &auth.Grant{MintedBy: admitted.Identity}
		req := verb.req()
		verb.call(s, w, req.WithContext(auth.WithAdmission(req.Context(), admitted)))

		if w.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want %d: %s", verb.what, w.Code, http.StatusForbidden, w.Body.String())
		}
		if namespaces.amended != "" {
			t.Errorf("%s: the store was told to supersede %q", verb.what, namespaces.amended)
		}
	}
}

// Both verbs are written down, into system, as facts about the namespace.
func TestBothVerbsAreAttested(t *testing.T) {
	s, _, _ := park(t)
	fill(t, s, "pond", said("AS-ONE", "duck"))

	drainedIt := httptest.NewRecorder()
	s.HandleNamespaceDrain(drainedIt, drainRequest(t, "pond", "playground"))
	if drainedIt.Code != http.StatusOK {
		t.Fatalf("the drain failed: %s", drainedIt.Body.String())
	}
	deletedIt := httptest.NewRecorder()
	s.HandleNamespace(deletedIt, deleteRequest(t, "pond"))
	if deletedIt.Code != http.StatusOK {
		t.Fatalf("the delete failed: %s", deletedIt.Body.String())
	}

	system, isCounting := s.systemStore.(*countingStore)
	if !isCounting {
		t.Fatal("the system store is not the one this test put there")
	}
	written := map[string]*types.As{}
	for _, as := range system.held {
		written[as.Predicates[0]] = as
	}

	for _, predicate := range []string{PredicateNamespaceDrained, PredicateNamespaceDeleted} {
		as, recorded := written[predicate]
		if !recorded {
			t.Fatalf("%s was not written into system", predicate)
		}
		if as.Subjects[0] != "pond" {
			t.Errorf("%s is about %v, want pond", predicate, as.Subjects)
		}
		if as.Contexts[0] != auth.NamespaceSystem {
			t.Errorf("%s landed in %v, want system", predicate, as.Contexts)
		}
	}
	if into := written[PredicateNamespaceDrained].Attributes["into"]; into != "playground" {
		t.Errorf("the drain record says it went to %v, want playground", into)
	}
	if carried := written[PredicateNamespaceDrained].Attributes["carried"]; carried != 1 {
		t.Errorf("the drain record carried %v, want 1", carried)
	}
	if drainedIt := drained(t, drainedIt); !drainedIt.Attested {
		t.Errorf("the answer says the drain was not attested: %s", drainedIt.NotAttested)
	}
}

// A namespace nobody defined has no file to record a drain or a deletion on.
// Writing one would define it by the back door, so it is refused instead.
func TestANamespaceWithoutADefinitionReachesNeitherVerb(t *testing.T) {
	for _, verb := range []struct {
		what string
		call func(*QNTXServer, http.ResponseWriter, *http.Request)
		req  func(*testing.T) *http.Request
	}{
		{"drain", (*QNTXServer).HandleNamespaceDrain, func(t *testing.T) *http.Request {
			return drainRequest(t, "ducks", "playground")
		}},
		{"delete", (*QNTXServer).HandleNamespace, func(t *testing.T) *http.Request {
			return deleteRequest(t, "ducks")
		}},
	} {
		s, namespaces, _ := park(t)
		namespaces.held = append(namespaces.held,
			storage.Namespace{Name: "ducks", Kinds: []string{"attestations"}})
		w := httptest.NewRecorder()

		verb.call(s, w, verb.req(t))

		if w.Code != http.StatusConflict {
			t.Errorf("%s: status = %d, want %d: %s", verb.what, w.Code, http.StatusConflict, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "ns.toml") {
			t.Errorf("%s: the refusal does not say what is missing: %s", verb.what, w.Body.String())
		}
	}
}
