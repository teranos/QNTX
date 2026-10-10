package auth

import (
	"crypto/ed25519"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/internal/access"
)

// rootAgentToken is a token derived the way the node derives the ROOT agent's.
func rootAgentToken(t *testing.T) (raw, did string) {
	t.Helper()
	raw, did, err := access.DeriveToken(ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize)), "agent:root")
	require.NoError(t, err)
	return raw, did
}

func rootAgentHandler(roots ...string) (*Handler, *memTokenStore) {
	store := newMemTokenStore()
	h := &Handler{users: &memUsers{}, sessions: newSessionStore(1), tokens: store, logger: testLogger()}
	h.SetIdentities(roots, nil)
	return h, store
}

// presenting asks a route no line grants to anybody, which is ROOT's alone,
// with a bearer, and gives the status and what was admitted.
func presenting(h *Handler, raw string) (int, Admission) {
	var admitted Admission
	gated := h.Middleware("/test", Reach{}, func(w http.ResponseWriter, r *http.Request) {
		admitted, _ = AdmissionFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	gated.ServeHTTP(rec, req)
	return rec.Code, admitted
}

// The ROOT agent's token is ROOT's kind, written down by the node for the
// agent it runs (ADR-048). It reaches what ROOT reaches, and what it writes is
// its own DID's.
func TestTheRootAgentsTokenIsRootsKindAndItsOwnDID(t *testing.T) {
	h, _ := rootAgentHandler(mastodonAccount)
	raw, did := rootAgentToken(t)
	require.NoError(t, h.HoldRootAgent(raw, did))

	status, admitted := presenting(h, raw)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, did, admitted.ActsAs(), "the ROOT agent attests as itself")
	assert.Equal(t, mastodonAccount, admitted.Identity, "it speaks for ROOT")
	assert.True(t, admitted.MaySeeSystem())
	assert.True(t, admitted.MayWrite("anything at all"), "no line narrows what ROOT's kind may write")
	assert.True(t, admitted.MayRead("anything at all"))
}

// The node holds it at every start, and it stays the one token.
func TestHoldingTheRootAgentAgainWritesNothingMore(t *testing.T) {
	h, store := rootAgentHandler(mastodonAccount)
	raw, did := rootAgentToken(t)
	require.NoError(t, h.HoldRootAgent(raw, did))
	require.NoError(t, h.HoldRootAgent(raw, did))

	held, err := store.List()
	require.NoError(t, err)
	require.Len(t, held, 1)
	assert.Equal(t, RootAgentLabel, held[0].Label)
	assert.Equal(t, LevelRoot, held[0].Level)
	assert.Equal(t, did, held[0].DID)
}

// Revocation is a switch (ADR-025), and ROOT's hand on it holds: a start does
// not turn the ROOT agent back on.
func TestARootAgentSwitchedOffStaysOff(t *testing.T) {
	h, store := rootAgentHandler(mastodonAccount)
	raw, did := rootAgentToken(t)
	require.NoError(t, h.HoldRootAgent(raw, did))
	held, err := store.List()
	require.NoError(t, err)
	require.NoError(t, store.setRevoked(held[0].ID, true))

	err = h.HoldRootAgent(raw, did)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "switched off")
	status, _ := presenting(h, raw)
	assert.Equal(t, http.StatusUnauthorized, status)
	held, err = store.List()
	require.NoError(t, err)
	assert.Len(t, held, 1, "a second token was written for an agent that was switched off")
}

// It speaks for ROOT, so a node that lists nobody has nobody for it to speak for.
func TestTheRootAgentSpeaksForSomebodyListed(t *testing.T) {
	h, store := rootAgentHandler()
	raw, did := rootAgentToken(t)
	err := h.HoldRootAgent(raw, did)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth.root_identities")
	held, listErr := store.List()
	require.NoError(t, listErr)
	assert.Empty(t, held)
}

func TestANodeThatKeepsNoTokensHoldsNoRootAgent(t *testing.T) {
	h := &Handler{users: &memUsers{}, sessions: newSessionStore(1), logger: testLogger()}
	h.SetIdentities([]string{mastodonAccount}, nil)
	raw, did := rootAgentToken(t)
	require.Error(t, h.HoldRootAgent(raw, did))
}

// Minting hands ROOT's kind to nobody, as before: the node writing it down for
// the agent it runs is the one way one comes to be.
func TestMintingStillHandsRootToNobody(t *testing.T) {
	_, minted := mintable("ROOT")
	assert.False(t, minted)
}
