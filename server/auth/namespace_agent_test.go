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

// namespaceAgentToken is a token derived the way the node derives a namespace
// agent's: from its key, for the namespace and the model.
func namespaceAgentToken(t *testing.T) (raw, did string) {
	t.Helper()
	raw, did, err := access.DeriveToken(ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize)), "agent:garden:claude-sonnet-5-5")
	require.NoError(t, err)
	return raw, did
}

const theGardener = "https://example.org/theGardener"

// namespaceAgentHandler is rootAgentHandler with the theGardener a USER the node
// invited: a token speaks for whoever minted it, and the gate asks that they
// are still admitted.
func namespaceAgentHandler() (*Handler, *memTokenStore) {
	h, store := rootAgentHandler(mastodonAccount)
	h.users = &memUsers{held: []User{{ID: "US-GARDENER", Level: LevelUser, DisplayName: "The theGardener",
		Accounts: []UserAccount{{CanonicalID: theGardener}}}}}
	return h, store
}

// A namespace agent's token is a token in its namespace and nowhere else,
// speaking for whoever opted the namespace into it (ADR-048): it attests as
// its own DID, acts in the namespace, and sees no system.
func TestANamespaceAgentsTokenActsInItsNamespaceAlone(t *testing.T) {
	h, store := namespaceAgentHandler()
	raw, did := namespaceAgentToken(t)
	require.NoError(t, h.HoldNamespaceAgent(raw, did, "agent:garden:claude-sonnet-5-5", theGardener, "garden"))

	// A route ROOT alone reaches is out of its reach: it is a TOKEN, not ROOT's kind.
	status, _ := presenting(h, raw)
	assert.Equal(t, http.StatusForbidden, status)
	status, admitted := presentingAt(h, raw, Also(LevelToken))
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, did, admitted.ActsAs(), "the agent attests as itself")
	assert.Equal(t, theGardener, admitted.Identity, "it speaks for whoever set it up")
	assert.Equal(t, "US-GARDENER", admitted.UserID)
	assert.True(t, admitted.MayActIn("garden"))
	assert.False(t, admitted.MayActIn("orchard"))
	assert.False(t, admitted.MaySeeSystem())

	held, err := store.List()
	require.NoError(t, err)
	require.Len(t, held, 1)
	assert.Equal(t, "agent:garden:claude-sonnet-5-5", held[0].Label)
	assert.Equal(t, LevelToken, held[0].Level)
	assert.Equal(t, []string{"garden"}, held[0].Namespaces)

	// Held again, it is the one token still.
	require.NoError(t, h.HoldNamespaceAgent(raw, did, "agent:garden:claude-sonnet-5-5", theGardener, "garden"))
	held, err = store.List()
	require.NoError(t, err)
	assert.Len(t, held, 1)
}

// Switched off, it stays off: a use after a restart does not turn it back on.
func TestANamespaceAgentSwitchedOffStaysOff(t *testing.T) {
	h, store := namespaceAgentHandler()
	raw, did := namespaceAgentToken(t)
	require.NoError(t, h.HoldNamespaceAgent(raw, did, "agent:garden:claude-sonnet-5-5", theGardener, "garden"))
	held, err := store.List()
	require.NoError(t, err)
	require.NoError(t, store.setRevoked(held[0].ID, true))

	err = h.HoldNamespaceAgent(raw, did, "agent:garden:claude-sonnet-5-5", theGardener, "garden")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "switched off")
	status, _ := presentingAt(h, raw, Also(LevelToken))
	assert.Equal(t, http.StatusUnauthorized, status)
}

// presentingAt is presenting, on a route a line grants to reach.
func presentingAt(h *Handler, raw string, reach Reach) (int, Admission) {
	var admitted Admission
	gated := h.Middleware("/test", reach, func(w http.ResponseWriter, r *http.Request) {
		admitted, _ = AdmissionFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	gated.ServeHTTP(rec, req)
	return rec.Code, admitted
}

// It speaks for whoever opted the namespace in, so a line that names nobody
// gives it nobody to speak for.
func TestANamespaceAgentSpeaksForWhoeverSetItUp(t *testing.T) {
	h, store := rootAgentHandler(mastodonAccount)
	raw, did := namespaceAgentToken(t)
	require.Error(t, h.HoldNamespaceAgent(raw, did, "agent:garden:claude-sonnet-5-5", "", "garden"))
	held, err := store.List()
	require.NoError(t, err)
	assert.Empty(t, held)
}
