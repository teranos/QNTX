package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"go.uber.org/zap"
)

func staandServer(t *testing.T) (*QNTXServer, ats.AttestationStore) {
	t.Helper()
	store, db := createTestStore(t)
	return &QNTXServer{db: db, atsStore: store, logger: zap.NewNop().Sugar()}, store
}

// raise writes the defining attestation for a staand into the default market:
// the slug is the subject, staand:raised the predicate, the ware and label its
// attributes (ADR-035).
func raise(t *testing.T, store ats.AttestationStore, slug, ware, label string, at time.Time) {
	t.Helper()
	_, err := store.GenerateAndCreateAttestation(context.Background(), &types.AsCommand{
		Subjects:   []string{slug},
		Predicates: []string{"staand:raised"},
		Contexts:   []string{"_"},
		Actors:     []string{"root"},
		Source:     "cli",
		Timestamp:  at,
		Attributes: map[string]any{"writes": ware, "label": label},
	})
	if err != nil {
		t.Fatalf("raise %q: %v", slug, err)
	}
}

func strike(t *testing.T, store ats.AttestationStore, slug string, at time.Time) {
	t.Helper()
	_, err := store.GenerateAndCreateAttestation(context.Background(), &types.AsCommand{
		Subjects:   []string{slug},
		Predicates: []string{"staand:struck"},
		Contexts:   []string{"_"},
		Actors:     []string{"root"},
		Source:     "cli",
		Timestamp:  at,
	})
	if err != nil {
		t.Fatalf("strike %q: %v", slug, err)
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

func arrivals(t *testing.T, store ats.AttestationStore, subject string) []*types.As {
	t.Helper()
	found, err := store.GetAttestations(ats.AttestationFilter{Subjects: []string{subject}, Limit: 10})
	if err != nil {
		t.Fatalf("GetAttestations: %v", err)
	}
	return found
}

// A call to a raised staand lands one arrival in its market: the ware as the
// predicate, the subject in the ware's vocabulary, the actor forced to the
// staand, the context the page it fired from (ADR-035).
func TestAStaandArrivalIsRecorded(t *testing.T) {
	s, store := staandServer(t)
	raise(t, store, "boutique", "page:seen", "home", time.Now())

	rec := fire(s, "/s/default/boutique?subject=VISIT01&schema=1", "https://example.com/deep-clean")

	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/gif" {
		t.Fatalf("the pixel did not come back: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	got := arrivals(t, store, "page:VISIT01")
	if len(got) != 1 {
		t.Fatalf("stored %d arrivals, want 1", len(got))
	}
	as := got[0]
	if as.Predicates[0] != "page:seen" || as.Source != "staand" {
		t.Fatalf("stored %v from %q", as.Predicates, as.Source)
	}
	if len(as.Actors) != 1 || as.Actors[0] != "staand:home" {
		t.Fatalf("the actor is %v, not the staand", as.Actors)
	}
	if as.Contexts[0] != "https://example.com/deep-clean" {
		t.Fatalf("the context is %v, not the page it fired from", as.Contexts)
	}
	if as.Attributes["schema"] != "1" {
		t.Fatalf("the schema attribute did not survive: %v", as.Attributes)
	}
	if _, leaked := as.Attributes["subject"]; leaked {
		t.Fatal("the subject parameter doubled as an attribute")
	}
}

// A slug no staand stands under records nothing, and still answers with the
// pixel — probing teaches nothing.
func TestAnUnraisedSlugRecordsNothing(t *testing.T) {
	s, store := staandServer(t)

	rec := fire(s, "/s/default/nostall?subject=VISIT01", "")

	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/gif" {
		t.Fatalf("answered %d %s rather than the pixel", rec.Code, rec.Header().Get("Content-Type"))
	}
	if got := arrivals(t, store, "page:VISIT01"); len(got) != 0 {
		t.Fatalf("an unraised slug recorded: %v", got)
	}
}

// A struck staand supersedes its raising, so arrivals stop.
func TestAStruckStaandRecordsNothing(t *testing.T) {
	s, store := staandServer(t)
	now := time.Now()
	raise(t, store, "boutique", "page:seen", "home", now)
	strike(t, store, "boutique", now.Add(time.Second))

	fire(s, "/s/default/boutique?subject=VISIT01", "https://example.com/")

	if got := arrivals(t, store, "page:VISIT01"); len(got) != 0 {
		t.Fatalf("a struck staand recorded: %v", got)
	}
}
