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
	"github.com/teranos/QNTX/internal/nodedid"

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
	s.nodeDID = &nodedid.Handler{PrivateKey: key}
	require.NoError(t, s.nodeRecords().SetRelay(rootAccount, GitHubRelay{URL: q.URL}))

	answered, err := linkedHost{s}.AskGitHub(context.Background(), "CreateAPullRequest", map[string]any{"owner": "o", "repo": "r"})
	require.NoError(t, err)

	// 「[Opus 5.5] What came back through the relay reads as what this node's own GitHubService」
	// 「[Opus 5.5] answers: devkq's org binding, on 2026-11-27, read q's answer as a failure saying nothing.」
	assert.JSONEq(t, `{"success":true,"html_url":"https://github.com/o/r/pull/9"}`, string(answered))
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
	s.nodeDID = &nodedid.Handler{PrivateKey: key}
	require.NoError(t, s.nodeRecords().SetRelay(rootAccount, GitHubRelay{URL: q.URL}))

	_, err = linkedHost{s}.AskGitHub(context.Background(), "GetAPullRequest", map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
	assert.Contains(t, err.Error(), "not ROOT")
}

// 「[Opus 5.5] A node that runs without login holds its own key all the same, and asks」
// 「[Opus 5.5] as itself with it: devkq, on 2026-11-27, ran with auth off and the relay」
// 「[Opus 5.5] reached for the auth handler it did not have.」
func TestANodeWithoutLoginAsksAsItself(t *testing.T) {
	var authorization string
	q := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{"answer": map[string]any{}})
	}))
	defer q.Close()
	s := githubKnowingServer(t)
	require.NoError(t, s.nodeRecords().SetRelay(rootAccount, GitHubRelay{URL: q.URL}))
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	s.nodeDID = &nodedid.Handler{PrivateKey: key}
	s.authHandler = nil

	_, err = linkedHost{s}.AskGitHub(context.Background(), "GetAPullRequest", map[string]any{})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(authorization, "Signed "), "a node without login asked with %q", authorization)
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

// 「[Opus 5.5] A relay is an origin, over HTTPS or on this machine: the node signs what it」
// 「[Opus 5.5] hands over and sends it nowhere a path or plain HTTP across a network takes it.」
func TestARelayIsAnOriginOverHTTPSOrThisMachine(t *testing.T) {
	g := &gitHub{host: githubKnowingServer(t)}
	for _, refused := range []string{"http://q.example.com", "https://q.example.com/api", "https://q.example.com/?x=1"} {
		_, refusal := g.githubRelay(context.Background(), map[string]string{"url": refused})
		assert.NotNil(t, refusal, "the relay %s was taken", refused)
	}
	for _, taken := range []string{"https://api.q.sbvh.nl", "https://api.q.sbvh.nl/", "http://127.0.0.1:8791", "http://localhost:8791"} {
		_, refusal := g.githubRelay(context.Background(), map[string]string{"url": taken})
		assert.Nil(t, refusal, "the relay %s was refused", taken)
	}
}

// 「[Opus 5.5] A relay written before the rule is held to it when asked, before anything is signed.」
func TestARelayOverPlainHTTPElsewhereIsNotAsked(t *testing.T) {
	s := githubKnowingServer(t)
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	s.nodeDID = &nodedid.Handler{PrivateKey: key}
	require.NoError(t, s.nodeRecords().SetRelay(rootAccount, GitHubRelay{URL: "http://q.example.com"}))

	_, err = linkedHost{s}.AskGitHub(context.Background(), "GetAPullRequest", map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not HTTPS")
}

// 「[Opus 5.5] The signature goes to the relay and nowhere it is sent on to.」
func TestARelayFollowsNoRedirect(t *testing.T) {
	elsewhere := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("the relay's signature followed a redirect: %s", r.Header.Get("Authorization"))
	}))
	defer elsewhere.Close()
	q := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer q.Close()
	s := githubKnowingServer(t)
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	s.nodeDID = &nodedid.Handler{PrivateKey: key}
	require.NoError(t, s.nodeRecords().SetRelay(rootAccount, GitHubRelay{URL: q.URL}))

	_, err = linkedHost{s}.AskGitHub(context.Background(), "GetAPullRequest", map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "307")
}

// 「[Opus 5.5] A relay written with a trailing slash signs for the origin all the same.」
func TestARelaySignsForItsOrigin(t *testing.T) {
	var authorization string
	q := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{"answer": map[string]any{}})
	}))
	defer q.Close()
	s := githubKnowingServer(t)
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	s.nodeDID = &nodedid.Handler{PrivateKey: key}
	require.NoError(t, s.nodeRecords().SetRelay(rootAccount, GitHubRelay{URL: q.URL + "/"}))

	_, err = linkedHost{s}.AskGitHub(context.Background(), "GetAPullRequest", map[string]any{})
	require.NoError(t, err)
	var claims jwt.RegisteredClaims
	_, err = jwt.ParseWithClaims(strings.TrimPrefix(authorization, "Signed "), &claims,
		func(*jwt.Token) (any, error) { return key.Public(), nil }, jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}))
	require.NoError(t, err)
	assert.Equal(t, jwt.ClaimStrings{q.URL}, claims.Audience)
}
