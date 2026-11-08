package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The primary asks a peer for its row as the person, signing for them as
// itself, so the peer files and finds news under the same person.
func TestAPeerIsAskedAsThePerson(t *testing.T) {
	primary, got, _ := anyPeer(t)
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
	assert.True(t, strings.HasPrefix(got.authorization, signedScheme), "the person was not carried by a signature")
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

// A peer is asked as the person acting in one namespace it serves: what the
// primary signs names that one, so the peer reads where it was asked to.
func TestAPeerIsAskedInOneNamespaceItServes(t *testing.T) {
	primary, got, _ := anyPeer(t)
	admitted := Admitted(LevelRoot)
	admitted.Identity = mastodonAccount
	peer := primary.Peers()[0]

	answer := primary.AskPeerIn(context.Background(), admitted, peer, NamespaceDefault, "/api/transcripts?session=s-1", time.Second, 1<<10)
	require.NoError(t, answer.Err)
	assert.Equal(t, http.StatusTeapot, answer.Status)
	assert.Equal(t, "/api/transcripts", got.path)
	assert.Equal(t, "session=s-1", got.query)

	signed, carried := strings.CutPrefix(got.authorization, signedScheme)
	require.True(t, carried, "the person was not carried by a signature: %q", got.authorization)
	var claims carriedClaims
	_, _, err := jwt.NewParser().ParseUnverified(signed, &claims)
	require.NoError(t, err)
	assert.Equal(t, []string{NamespaceDefault}, claims.Namespaces)
}

// A namespace the peer is not declared serving is not asked in.
func TestAPeerIsNotAskedInANamespaceItDoesNotServe(t *testing.T) {
	primary, got, _ := anyPeer(t)
	admitted := Admitted(LevelRoot)
	admitted.Identity = mastodonAccount

	answer := primary.AskPeerIn(context.Background(), admitted, primary.Peers()[0], "Ground", "/api/transcripts?session=s-1", time.Second, 1<<10)
	require.Error(t, answer.Err)
	assert.Empty(t, got.path, "the peer was asked in a namespace it does not serve")
}

// An answer longer than the most asked for is an error, never an answer cut short.
func TestAnAnswerPastWhatWasAskedForIsAnError(t *testing.T) {
	_, did := otherNode(t)
	primary, _, _ := authorizingHandler(t)
	long := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 2<<10)))
	}))
	t.Cleanup(long.Close)
	require.NoError(t, primary.DeclarePeer(peerName, did, []string{NamespaceDefault}, long.URL, long.URL))
	admitted := Admitted(LevelRoot)
	admitted.Identity = mastodonAccount

	answer := primary.AskPeerIn(context.Background(), admitted, primary.Peers()[0], NamespaceDefault, "/api/transcripts", time.Second, 1<<10)
	require.Error(t, answer.Err)
	assert.Empty(t, answer.Body)
}
