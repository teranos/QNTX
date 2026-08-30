package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/server/auth"
	"go.uber.org/zap"
)

const tokenDID = "did:key:ztoken"

// writingAs posts one attestation through the real handler as the given caller.
func writingAs(t *testing.T, caller *auth.Admission, body string) (ats.AttestationStore, *httptest.ResponseRecorder) {
	t.Helper()
	store, db := createTestStore(t)
	s := &QNTXServer{db: db, atsStore: store, logger: zap.NewNop().Sugar()}

	req := httptest.NewRequest(http.MethodPost, "/api/attestations", jsonBody(body))
	if caller != nil {
		req = req.WithContext(auth.WithAdmission(req.Context(), *caller))
	}
	rec := httptest.NewRecorder()
	s.handleCreateAttestation(rec, req)
	return store, rec
}

// actorsOf reads back the actors the handler wrote — `by` in AX.
func actorsOf(t *testing.T, store ats.AttestationStore, rec *httptest.ResponseRecorder) []string {
	t.Helper()
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var said struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &said); err != nil {
		t.Fatalf("the response did not parse: %v", err)
	}
	found, err := store.GetAttestations(ats.AttestationFilter{Limit: 10})
	if err != nil {
		t.Fatalf("GetAttestations: %v", err)
	}
	for _, as := range found {
		if as.ID == said.ID {
			return as.Actors
		}
	}
	t.Fatalf("the attestation %s the handler said it created is not there", said.ID)
	return nil
}

func tokenAdmission() *auth.Admission {
	grant := &auth.Grant{DID: tokenDID, ScopeWrite: []string{auth.ScopeAll}, ScopeRead: []string{auth.ScopeAll}}
	return &auth.Admission{Level: auth.LevelToken, Grant: grant}
}

// TOKATTEST: each token is its own actor.
func TestATokenIsItsOwnActor(t *testing.T) {
	store, rec := writingAs(t, tokenAdmission(),
		`{"subjects":["qntx"],"predicates":["noted"],"actors":[]}`)

	actors := actorsOf(t, store, rec)
	if len(actors) != 1 || actors[0] != tokenDID {
		t.Fatalf("actors = %v, want the token's own DID alone", actors)
	}
}

// Its own, not the only one. Two actors can make contradictory claims about
// the same subject and both are valid, so what a caller names stands after it.
func TestWhatTheCallerNamesStandsAfterTheToken(t *testing.T) {
	store, rec := writingAs(t, tokenAdmission(),
		`{"subjects":["qntx"],"predicates":["noted"],"actors":["ground","claude"]}`)

	actors := actorsOf(t, store, rec)
	want := []string{tokenDID, "ground", "claude"}
	if len(actors) != len(want) {
		t.Fatalf("actors = %v, want %v", actors, want)
	}
	for i, name := range want {
		if actors[i] != name {
			t.Fatalf("actors = %v, want %v — the DID leads and the rest keep their order", actors, want)
		}
	}
}

// A session carries no grant. Nothing is prepended, or every attestation a
// person writes would gain an actor that is not a token.
func TestASessionAddsNoActor(t *testing.T) {
	session := &auth.Admission{Level: auth.LevelSuper, Identity: "https://mastodon.example/@tim"}
	store, rec := writingAs(t, session,
		`{"subjects":["qntx"],"predicates":["noted"],"actors":["tim"]}`)

	actors := actorsOf(t, store, rec)
	if len(actors) != 1 || actors[0] != "tim" {
		t.Fatalf("actors = %v, want only what the caller named", actors)
	}
}
