package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// standInGoogle points the exchange at a server this test controls, and puts
// the real endpoints back afterwards.
func standInGoogle(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	tokenWas, whoWas := googleTokenURL, googleWhoURL
	googleTokenURL = server.URL + "/token"
	googleWhoURL = server.URL + "/v1/userinfo"
	t.Cleanup(func() {
		googleTokenURL, googleWhoURL = tokenWas, whoWas
		server.Close()
	})
}

// The consent URL carries the operator's client and this node's redirect, and
// nothing the browser wrote.
func TestGoogleAuthorizeCarriesTheOperatorsClient(t *testing.T) {
	p := googleProvider(googleClient{ID: "client-id", Secret: "client-secret"})

	url, state, err := p.authorize(context.Background(), googleAuthHost, "https://api.example.com/auth/binding/callback")
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(url, "https://accounts.google.com/o/oauth2/v2/auth?"), url)
	assert.Contains(t, url, "client_id=client-id")
	assert.Contains(t, url, "redirect_uri=https%3A%2F%2Fapi.example.com%2Fauth%2Fbinding%2Fcallback")
	assert.Contains(t, url, "response_type=code")
	assert.Contains(t, url, "scope=openid+email")

	// The secret goes into the state, never into the URL the person follows.
	assert.NotContains(t, url, "client-secret")
	assert.Equal(t, "client-secret", state.ClientSecret)
	assert.Equal(t, "client-id", state.ClientID)
}

// sub is the identity, qualified so am.toml says what it is. The email is what
// a person recognises, and Google lets it be reassigned, so it is only a handle.
func TestGoogleExchangeReturnsAQualifiedSub(t *testing.T) {
	var sentTo string
	standInGoogle(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			require.NoError(t, r.ParseForm())
			assert.Equal(t, "client-id", r.PostForm.Get("client_id"))
			assert.Equal(t, "client-secret", r.PostForm.Get("client_secret"))
			assert.Equal(t, "authorization_code", r.PostForm.Get("grant_type"))
			assert.Equal(t, "the-code", r.PostForm.Get("code"))
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "spent-once"})
		case "/v1/userinfo":
			sentTo = r.Header.Get("Authorization")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"sub":   "110169484474386276334",
				"email": "someone@gmail.com",
			})
		default:
			t.Errorf("the exchange asked for %s, which Google does not serve", r.URL.Path)
		}
	})

	acct, err := googleExchange(
		context.Background(),
		providerState{ClientID: "client-id", ClientSecret: "client-secret"},
		"the-code",
		"https://api.example.com/auth/binding/callback",
	)
	require.NoError(t, err)

	assert.Equal(t, "google:110169484474386276334", acct.CanonicalID)
	assert.Equal(t, "someone@gmail.com", acct.Handle)
	assert.Equal(t, "Bearer spent-once", sentTo)
}

// Without a sub there is nothing to match against auth.root_identities, and an
// account keyed on a reassignable email is a door that changes hands.
func TestGoogleExchangeRefusesUserinfoWithNoSub(t *testing.T) {
	standInGoogle(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "spent-once"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"email": "someone@gmail.com"})
	})

	_, err := googleExchange(
		context.Background(),
		providerState{ClientID: "client-id", ClientSecret: "client-secret"},
		"the-code",
		"https://api.example.com/auth/binding/callback",
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "carries no sub")
}
