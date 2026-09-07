package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
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
func (m markets) OpenNamespace(name string) (ats.AttestationStore, error) {
	if s, ok := m.store[name]; ok {
		return s, nil
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
	s := &QNTXServer{
		db:              db,
		atsStore:        sys,
		systemStore:     sys,
		logger:          zap.NewNop().Sugar(),
		namespaces:      m,
		namespaceOpener: m,
	}
	return s, sys, stores
}

// define writes a stand's created line into system directly (ADR-035): the
// subject is the stand's key market/slug, the door and label its attributes.
func define(t *testing.T, sys ats.AttestationStore, market, slug, label, origin string, at time.Time) {
	t.Helper()
	_, err := sys.GenerateAndCreateAttestation(context.Background(), &types.AsCommand{
		Subjects:   []string{market + "/" + slug},
		Predicates: []string{staandCreated},
		Contexts:   []string{"system"},
		Actors:     []string{"root"},
		Source:     "cli",
		Timestamp:  at,
		Attributes: map[string]any{defLabel: label, defOrigin: origin, defMarket: market},
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
		Attributes: map[string]any{defMarket: market},
	})
	if err != nil {
		t.Fatalf("undefine %q: %v", slug, err)
	}
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

// A call to a defined stand lands one arrival in its market: the predicate is
// the stand vocabulary plus the pixel side's event, the subject is the id the
// pixel side sent, the actor is the stand, the context the page (ADR-035).
func TestAnArrivalIsRecorded(t *testing.T) {
	s, sys, stores := standServer(t, "clean")
	define(t, sys, "clean", "boutique", "home", "", time.Now())

	rec := fire(s, "/s/clean/boutique?e=contact_click&subject=VISIT01&method=whatsapp", "https://example.com/deep-clean")

	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/gif" {
		t.Fatalf("the pixel did not come back: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	got := arrivalsFor(t, stores["clean"], "VISIT01")
	if len(got) != 1 {
		t.Fatalf("stored %d arrivals, want 1", len(got))
	}
	as := got[0]
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
	if as.Attributes[staandSlugAttr] != "boutique" {
		t.Fatalf("the arrival does not carry its slug: %v", as.Attributes)
	}
	if _, leaked := as.Attributes["e"]; leaked {
		t.Fatal("the event parameter doubled as an attribute")
	}
}

// The pixel side names the event; no event is a page view.
func TestNoEventIsAPageView(t *testing.T) {
	s, sys, stores := standServer(t, "clean")
	define(t, sys, "clean", "boutique", "home", "", time.Now())

	fire(s, "/s/clean/boutique?subject=VISIT01", "https://example.com/")

	got := arrivalsFor(t, stores["clean"], "VISIT01")
	if len(got) != 1 || got[0].Predicates[0] != "staand:pageview" {
		t.Fatalf("a bare arrival recorded %v, want staand:pageview", got)
	}
}

// The definition is a system attestation, and the market holds only arrivals:
// after a create, system carries the created line and the market carries none.
func TestDefinitionLivesInSystemNotMarket(t *testing.T) {
	s, sys, stores := standServer(t, "clean")

	rec := httptest.NewRecorder()
	body := `{"market":"clean","slug":"home","label":"home page","origin":""}`
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

	rec := fire(s, "/s/clean/nostall?subject=VISIT01", "")

	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/gif" {
		t.Fatalf("answered %d %s rather than the pixel", rec.Code, rec.Header().Get("Content-Type"))
	}
	if got := arrivalsFor(t, stores["clean"], "VISIT01"); len(got) != 0 {
		t.Fatalf("an undefined stand recorded: %v", got)
	}
}

// A deleted stand supersedes its definition, so arrivals stop.
func TestADeletedStandRecordsNothing(t *testing.T) {
	s, sys, stores := standServer(t, "clean")
	now := time.Now()
	define(t, sys, "clean", "boutique", "home", "", now)
	undefine(t, sys, "clean", "boutique", now.Add(time.Second))

	fire(s, "/s/clean/boutique?subject=VISIT01", "https://example.com/")

	if got := arrivalsFor(t, stores["clean"], "VISIT01"); len(got) != 0 {
		t.Fatalf("a deleted stand recorded: %v", got)
	}
}

// A stand never records into system or default: an arrival is an untrusted
// public write, and those namespaces hold the node's own records.
func TestAStandNeverWritesSystemOrDefault(t *testing.T) {
	s, sys, _ := standServer(t, "clean")
	// Even with a definition keyed under system/default, the guard refuses.
	define(t, sys, "system", "x", "", "", time.Now())
	define(t, sys, "default", "y", "", "", time.Now())

	fire(s, "/s/system/x?subject=V", "https://example.com/")
	fire(s, "/s/default/y?subject=V", "https://example.com/")

	if got := arrivalsFor(t, sys, "V"); len(got) != 0 {
		t.Fatalf("a stand wrote into system/default: %v", got)
	}
}

// The door: only the bound origin (and its subdomains) may write; anywhere else
// is refused, and a bound stand with no Referer cannot be verified.
func TestOnlyTheBoundDoorWrites(t *testing.T) {
	s, sys, stores := standServer(t, "clean")
	define(t, sys, "clean", "boutique", "home", "example.com", time.Now())

	fire(s, "/s/clean/boutique?subject=OK", "https://www.example.com/x") // subdomain of the door
	fire(s, "/s/clean/boutique?subject=NO", "https://elsewhere.test/x")  // another origin
	fire(s, "/s/clean/boutique?subject=BARE", "")                        // no Referer

	if got := arrivalsFor(t, stores["clean"], "OK"); len(got) != 1 {
		t.Fatalf("the bound door did not write: %v", got)
	}
	if got := arrivalsFor(t, stores["clean"], "NO"); len(got) != 0 {
		t.Fatalf("another origin wrote: %v", got)
	}
	if got := arrivalsFor(t, stores["clean"], "BARE"); len(got) != 0 {
		t.Fatalf("a bound stand wrote with no Referer: %v", got)
	}
}

// A stand spends only its own budget: past the burst, arrivals are dropped and
// the drop is counted for the market view.
func TestAStandSpendsOnlyItsOwnBudget(t *testing.T) {
	s, sys, stores := standServer(t, "clean")
	define(t, sys, "clean", "boutique", "home", "", time.Now())
	s.rlStaand = newRateLimitGroup(0, 2) // two tokens, no refill

	fire(s, "/s/clean/boutique?subject=A", "https://example.com/")
	fire(s, "/s/clean/boutique?subject=B", "https://example.com/")
	fire(s, "/s/clean/boutique?subject=C", "https://example.com/") // over budget

	recorded := len(arrivalsFor(t, stores["clean"], "A")) + len(arrivalsFor(t, stores["clean"], "B")) + len(arrivalsFor(t, stores["clean"], "C"))
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
	now := time.Now()
	define(t, sys, "clean", "boutique", "home", "example.com", now)
	define(t, sys, "haarlem", "market", "square", "", now)
	define(t, sys, "clean", "gone", "old", "", now)
	undefine(t, sys, "clean", "gone", now.Add(time.Second))

	// One arrival to the clean stand, so its activity shows.
	fire(s, "/s/clean/boutique?subject=V1", "https://example.com/deep")

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
	body := `{"market":"clean","slug":"home","label":"home page"}`
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
