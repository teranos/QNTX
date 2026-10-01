package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// standInGitHub points the exchange at a server this test controls, and puts
// the real endpoints back afterwards.
func standInGitHub(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	tokenWas, whoWas := githubTokenURL, githubWhoURL
	githubTokenURL = server.URL + "/login/oauth/access_token"
	githubWhoURL = server.URL + "/user"
	t.Cleanup(func() {
		githubTokenURL, githubWhoURL = tokenWas, whoWas
		server.Close()
	})
}

// The consent URL carries the operator's client, this node's redirect and a
// PKCE challenge whose verifier stays in the state.
func TestGitHubAuthorizeCarriesTheOperatorsClientAndAChallenge(t *testing.T) {
	p := githubProvider(OperatorClient{ID: "client-id", Secret: "client-secret"})

	authorize, state, err := p.authorize(context.Background(), githubAuthHost, "https://api.example.com/auth/binding/callback")
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(authorize, "https://github.com/login/oauth/authorize?"), authorize)
	parsed, err := url.Parse(authorize)
	require.NoError(t, err)
	q := parsed.Query()
	assert.Equal(t, "client-id", q.Get("client_id"))
	assert.Equal(t, "https://api.example.com/auth/binding/callback", q.Get("redirect_uri"))
	assert.Equal(t, "S256", q.Get("code_challenge_method"))
	assert.Empty(t, q.Get("scope"), "GET /user needs no scope")

	require.NotEmpty(t, state.Verifier)
	sum := sha256.Sum256([]byte(state.Verifier))
	assert.Equal(t, base64.RawURLEncoding.EncodeToString(sum[:]), q.Get("code_challenge"))

	// The secret and the verifier go into the state, never into the URL.
	assert.NotContains(t, authorize, "client-secret")
	assert.NotContains(t, authorize, state.Verifier)
	assert.Equal(t, "client-secret", state.ClientSecret)
}

// The id is the identity, qualified so am.toml says what it is. The login is
// renamed and released, so it is only the handle.
func TestGitHubExchangeReturnsAQualifiedID(t *testing.T) {
	var sentTo string
	standInGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/oauth/access_token":
			require.NoError(t, r.ParseForm())
			assert.Equal(t, "application/json", r.Header.Get("Accept"))
			assert.Equal(t, "client-id", r.PostForm.Get("client_id"))
			assert.Equal(t, "client-secret", r.PostForm.Get("client_secret"))
			assert.Equal(t, "the-code", r.PostForm.Get("code"))
			assert.Equal(t, "the-verifier", r.PostForm.Get("code_verifier"))
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "spent-once"})
		case "/user":
			sentTo = r.Header.Get("Authorization")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         1739143,
				"login":      "teranos",
				"name":       "Brandon",
				"avatar_url": "https://avatars.githubusercontent.com/u/1739143?v=4",
			})
		default:
			t.Errorf("the exchange asked for %s, which GitHub does not serve", r.URL.Path)
		}
	})

	acct, err := githubExchange(
		context.Background(),
		providerState{ClientID: "client-id", ClientSecret: "client-secret", Verifier: "the-verifier"},
		"the-code",
		"https://api.example.com/auth/binding/callback",
	)
	require.NoError(t, err)

	assert.Equal(t, "github:1739143", acct.CanonicalID)
	assert.Equal(t, "teranos", acct.Handle)
	assert.Equal(t, "Brandon", acct.Name)
	assert.Equal(t, "https://avatars.githubusercontent.com/u/1739143?v=4", acct.Picture)
	assert.Equal(t, "Bearer spent-once", sentTo)
}

// GitHub answers a refused code with an error in the body, and the refusal
// says what GitHub said.
func TestGitHubExchangeNamesTheRefusal(t *testing.T) {
	standInGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":             "bad_verification_code",
			"error_description": "The code passed is incorrect or expired.",
		})
	})

	_, err := githubExchange(context.Background(), providerState{ClientID: "client-id"}, "stale", "https://api.example.com/auth/binding/callback")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad_verification_code")
}

// Without an id there is nothing to match against auth.root_identities.
func TestGitHubExchangeRefusesAUserWithNoID(t *testing.T) {
	standInGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login/oauth/access_token" {
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "spent-once"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"login": "teranos"})
	})

	_, err := githubExchange(context.Background(), providerState{ClientID: "client-id"}, "the-code", "https://api.example.com/auth/binding/callback")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "carries no id")
}

// A GitHub button on a node holding no OAuth client is a button that can only
// fail, so it is not drawn.
func TestGitHubIsOfferedOnlyOnceConfigured(t *testing.T) {
	h := &Handler{}
	_, known := h.providerAt(NamespaceDefault, "github")
	assert.False(t, known, "github before it is configured")

	h.SetGitHubClient("client-id", "client-secret")
	p, known := h.providerAt(NamespaceDefault, "github")
	require.True(t, known, "github once configured")
	assert.Equal(t, githubAuthHost, hostFor(p, "evil.example.com"))

	h.SetGitHubClient("client-id", "")
	_, known = h.providerAt(NamespaceDefault, "github")
	assert.False(t, known, "github with half a client")
}
