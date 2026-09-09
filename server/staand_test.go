package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/teranos/QNTX/server/namespaces"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/slug"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
)

// A stand's definition is a system attestation (ADR-035); arrivals land in the
// market it feeds. The tests keep the two apart: sys is the system store,
// markets maps each market name to its own store.
type markets struct {
	store map[string]ats.AttestationStore
}

func (m markets) List() ([]storage.Namespace, error) {
	out := make([]storage.Namespace, 0, len(m.store))
	for name := range m.store {
		out = append(out, storage.Namespace{Name: name})
	}
	return out, nil
}
func (markets) Create(string, storage.NamespaceDefinition) error { return nil }
func (m markets) OpenNamespace(name string) (*namespaces.Universe, error) {
	if s, ok := m.store[name]; ok {
		return oneNamespace(name, s), nil
	}
	return nil, fmt.Errorf("no market %q served in test", name)
}

// standServer wires a server whose system store holds definitions and whose
// named markets each hold their own arrivals.
func standServer(t *testing.T, marketNames ...string) (*QNTXServer, ats.AttestationStore, map[string]ats.AttestationStore) {
	t.Helper()
	sys, db := createTestStore(t)
	stores := map[string]ats.AttestationStore{}
	for _, n := range marketNames {
		st, _ := createTestStore(t)
		stores[n] = st
	}
	m := markets{store: stores}
	s := &QNTXServer{nodeDB: db, logger: zap.NewNop().Sugar()}
	s.held = servingOne(db, sys)
	s.held.SetSystem(oneNamespace("system", sys))
	// A stand's market is never default, so the default store standing in for
	// system here is not one an arrival can reach.
	s.held.SetKnown(m)
	s.held.SetOpener(m)
	return s, sys, stores
}

// define writes a stand's created line into system directly (ADR-035): the
// subject is the stand's key market/slug. The definition carries no attributes.
func define(t *testing.T, sys ats.AttestationStore, market, slug string, at time.Time) {
	t.Helper()
	_, err := sys.GenerateAndCreateAttestation(context.Background(), &types.AsCommand{
		Subjects:   []string{market + "/" + slug},
		Predicates: []string{staandCreated},
		Contexts:   []string{"system"},
		Actors:     []string{"root"},
		Source:     "cli",
		Timestamp:  at,
	})
	if err != nil {
		t.Fatalf("define %q: %v", slug, err)
	}
}

func undefine(t *testing.T, sys ats.AttestationStore, market, slug string, at time.Time) {
	t.Helper()
	_, err := sys.GenerateAndCreateAttestation(context.Background(), &types.AsCommand{
		Subjects:   []string{market + "/" + slug},
		Predicates: []string{staandDeleted},
		Contexts:   []string{"system"},
		Actors:     []string{"root"},
		Source:     "cli",
		Timestamp:  at,
	})
	if err != nil {
		t.Fatalf("undefine %q: %v", slug, err)
	}
}

// withDoor gives a namespace a front door with these origins, so a stand in it
// inherits that write-origin (ADR-032, ADR-035).
func withDoor(s *QNTXServer, namespace string, origins ...string) {
	s.deps = &serverDependencies{cfg: &config.Config{Auth: config.AuthConfig{
		Door: map[string]config.DoorConfig{slug.Of(namespace): {Origins: origins}},
	}}}
}

func fire(s *QNTXServer, path, referer string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	s.HandleStaand(rec, req)
	return rec
}

func arrivalsFor(t *testing.T, store ats.AttestationStore, subject string) []*types.As {
	t.Helper()
	found, err := store.GetAttestations(ats.AttestationFilter{Subjects: []string{subject}, Limit: 10})
	if err != nil {
		t.Fatalf("GetAttestations: %v", err)
	}
	return found
}

// A call to a defined stand lands one arrival in its market: the subject is the
// page the hit is about (reads aloud, CDR-010), the predicate the stand
// vocabulary plus the pixel side's event, the actor the stand, and the visitor
// id an attribute (v), never the subject.
func TestAnArrivalIsRecorded(t *testing.T) {
	s, sys, stores := standServer(t, "clean")
	define(t, sys, "clean", "boutique", time.Now())

	rec := fire(s, "/s/clean/boutique?e=contact_click&page=/deep-clean&v=VISITOR-01&method=whatsapp", "https://example.com/deep-clean")

	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/gif" {
		t.Fatalf("the pixel did not come back: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	got := arrivalsFor(t, stores["clean"], "/deep-clean")
	if len(got) != 1 {
		t.Fatalf("stored %d arrivals, want 1", len(got))
	}
	as := got[0]
	if as.Subjects[0] != "/deep-clean" {
		t.Fatalf("the subject is %v, not the page the hit is about", as.Subjects)
	}
	if as.Predicates[0] != "staand:contact_click" || as.Source != "staand" {
		t.Fatalf("stored %v from %q", as.Predicates, as.Source)
	}
	if len(as.Actors) != 1 || as.Actors[0] != "staand:boutique" {
		t.Fatalf("the actor is %v, not the stand", as.Actors)
	}
	if as.Contexts[0] != "https://example.com/deep-clean" {
		t.Fatalf("the context is %v, not the page it fired from", as.Contexts)
	}
	if as.Attributes["method"] != "whatsapp" {
		t.Fatalf("the method attribute did not survive: %v", as.Attributes)
	}
	if as.Attributes["v"] != "VISITOR-01" {
		t.Fatalf("the visitor id is not carried as v: %v", as.Attributes)
	}
	if as.Attributes[staandSlugAttr] != "boutique" {
		t.Fatalf("the arrival does not carry its slug: %v", as.Attributes)
	}
	if _, leaked := as.Attributes["e"]; leaked {
		t.Fatal("the event parameter doubled as an attribute")
	}
	if _, leaked := as.Attributes["page"]; leaked {
		t.Fatal("the page parameter doubled as an attribute")
	}
}

// The pixel side names the event; a bare hit is a page view. page_view is
// Google's word for it, so a site already firing gtag needs no translation.
func TestNoEventIsAPageView(t *testing.T) {
	s, sys, stores := standServer(t, "clean")
	define(t, sys, "clean", "boutique", time.Now())

	fire(s, "/s/clean/boutique?page=/x&v=VISITOR-01", "https://example.com/")

	got := arrivalsFor(t, stores["clean"], "/x")
	if len(got) != 1 || got[0].Predicates[0] != "staand:page_view" {
		t.Fatalf("a bare arrival recorded %v, want staand:page_view", got)
	}
}

// The definition is a system attestation, and the market holds only arrivals:
// after a create, system carries the created line and the market carries none.
func TestDefinitionLivesInSystemNotMarket(t *testing.T) {
	s, sys, stores := standServer(t, "clean")

	rec := httptest.NewRecorder()
	body := `{"market":"clean","slug":"home"}`
	s.HandleStaands(rec, httptest.NewRequest(http.MethodPost, "/api/staands", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}

	inSystem := arrivalsFor(t, sys, "clean/home")
	if len(inSystem) != 1 || inSystem[0].Predicates[0] != staandCreated {
		t.Fatalf("system holds %v, want one staand:created", inSystem)
	}
	if inMarket := arrivalsFor(t, stores["clean"], "clean/home"); len(inMarket) != 0 {
		t.Fatalf("the market holds the definition, it should not: %v", inMarket)
	}
}

// A slug no stand stands under records nothing, and still answers with the
// pixel — probing teaches nothing.
func TestAnUndefinedStandRecordsNothing(t *testing.T) {
	s, _, stores := standServer(t, "clean")

	rec := fire(s, "/s/clean/nostall?page=/x&v=VISITOR-01", "")

	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/gif" {
		t.Fatalf("answered %d %s rather than the pixel", rec.Code, rec.Header().Get("Content-Type"))
	}
	if got := arrivalsFor(t, stores["clean"], "/x"); len(got) != 0 {
		t.Fatalf("an undefined stand recorded: %v", got)
	}
}

// A deleted stand supersedes its definition, so arrivals stop.
func TestADeletedStandRecordsNothing(t *testing.T) {
	s, sys, stores := standServer(t, "clean")
	now := time.Now()
	define(t, sys, "clean", "boutique", now)
	undefine(t, sys, "clean", "boutique", now.Add(time.Second))

	fire(s, "/s/clean/boutique?page=/x&v=VISITOR-01", "https://example.com/")

	if got := arrivalsFor(t, stores["clean"], "/x"); len(got) != 0 {
		t.Fatalf("a deleted stand recorded: %v", got)
	}
}

// A stand never records into system or default: an arrival is an untrusted
// public write, and those namespaces hold the node's own records.
func TestAStandNeverWritesSystemOrDefault(t *testing.T) {
	s, sys, _ := standServer(t, "clean")
	// Even with a definition keyed under system/default, the guard refuses.
	define(t, sys, "system", "x", time.Now())
	define(t, sys, "default", "y", time.Now())

	fire(s, "/s/system/x?page=/p&v=V", "https://example.com/")
	fire(s, "/s/default/y?page=/p&v=V", "https://example.com/")

	if got := arrivalsFor(t, sys, "/p"); len(got) != 0 {
		t.Fatalf("a stand wrote into system/default: %v", got)
	}
}

// The door: a stand inherits its namespace's front door as its write-origin
// (ADR-032). A present host must match the door (or be a subdomain); a host from
// anywhere else is refused; a missing Referer is allowed, so PDFs and other
// no-origin clients still record (ADR-035).
func TestOnlyTheNamespaceDoorWrites(t *testing.T) {
	s, sys, stores := standServer(t, "clean")
	withDoor(s, "clean", "https://example.com")
	define(t, sys, "clean", "boutique", time.Now())

	fire(s, "/s/clean/boutique?page=/ok", "https://www.example.com/x") // subdomain of the door
	fire(s, "/s/clean/boutique?page=/no", "https://elsewhere.test/x")  // another origin
	fire(s, "/s/clean/boutique?page=/bare", "")                        // no Referer — allowed

	if got := arrivalsFor(t, stores["clean"], "/ok"); len(got) != 1 {
		t.Fatalf("the bound door did not write: %v", got)
	}
	if got := arrivalsFor(t, stores["clean"], "/no"); len(got) != 0 {
		t.Fatalf("another origin wrote: %v", got)
	}
	if got := arrivalsFor(t, stores["clean"], "/bare"); len(got) != 1 {
		t.Fatalf("a no-Referer arrival was refused, but it should be allowed: %v", got)
	}
}

// A stand spends only its own budget: past the burst, arrivals are dropped and
// the drop is counted for the market view.
func TestAStandSpendsOnlyItsOwnBudget(t *testing.T) {
	s, sys, stores := standServer(t, "clean")
	define(t, sys, "clean", "boutique", time.Now())
	s.rlStaand = newRateLimitGroup(0, 2) // two tokens, no refill

	fire(s, "/s/clean/boutique?page=/a", "https://example.com/")
	fire(s, "/s/clean/boutique?page=/b", "https://example.com/")
	fire(s, "/s/clean/boutique?page=/c", "https://example.com/") // over budget

	recorded := len(arrivalsFor(t, stores["clean"], "/a")) + len(arrivalsFor(t, stores["clean"], "/b")) + len(arrivalsFor(t, stores["clean"], "/c"))
	if recorded != 2 {
		t.Fatalf("recorded %d arrivals, want 2 with the third dropped", recorded)
	}
	if dropped := s.staandDropCount("clean/boutique"); dropped != 1 {
		t.Fatalf("counted %d drops, want 1", dropped)
	}
}

func listStands(t *testing.T, s *QNTXServer) []staandInfo {
	t.Helper()
	rec := httptest.NewRecorder()
	s.HandleStaands(rec, httptest.NewRequest(http.MethodGet, "/api/staands", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Staands []staandInfo `json:"staands"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	return body.Staands
}

// Listing shows every stand across all markets, each naming its market, with
// its door, creator, defining ASID, and activity; a deleted stand drops out.
func TestListingAcrossMarkets(t *testing.T) {
	s, sys, stores := standServer(t, "clean", "haarlem")
	withDoor(s, "clean", "https://example.com")
	now := time.Now()
	define(t, sys, "clean", "boutique", now)
	define(t, sys, "haarlem", "market", now)
	define(t, sys, "clean", "gone", now)
	undefine(t, sys, "clean", "gone", now.Add(time.Second))

	// One arrival to the clean stand, so its activity shows.
	fire(s, "/s/clean/boutique?page=/deep&v=V1", "https://example.com/deep")

	list := listStands(t, s)
	if len(list) != 2 {
		t.Fatalf("listed %d stands, want 2 (gone is deleted): %+v", len(list), list)
	}

	byKey := map[string]staandInfo{}
	for _, st := range list {
		byKey[st.Market+"/"+st.Slug] = st
	}
	clean, ok := byKey["clean/boutique"]
	if !ok {
		t.Fatalf("clean/boutique not listed: %+v", list)
	}
	if clean.Market != "clean" || clean.URL != "/s/clean/boutique" || clean.Origin != "example.com" {
		t.Fatalf("clean stand listed wrong: %+v", clean)
	}
	if clean.DefID == "" || clean.Created == "" || clean.Creator == "" {
		t.Fatalf("the defining attestation is not surfaced: %+v", clean)
	}
	if clean.Arrivals != 1 || len(clean.Sites) != 1 || clean.Sites[0] != "example.com" {
		t.Fatalf("clean activity wrong: arrivals=%d sites=%v", clean.Arrivals, clean.Sites)
	}
	if _, ok := byKey["haarlem/market"]; !ok {
		t.Fatalf("the haarlem stand is not listed across markets: %+v", list)
	}
	_ = stores
}

// Creating a stand then removing it: it lists, then it does not.
func TestCreatingAndRemovingAStand(t *testing.T) {
	s, _, _ := standServer(t, "clean")

	rec := httptest.NewRecorder()
	body := `{"market":"clean","slug":"home"}`
	s.HandleStaands(rec, httptest.NewRequest(http.MethodPost, "/api/staands", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	if got := listStands(t, s); len(got) != 1 || got[0].Slug != "home" || got[0].URL != "/s/clean/home" {
		t.Fatalf("after create, listed %+v", got)
	}

	rec = httptest.NewRecorder()
	s.HandleStaands(rec, httptest.NewRequest(http.MethodDelete, "/api/staands?market=clean&slug=home", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("remove: %d %s", rec.Code, rec.Body.String())
	}
	if got := listStands(t, s); len(got) != 0 {
		t.Fatalf("after remove, still listed %+v", got)
	}
}

// The Sentry event dimension is bounded: a stand's first staandEventCap distinct
// events keep their name, and the rest fold to "other" so a caller cannot explode
// the metric's cardinality (ADR-035).
func TestStaandEventDimCapsCardinality(t *testing.T) {
	s := &QNTXServer{}
	key := "clean/boutique"
	for i := 0; i < staandEventCap; i++ {
		e := fmt.Sprintf("staand:e%d", i)
		if got := s.staandEventDim(key, e); got != e {
			t.Fatalf("event %q folded before the cap: %q", e, got)
		}
	}
	if got := s.staandEventDim(key, "staand:e0"); got != "staand:e0" {
		t.Fatalf("a seen event folded: %q", got)
	}
	if got := s.staandEventDim(key, "staand:overflow"); got != "other" {
		t.Fatalf("past the cap should fold to other, got %q", got)
	}
}

// The hit is read once, into the shape ADR-036 fixed. What it says is filled in;
// what nothing on the request sources stays empty, and that emptiness is the
// distance between the stand as it stands and the arrival as it is specified.
func TestAHitIsReadOnceIntoAnArrival(t *testing.T) {
	at := time.Date(2026, 9, 8, 14, 30, 0, 0, time.UTC)
	req := httptest.NewRequest(http.MethodGet,
		"/s/clean/boutique?page=/deep-clean&v=WHO-000001&visit=SITTING-01&e=contact_click"+
			"&ref=https%3A%2F%2Fnews.example%2Fposts%2F7"+
			"&utm_source=newsletter&utm_medium=email&utm_campaign=autumn"+
			"&utm_content=header&utm_term=schoonmaak&flyer=blue", nil)

	a := staandArrival(req, "clean", "boutique", at)

	if a.Market != "clean" || a.Slug != "boutique" {
		t.Fatalf("the stand read as %s/%s, want clean/boutique", a.Market, a.Slug)
	}
	if a.Path != "/deep-clean" || a.Event != "contact_click" {
		t.Fatalf("the hit read as %q %q", a.Path, a.Event)
	}
	// Two ids: the person, and the sitting the person is in.
	if a.Visitor != "WHO-000001" || a.Visit != "SITTING-01" {
		t.Fatalf("the ids read as visitor %q visit %q", a.Visitor, a.Visit)
	}
	// The referrer arrives whole and is kept split.
	if a.ReferrerDomain != "news.example" || a.ReferrerPath != "/posts/7" {
		t.Fatalf("the referrer split to %q %q", a.ReferrerDomain, a.ReferrerPath)
	}
	if a.UtmSource != "newsletter" || a.UtmMedium != "email" || a.UtmCampaign != "autumn" ||
		a.UtmContent != "header" || a.UtmTerm != "schoonmaak" {
		t.Fatalf("the campaign five read as %+v", a)
	}
	// Everything with a field of its own is gone from params; what the site
	// invented is kept under the key it arrived with.
	if len(a.Params) != 1 || a.Params["flyer"] != "blue" {
		t.Fatalf("the leftover params are %v, want only flyer=blue", a.Params)
	}
	if a.At != at.Format(time.RFC3339Nano) {
		t.Fatalf("stamped %q, want %q", a.At, at.Format(time.RFC3339Nano))
	}
	if a.Browser != "" || a.Country != "" {
		t.Fatalf("a field with no source on the request was filled: %+v", a)
	}
}

// A shared fallback is not an id. A site that wanted one and had none ships the
// same word to everybody, and the node cannot tell that from a person later.
func TestASharedFallbackIsNotAVisitor(t *testing.T) {
	s, sys, stores := standServer(t, "clean")
	define(t, sys, "clean", "boutique", time.Now())

	fire(s, "/s/clean/boutique?page=/&v=anon", "https://clean.example/")
	fire(s, "/s/clean/boutique?page=/&v=ANON", "https://clean.example/")
	fire(s, "/s/clean/boutique?page=/&v=none", "https://clean.example/")
	fire(s, "/s/clean/boutique?page=/&v=short", "https://clean.example/")
	fire(s, "/s/clean/boutique?page=/&v=REAL-VISITOR-01", "https://clean.example/")

	found := arrivalsFor(t, stores["clean"], "/")
	if len(found) != 5 {
		t.Fatalf("every arrival is still recorded: got %d, want 5", len(found))
	}
	kept := map[string]int{}
	for _, as := range found {
		if v := attrString(as.Attributes, staandVisitor); v != "" {
			kept[v]++
		}
	}
	if len(kept) != 1 || kept["REAL-VISITOR-01"] != 1 {
		t.Fatalf("only a real id is kept as a visitor, got %v", kept)
	}
}

// A walk is one sitting. Keyed by the visitor it is a lifetime, and a person
// who came back on two days drew one line spanning both.
func TestAWalkIsOneSittingWhenTheHitSaysWhich(t *testing.T) {
	s, sys, stores := standServer(t, "clean")
	define(t, sys, "clean", "boutique", time.Now())

	fire(s, "/s/clean/boutique?page=/&v=WHO-000001&visit=SITTING-01", "https://clean.example/")
	fire(s, "/s/clean/boutique?page=/prices&v=WHO-000001&visit=SITTING-01", "https://clean.example/")
	fire(s, "/s/clean/boutique?page=/&v=WHO-000001&visit=SITTING-02", "https://clean.example/")

	arrivals, err := stores["clean"].GetAttestations(ats.AttestationFilter{Source: staandSource, Limit: 100})
	if err != nil {
		t.Fatalf("GetAttestations: %v", err)
	}
	keys := map[string]int{}
	for _, as := range arrivals {
		keys[walkKey(as, attrString(as.Attributes, staandVisitor))]++
	}
	// One person, two sittings, so two walks rather than one.
	if len(keys) != 2 || keys["SITTING-01"] != 2 || keys["SITTING-02"] != 1 {
		t.Fatalf("one visitor across two sittings drew %v, want two walks", keys)
	}
}

// An arrival recorded before anything sent a visit id still draws one walk for
// that browser: it is all the record supports, and inventing more would lie.
func TestAWalkWithNoVisitIdFallsBackToTheVisitor(t *testing.T) {
	as := &types.As{Attributes: map[string]any{staandVisitor: "WHO-000001"}}
	if got := walkKey(as, "WHO-000001"); got != "WHO-000001" {
		t.Fatalf("with no visit id the walk is the visitor's, got %q", got)
	}
	if got := walkKey(&types.As{}, ""); got != "" {
		t.Fatalf("an arrival naming nobody belongs to no walk, got %q", got)
	}
}

// A sitting is derived, never written. Entry and exit are the first and last
// arrival of a visit, and one page view alone is the bounce convention.
func TestAVisitIsDerivedFromItsArrivals(t *testing.T) {
	s, sys, stores := standServer(t, "clean")
	define(t, sys, "clean", "boutique", time.Now())

	fire(s, "/s/clean/boutique?page=/&v=WHO-000001&visit=SITTING-01", "https://clean.example/")
	fire(s, "/s/clean/boutique?page=/prices&v=WHO-000001&visit=SITTING-01&e=hover", "https://clean.example/prices")
	fire(s, "/s/clean/boutique?page=/only&v=WHO-000002&visit=SITTING-02", "https://clean.example/only")

	arrivals, err := stores["clean"].GetAttestations(ats.AttestationFilter{Source: staandSource, Limit: 100})
	if err != nil {
		t.Fatalf("GetAttestations: %v", err)
	}
	visits := staandVisits(arrivals, "clean", "boutique")
	if len(visits) != 2 {
		t.Fatalf("derived %d visits from two sittings: %+v", len(visits), visits)
	}

	by := map[string]*protocol.Visit{}
	for _, v := range visits {
		by[v.Visit] = v
	}
	walked, alone := by["SITTING-01"], by["SITTING-02"]
	if walked == nil || alone == nil {
		t.Fatalf("the sittings derived were %+v", visits)
	}
	if walked.EntryPath != "/" || walked.ExitPath != "/prices" {
		t.Fatalf("the walk entered %q and left %q", walked.EntryPath, walked.ExitPath)
	}
	if walked.Events != 2 || walked.Views != 1 {
		t.Fatalf("the walk counted %d events and %d views", walked.Events, walked.Views)
	}
	if walked.Bounce {
		t.Fatal("two arrivals is not a bounce")
	}
	if !alone.Bounce {
		t.Fatalf("one page view alone is a bounce: %+v", alone)
	}
	if alone.Visitor != "WHO-000002" {
		t.Fatalf("the sitting belongs to %q", alone.Visitor)
	}
}

// A breakdown is one endpoint with a dimension parameter, and the dimension is
// a field of an arrival. An unknown one is refused, not answered with nothing.
func TestABreakdownGroupsByOneDimension(t *testing.T) {
	s, sys, _ := standServer(t, "clean")
	define(t, sys, "clean", "boutique", time.Now())

	fire(s, "/s/clean/boutique?page=/&v=WHO-000001&utm_source=newsletter", "https://clean.example/")
	fire(s, "/s/clean/boutique?page=/&v=WHO-000002&utm_source=newsletter", "https://clean.example/")
	fire(s, "/s/clean/boutique?page=/prices&v=WHO-000001", "https://clean.example/prices")

	pages := breakdown(t, s, "page")
	if len(pages) != 2 || pages[0].Name != "/" || pages[0].Count != 2 {
		t.Fatalf("by page: %+v", pages)
	}
	campaign := breakdown(t, s, "utm_source")
	if len(campaign) != 1 || campaign[0].Name != "newsletter" || campaign[0].Count != 2 {
		t.Fatalf("by utm_source: %+v", campaign)
	}
	visitors := breakdown(t, s, "visitor")
	if len(visitors) != 2 {
		t.Fatalf("by visitor: %+v", visitors)
	}

	rec := httptest.NewRecorder()
	s.HandleStaandMetrics(rec, httptest.NewRequest(http.MethodGet,
		"/api/staands/metrics?market=clean&slug=boutique&type=eyecolour", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("an unknown dimension returned %d, want 400", rec.Code)
	}
}

// breakdown asks one stand's metrics endpoint for one dimension.
func breakdown(t *testing.T, s *QNTXServer, dim string) []staandCount {
	t.Helper()
	rec := httptest.NewRecorder()
	s.HandleStaandMetrics(rec, httptest.NewRequest(http.MethodGet,
		"/api/staands/metrics?market=clean&slug=boutique&type="+dim, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: %d %s", dim, rec.Code, rec.Body.String())
	}
	var got struct {
		Counts []staandCount `json:"counts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("%s: %v", dim, err)
	}
	return got.Counts
}

// The window a read covers is named in AX's own words, and a word that is not a
// time is refused rather than read as no window at all.
func TestAReadTakesAWindowInWords(t *testing.T) {
	s, sys, _ := standServer(t, "clean")
	define(t, sys, "clean", "boutique", time.Now())
	fire(s, "/s/clean/boutique?page=/&v=WHO-000001", "https://clean.example/")

	rec := httptest.NewRecorder()
	s.HandleStaandMetrics(rec, httptest.NewRequest(http.MethodGet,
		"/api/staands/metrics?market=clean&slug=boutique&type=page&since=yesterday", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("since yesterday: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	s.HandleStaandMetrics(rec, httptest.NewRequest(http.MethodGet,
		"/api/staands/metrics?market=clean&slug=boutique&type=page&since=whenever", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("since whenever returned %d, want 400", rec.Code)
	}
}

// Creating a stand into system or default is refused.
func TestCreatingAStandInSystemOrDefaultIsRefused(t *testing.T) {
	s, _, _ := standServer(t, "clean")
	for _, market := range []string{"system", "default"} {
		rec := httptest.NewRecorder()
		body := `{"market":"` + market + `","slug":"x"}`
		s.HandleStaands(rec, httptest.NewRequest(http.MethodPost, "/api/staands", strings.NewReader(body)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: create returned %d, want 400", market, rec.Code)
		}
	}
}
