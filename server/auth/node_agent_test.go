package auth

import (
	"context"
	"crypto/ed25519"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/internal/access"
)

// "「[Sonnet 5.5] A signature proves the key, and nothing reusable leaves the node.」"

// 「[Opus 5.5] nodeAgentDID is the DID the node derives the Node Agent's key under.」
func nodeAgentDID(t *testing.T) string {
	t.Helper()
	key, err := access.DeriveKey(ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize)), "agent:root")
	require.NoError(t, err)
	return EncodeDIDKey(key.Public().(ed25519.PublicKey))
}

func nodeAgentHandler(roots ...string) (*Handler, *memTokenStore) {
	store := newMemTokenStore()
	h := &Handler{users: &memUsers{}, sessions: newSessionStore(1), tokens: store, logger: testLogger()}
	h.SetIdentities(roots, nil)
	return h, store
}

// 「[Opus 5.5] throughDoor asks a route reach grants, as a request arriving at the door the」
// 「[Opus 5.5] node opened for the agent whose grant this is: nothing presented.」
func throughDoor(h *Handler, grant Grant, reach Reach) (int, Admission) {
	var admitted Admission
	gated := h.Middleware("/test", reach, func(w http.ResponseWriter, r *http.Request) {
		admitted, _ = AdmissionFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req = req.WithContext(ThroughDoor(context.Background(), grant))
	rec := httptest.NewRecorder()
	gated.ServeHTTP(rec, req)
	return rec.Code, admitted
}

// 「[Opus 5.5] The Node Agent is ROOT's kind at its own door (ADR-048). It reaches what」
// 「[Opus 5.5] ROOT reaches, what it writes is its own DID's, and no token is written.」
func TestTheNodeAgentIsRootsKindAndItsOwnDID(t *testing.T) {
	h, store := nodeAgentHandler(mastodonAccount)
	did := nodeAgentDID(t)
	grant, err := h.NodeAgentGrant(did)
	require.NoError(t, err)

	status, admitted := throughDoor(h, grant, Reach{})
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, did, admitted.ActsAs(), "the Node Agent attests as itself")
	assert.Equal(t, mastodonAccount, admitted.Identity, "it speaks for ROOT")
	assert.True(t, admitted.MaySeeSystem())
	assert.True(t, admitted.MayWrite("anything at all"), "no line narrows what ROOT's kind may write")
	assert.True(t, admitted.MayRead("anything at all"))

	held, err := store.List()
	require.NoError(t, err)
	assert.Empty(t, held, "a token was written for the Node Agent")
}

// It speaks for ROOT, so a node that lists nobody has nobody for it to speak for.
func TestTheNodeAgentSpeaksForSomebodyListed(t *testing.T) {
	h, _ := nodeAgentHandler()
	_, err := h.NodeAgentGrant(nodeAgentDID(t))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth.root_identities")
}

// 「[Opus 5.5] ROOT struck out of am.toml takes the Node Agent with it at the next request.」
func TestTheNodeAgentStopsWithWhoItSpeaksFor(t *testing.T) {
	h, _ := nodeAgentHandler(mastodonAccount)
	grant, err := h.NodeAgentGrant(nodeAgentDID(t))
	require.NoError(t, err)
	h.SetIdentities(nil, nil)
	status, _ := throughDoor(h, grant, Reach{})
	assert.Equal(t, http.StatusUnauthorized, status)
}

// 「[Opus 5.5] An element agent is ROOT's kind as the Node Agent is, named for its element.」
func TestAnElementAgentIsRootsKindNamedForItsElement(t *testing.T) {
	h, store := nodeAgentHandler(mastodonAccount)
	did := nodeAgentDID(t)
	grant, err := h.ElementAgentGrant(did, "tokens-element")
	require.NoError(t, err)
	assert.Equal(t, ElementAgentLabel+":tokens-element", grant.Label)
	status, admitted := throughDoor(h, grant, Reach{})
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, did, admitted.ActsAs())
	held, err := store.List()
	require.NoError(t, err)
	assert.Empty(t, held, "a token was written for an element agent")
}

// 「[Opus 5.5] Minting hands ROOT's kind to nobody.」
func TestMintingStillHandsRootToNobody(t *testing.T) {
	_, minted := mintable("ROOT")
	assert.False(t, minted)
}
