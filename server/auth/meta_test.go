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

// standInMeta points the exchange at a server this test controls, and puts the
// real endpoints back afterwards.
func standInMeta(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	tokenWas, whoWas := metaTokenURL, metaWhoURL
	metaTokenURL = server.URL + "/oauth/access_token"
	metaWhoURL = server.URL + "/me"
	t.Cleanup(func() {
		metaTokenURL, metaWhoURL = tokenWas, whoWas
		server.Close()
	})
}

// The consent URL carries the operator's app and this node's redirect, and
// nothing the browser wrote.
func TestMetaAuthorizeCarriesTheOperatorsClient(t *testing.T) {
	p := metaProvider(OperatorClient{ID: "app-id", Secret: "app-secret"})

	url, state, err := p.authorize(context.Background(), metaAuthHost, "https://api.example.com/auth/binding/callback")
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(url, "https://www.facebook.com/v21.0/dialog/oauth?"), url)
	assert.Contains(t, url, "client_id=app-id")
	assert.Contains(t, url, "redirect_uri=https%3A%2F%2Fapi.example.com%2Fauth%2Fbinding%2Fcallback")
	assert.Contains(t, url, "response_type=code")

	// The secret goes into the state, never into the URL the person follows.
	assert.NotContains(t, url, "app-secret")
	assert.Equal(t, "app-secret", state.ClientSecret)
	assert.Equal(t, "app-id", state.ClientID)
}

// id is the identity, qualified so am.toml says what it is. The address is a
// handle: Meta lets it change, and two providers both issuing bare numbers
// would collide without the prefix.
func TestMetaExchangeReturnsAQualifiedID(t *testing.T) {
	var sentTo string
	standInMeta(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/access_token":
			assert.Equal(t, "app-id", r.URL.Query().Get("client_id"))
			assert.Equal(t, "app-secret", r.URL.Query().Get("client_secret"))
			assert.Equal(t, "the-code", r.URL.Query().Get("code"))
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "spent-once"})
		case "/me":
			sentTo = r.Header.Get("Authorization")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"id":    "10223372036854775807",
				"email": "someone@example.com",
			})
		default:
			t.Errorf("the exchange asked for %s, which Meta does not serve", r.URL.Path)
		}
	})

	acct, err := metaExchange(
		context.Background(),
		providerState{ClientID: "app-id", ClientSecret: "app-secret"},
		"the-code",
		"https://api.example.com/auth/binding/callback",
	)
	require.NoError(t, err)

	assert.Equal(t, "meta:10223372036854775807", acct.CanonicalID)
	assert.Equal(t, "someone@example.com", acct.Handle)
	assert.Equal(t, "Bearer spent-once", sentTo)
}

// "when a user registers, we attest it, and in it, an email address may be."
// May be — an account that never confirmed one still registers.
func TestMetaExchangeAcceptsAnAccountWithNoAddress(t *testing.T) {
	standInMeta(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/access_token" {
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "spent-once"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "10223372036854775807"})
	})

	acct, err := metaExchange(
		context.Background(),
		providerState{ClientID: "app-id", ClientSecret: "app-secret"},
		"the-code",
		"https://api.example.com/auth/binding/callback",
	)
	require.NoError(t, err)

	assert.Equal(t, "meta:10223372036854775807", acct.CanonicalID)
	assert.Empty(t, acct.Handle)
}

// Without an id there is nothing to match against auth.root_identities, and an
// account keyed on a reassignable address is a door that changes hands.
func TestMetaExchangeRefusesAnIdentityWithNoID(t *testing.T) {
	standInMeta(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/access_token" {
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "spent-once"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"email": "someone@example.com"})
	})

	_, err := metaExchange(
		context.Background(),
		providerState{ClientID: "app-id", ClientSecret: "app-secret"},
		"the-code",
		"https://api.example.com/auth/binding/callback",
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "carries no id")
}

// A node with no Meta app draws no Meta button, so nobody clicks a register
// button that could only fail.
func TestMetaIsNotOfferedUntilItIsConfigured(t *testing.T) {
	h := &Handler{logger: testLogger()}
	_, offered := h.providerAt(NamespaceDefault, "meta")
	assert.False(t, offered)

	h.SetMetaClient("app-id", "app-secret")
	p, offered := h.providerAt(NamespaceDefault, "meta")
	require.True(t, offered)
	assert.Equal(t, "Meta", p.Label)

	h.SetMetaClient("app-id", "")
	_, offered = h.providerAt(NamespaceDefault, "meta")
	assert.False(t, offered, "half an app is a button that fails at the exchange")
}
