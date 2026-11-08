package auth

import (
	"crypto/ed25519"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/internal/access"
)

// 「[Opus 5.5] namespaceAgentDID is the DID the node derives a namespace agent's key under:」
// 「[Opus 5.5] from its key, for the namespace and the model.」
func namespaceAgentDID(t *testing.T) string {
	t.Helper()
	key, err := access.DeriveKey(ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize)), "agent:garden:claude-sonnet-5-5")
	require.NoError(t, err)
	return EncodeDIDKey(key.Public().(ed25519.PublicKey))
}

const theGardener = "https://example.org/theGardener"

// namespaceAgentHandler is nodeAgentHandler with the theGardener a USER the node
// 「[Opus 5.5] invited: an agent speaks for whoever set it up, and the gate asks that they」
// are still admitted.
func namespaceAgentHandler() (*Handler, *memTokenStore) {
	h, store := nodeAgentHandler(mastodonAccount)
	h.users = &memUsers{held: []User{{ID: "US-GARDENER", Level: LevelUser, DisplayName: "The theGardener",
		Accounts: []UserAccount{{CanonicalID: theGardener}}}}}
	return h, store
}

// 「[Opus 5.5] A namespace agent acts in its namespace and nowhere else, speaking for」
// 「[Opus 5.5] whoever opted the namespace into it (ADR-048): it attests as its own DID,」
// 「[Opus 5.5] acts in the namespace, sees no system, and no token is written.」
func TestANamespaceAgentActsInItsNamespaceAlone(t *testing.T) {
	h, store := namespaceAgentHandler()
	did := namespaceAgentDID(t)
	grant, err := h.NamespaceAgentGrant(did, "agent:garden:claude-sonnet-5-5", theGardener, "garden")
	require.NoError(t, err)

	// A route ROOT alone reaches is out of its reach: it is a TOKEN, not ROOT's kind.
	status, _ := throughDoor(h, grant, Reach{})
	assert.Equal(t, http.StatusForbidden, status)
	status, admitted := throughDoor(h, grant, Also(LevelToken))
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, did, admitted.ActsAs(), "the agent attests as itself")
	assert.Equal(t, theGardener, admitted.Identity, "it speaks for whoever set it up")
	assert.Equal(t, "US-GARDENER", admitted.UserID)
	assert.True(t, admitted.MayActIn("garden"))
	assert.False(t, admitted.MayActIn("orchard"))
	assert.False(t, admitted.MaySeeSystem())

	held, err := store.List()
	require.NoError(t, err)
	assert.Empty(t, held, "a token was written for a namespace agent")
}

// It speaks for whoever opted the namespace in, so a line that names nobody
// gives it nobody to speak for.
func TestANamespaceAgentSpeaksForWhoeverSetItUp(t *testing.T) {
	h, _ := nodeAgentHandler(mastodonAccount)
	_, err := h.NamespaceAgentGrant(namespaceAgentDID(t), "agent:garden:claude-sonnet-5-5", "", "garden")
	require.Error(t, err)
}

// 「[Opus 5.5] A bearer is not a door: nothing presented in a header reaches an agent's grant.」
func TestNothingPresentedReachesAnAgentsGrant(t *testing.T) {
	h, _ := namespaceAgentHandler()
	did := namespaceAgentDID(t)
	_, err := h.NamespaceAgentGrant(did, "agent:garden:claude-sonnet-5-5", theGardener, "garden")
	require.NoError(t, err)
	for _, presented := range []string{did, "agent:garden:claude-sonnet-5-5", signedOnly(did)} {
		_, ok := presentedAs(h, presented)
		assert.False(t, ok, presented)
	}
}
