package auth

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The primary asks a peer for its row as the person, with a token issued here
// for that peer, so the peer files and finds news under the same person.
func TestAPeerIsAskedAsThePerson(t *testing.T) {
	primary, got, peerDID := anyPeer(t)
	admitted := Admitted(LevelRoot)
	admitted.Identity = mastodonAccount

	answers := primary.AskPeers(context.Background(), admitted, "/am/statusline?format=json")
	require.Len(t, answers, 1)
	answer := answers[0]
	require.NoError(t, answer.Err)
	assert.Equal(t, peerName, answer.Name)
	assert.Equal(t, http.StatusTeapot, answer.Status)

	assert.Equal(t, "/am/statusline", got.path)
	assert.Equal(t, "format=json", got.query)
	raw, bears := bearerToken(&http.Request{Header: http.Header{"Authorization": {got.authorization}}})
	require.True(t, bears, "the person was not carried")
	grant, live := primary.tokens.Lookup(sha256Hex(raw))
	require.True(t, live)
	assert.Equal(t, mastodonAccount, grant.MintedBy)
	assert.Equal(t, peerDID, grant.ClientDID)
}

// Nobody is carried to a peer: a caller with no identity is answered with why.
func TestNobodyIsNotCarriedToAPeer(t *testing.T) {
	primary, got, _ := anyPeer(t)
	answers := primary.AskPeers(context.Background(), Admitted(LevelRoot), "/am/statusline?format=json")
	require.Len(t, answers, 1)
	require.Error(t, answers[0].Err)
	assert.Empty(t, got.path, "the peer was asked for nobody")
}

// A node with no peers asks nobody and says so with an empty answer.
func TestANodeWithNoPeersAsksNobody(t *testing.T) {
	primary, _, _ := authorizingHandler(t)
	admitted := Admitted(LevelRoot)
	admitted.Identity = mastodonAccount
	assert.Empty(t, primary.AskPeers(context.Background(), admitted, "/am/statusline?format=json"))
}
