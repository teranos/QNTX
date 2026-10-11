package auth

import (
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A node asks the node that issued a token whether it is live, signing as
// itself. What comes back is what the issuing node's gate admits it as.

func introspectRequest(did, signed, token string) *http.Request {
	form := url.Values{}
	form.Set("token", token)
	form.Set("client_id", did)
	form.Set("client_assertion_type", clientAssertionType)
	form.Set("client_assertion", signed)
	r := httptest.NewRequest(http.MethodPost, introspectPath, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func introspected(t *testing.T, h *Handler, key ed25519.PrivateKey, did, jti, token string) introspection {
	t.Helper()
	w := httptest.NewRecorder()
	h.handleIntrospect(w, introspectRequest(did, assertion(t, key, did, nodeOrigin+tokenPath, jti, time.Now().Add(time.Minute)), token))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var answer introspection
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &answer))
	return answer
}

// mintedIn is an ATTESTOR token minted here, acting in one namespace.
func mintedIn(t *testing.T, store *memTokenStore, label, namespace string) string {
	t.Helper()
	raw, _, err := store.Create(NewToken{
		Label: label, MintedBy: mastodonAccount, Level: LevelAttestor,
		Namespaces: []string{namespace}, ExpiresAt: NeverEnds(),
	})
	require.NoError(t, err)
	return raw
}

// A token acting where the asking node was registered: live, and who.
func TestANodeLearnsWhoATokenInItsNamespaceIs(t *testing.T) {
	h, store, key, did := registered(t)
	token := mintedIn(t, store, "ground-writer", "ground")

	answer := introspected(t, h, key, did, "one", token)
	assert.True(t, answer.Active)
	assert.Equal(t, nodeOrigin, answer.Issuer)
	assert.Equal(t, mastodonAccount, answer.Subject)
	assert.Equal(t, []string{"ground"}, answer.Namespaces)
	assert.Equal(t, "ground-writer", answer.Label)
	assert.Equal(t, string(LevelAttestor), answer.Level)
	assert.Equal(t, "Bearer", answer.TokenType)
}

// A token acting elsewhere is none of the asking node's business.
func TestANodeLearnsNothingOfATokenElsewhere(t *testing.T) {
	h, store, key, did := registered(t)
	token := mintedIn(t, store, "vak-writer", "vakconnectie")

	answer := introspected(t, h, key, did, "one", token)
	assert.False(t, answer.Active)
	assert.Empty(t, answer.Subject, "an inactive answer said who")
}

// Revoked here is revoked wherever it is presented.
func TestARevokedTokenIsInactiveToTheAskingNode(t *testing.T) {
	h, store, key, did := registered(t)
	token := mintedIn(t, store, "ground-writer", "ground")
	grant, live := store.Lookup(sha256Hex(token))
	require.True(t, live)
	require.NoError(t, store.Revoke(grant.ID))

	assert.False(t, introspected(t, h, key, did, "one", token).Active)
}

// A token nobody issued is inactive, and says nothing more.
func TestATokenNobodyIssuedIsInactive(t *testing.T) {
	h, _, key, did := registered(t)
	assert.False(t, introspected(t, h, key, did, "one", "qntx_"+strings.Repeat("a", 64)).Active)
}

// Only a node that proves itself asks.
func TestAskingWithoutSigningIsRefused(t *testing.T) {
	h, store, _, _ := registered(t)
	token := mintedIn(t, store, "ground-writer", "ground")
	form := url.Values{}
	form.Set("token", token)
	r := httptest.NewRequest(http.MethodPost, introspectPath, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.handleIntrospect(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
}

// Another key naming the node is not the node.
func TestAskingAsANodeWithAnotherKeyIsRefused(t *testing.T) {
	h, store, _, did := registered(t)
	stranger, _ := otherNode(t)
	token := mintedIn(t, store, "ground-writer", "ground")
	w := httptest.NewRecorder()
	h.handleIntrospect(w, introspectRequest(did, assertion(t, stranger, did, nodeOrigin+tokenPath, "one", time.Now().Add(time.Minute)), token))
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
}
