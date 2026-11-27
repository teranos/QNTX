package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/teranos/QNTX/internal/access"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "why cant we make q relay it"
// "I WANT TO KILL THIS IDEA, I DONT WANT TO MAINTAIN TOKENS, AGENNTS KEEP REACHING FOR IT AND CREATING MORE TOKENS, THIS ISNT SECURE"
// 「[Opus 5.5] A relay asks the node that holds the App as this node, signed by its own key」
// 「[Opus 5.5] for that node's origin, once: nothing reusable leaves either node.」
func TestARelayAsksTheNodeThatHoldsTheAppAsItself(t *testing.T) {
	var asked map[string]any
	var authorization string
	q := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, githubPath+"/ask", r.URL.Path)
		authorization = r.Header.Get("Authorization")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&asked))
		_ = json.NewEncoder(w).Encode(map[string]any{"answer": map[string]any{"html_url": "https://github.com/o/r/pull/9"}})
	}))
	defer q.Close()
	s := githubKnowingServer(t)
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	s.authHandler.SetNodeKey(key)
	require.NoError(t, s.nodeRecords().SetRelay(rootAccount, GitHubRelay{URL: q.URL}))

	answered, err := linkedHost{s}.AskGitHub(context.Background(), "CreateAPullRequest", map[string]any{"owner": "o", "repo": "r"})
	require.NoError(t, err)

	assert.JSONEq(t, `{"html_url":"https://github.com/o/r/pull/9"}`, string(answered))
	assert.Equal(t, "CreateAPullRequest", asked["operation"])
	assert.JSONEq(t, `{"owner":"o","repo":"r"}`, asked["request"].(string))

	signed, isSigned := strings.CutPrefix(authorization, "Signed ")
	require.True(t, isSigned, "the relay presented %q, not the node's signature", authorization)
	var claims jwt.RegisteredClaims
	_, err = jwt.ParseWithClaims(signed, &claims, func(*jwt.Token) (any, error) { return key.Public(), nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}))
	require.NoError(t, err, "the relay's signature is not the node's own key's")
	assert.Equal(t, access.EncodeDIDKey(key.Public().(ed25519.PublicKey)), claims.Issuer)
	assert.Equal(t, jwt.ClaimStrings{q.URL}, claims.Audience)
}

func TestARelaysRefusalIsTheAskersError(t *testing.T) {
	q := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"not ROOT"}`))
	}))
	defer q.Close()
	s := githubKnowingServer(t)
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	s.authHandler.SetNodeKey(key)
	require.NoError(t, s.nodeRecords().SetRelay(rootAccount, GitHubRelay{URL: q.URL}))

	_, err = linkedHost{s}.AskGitHub(context.Background(), "GetAPullRequest", map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
	assert.Contains(t, err.Error(), "not ROOT")
}

// 「[Opus 5.5] A node with no key of its own asks through no relay: it has nothing to ask as.」
func TestANodeWithNoKeyAsksThroughNoRelay(t *testing.T) {
	q := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("the relay was asked by a node with no key: %s", r.Header.Get("Authorization"))
	}))
	defer q.Close()
	s := githubKnowingServer(t)
	require.NoError(t, s.nodeRecords().SetRelay(rootAccount, GitHubRelay{URL: q.URL}))

	_, err := linkedHost{s}.AskGitHub(context.Background(), "GetAPullRequest", map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no key of its own")
}

// 「[Opus 5.5] The relay sigil takes where the App is, and nothing to ask it with.」
func TestARelayTakesNoToken(t *testing.T) {
	for _, held := range githubRelaySigil().GetTakes() {
		assert.NotEqual(t, "token_ref", held.GetName())
	}
}

func TestANodeWithNoRelayHasNone(t *testing.T) {
	s := githubKnowingServer(t)
	_, found, err := s.nodeRecords().Relay()
	require.NoError(t, err)
	assert.False(t, found)
}
