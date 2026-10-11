package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A node is a client of another node, registered by its own did:key, and it
// proves itself by signing with that key. No secret exists anywhere.

// otherNode is a node's DID key: what it publishes at /.well-known/did.json.
func otherNode(t *testing.T) (ed25519.PrivateKey, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	return priv, EncodeDIDKey(pub)
}

// registered is a node with one other node registered as its client, in the
// ground namespace, by the identity am.toml admits.
func registered(t *testing.T) (*Handler, *memTokenStore, ed25519.PrivateKey, string) {
	t.Helper()
	h, store, _ := authorizingHandler(t)
	key, did := otherNode(t)
	session, err := h.sessions.create(mastodonAccount, User{ID: "US-1", DisplayName: "onf"})
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	mint(h, rec, mintRequest(`{"label":"kapteyn","level":"OAUTH","did":"`+did+`","namespaces":["ground"],"expires_at":"never"}`, session))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	return h, store, key, did
}

// assertion is what a node signs to ask for its token: about itself, by
// itself, for this node's token endpoint.
func assertion(t *testing.T, key ed25519.PrivateKey, did, audience, jti string, expires time.Time) string {
	t.Helper()
	signed, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.RegisteredClaims{
		Issuer:    did,
		Subject:   did,
		Audience:  jwt.ClaimStrings{audience},
		ExpiresAt: jwt.NewNumericDate(expires),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ID:        jti,
	}).SignedString(key)
	require.NoError(t, err)
	return signed
}

func credentialsRequest(did, signed string) *http.Request {
	form := url.Values{}
	form.Set("grant_type", grantClientCredentials)
	form.Set("client_id", did)
	form.Set("client_assertion_type", clientAssertionType)
	form.Set("client_assertion", signed)
	r := httptest.NewRequest(http.MethodPost, tokenPath, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func accessTokenOf(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var answer struct {
		AccessToken string `json:"access_token"`
		Refresh     string `json:"refresh_token"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &answer))
	assert.Empty(t, answer.Refresh, "a node asks again with its key; it is handed no refresh token")
	return answer.AccessToken
}

// The whole of it: the node signs as its did:key, gets a token, and the token
// is admitted as the node — by its client's name, where its client was put.
func TestANodeSigningAsItsKeyGetsATokenThatIsTheNode(t *testing.T) {
	h, store, key, did := registered(t)

	w := httptest.NewRecorder()
	h.handleToken(w, credentialsRequest(did, assertion(t, key, did, nodeOrigin+tokenPath, "one", time.Now().Add(time.Minute))))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	token := accessTokenOf(t, w)

	grant, live := store.Lookup(sha256Hex(token))
	require.True(t, live, "the node's token does not authenticate")
	assert.Equal(t, "kapteyn", grant.Label)
	assert.Equal(t, LevelAttestor, grant.Level)
	assert.Equal(t, []string{"ground"}, grant.Namespaces)
	assert.Equal(t, mastodonAccount, grant.MintedBy, "a node answers to whoever registered it")
	assert.Empty(t, grant.ClientDID, "a node's token would be admitted as the person who registered it")

	req := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	admission, admitted := h.admissionOf(h.presented(req))
	require.True(t, admitted, "the node's token is not admitted as a bearer")
	assert.Equal(t, []string{"ground"}, admission.Namespaces)
	require.NotNil(t, admission.Grant, "the node was admitted as a person rather than as itself")
	assert.Equal(t, "kapteyn", admission.Grant.Label)
}

// Anyone can name a did:key. Only its holder can sign as it.
func TestAnAssertionSignedByAnotherKeyGetsNoToken(t *testing.T) {
	h, store, _, did := registered(t)
	stranger, _ := otherNode(t)
	before, err := store.List()
	require.NoError(t, err)

	w := httptest.NewRecorder()
	h.handleToken(w, credentialsRequest(did, assertion(t, stranger, did, nodeOrigin+tokenPath, "one", time.Now().Add(time.Minute))))
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "invalid_client")

	after, err := store.List()
	require.NoError(t, err)
	assert.Len(t, after, len(before), "a token was written for a stranger")
}

// A captured assertion is not a second token.
func TestAnAssertionPresentedTwiceGetsOneToken(t *testing.T) {
	h, _, key, did := registered(t)
	signed := assertion(t, key, did, nodeOrigin+tokenPath, "one", time.Now().Add(time.Minute))

	first := httptest.NewRecorder()
	h.handleToken(first, credentialsRequest(did, signed))
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())

	again := httptest.NewRecorder()
	h.handleToken(again, credentialsRequest(did, signed))
	assert.NotEqual(t, http.StatusOK, again.Code, again.Body.String())
}

// An assertion made out to another node is not spent here.
func TestAnAssertionForAnotherNodeGetsNoToken(t *testing.T) {
	h, _, key, did := registered(t)
	w := httptest.NewRecorder()
	h.handleToken(w, credentialsRequest(did, assertion(t, key, did, "https://other.example"+tokenPath, "one", time.Now().Add(time.Minute))))
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
}

// A signature that lasts an hour is an hour to replay it in.
func TestAnAssertionLastingPastItsWindowGetsNoToken(t *testing.T) {
	h, _, key, did := registered(t)
	w := httptest.NewRecorder()
	h.handleToken(w, credentialsRequest(did, assertion(t, key, did, nodeOrigin+tokenPath, "one", time.Now().Add(time.Hour))))
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
}

// A node answers to whoever registered it. Struck out of am.toml, the node is
// given nothing, and what it already holds is nobody.
func TestANodeIsNobodyOnceWhoRegisteredItIs(t *testing.T) {
	h, _, key, did := registered(t)
	w := httptest.NewRecorder()
	h.handleToken(w, credentialsRequest(did, assertion(t, key, did, nodeOrigin+tokenPath, "one", time.Now().Add(time.Minute))))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	token := accessTokenOf(t, w)

	h.SetIdentities(nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	_, admitted := h.admissionOf(h.presented(req))
	assert.False(t, admitted, "the node's token outlived who registered it")

	again := httptest.NewRecorder()
	h.handleToken(again, credentialsRequest(did, assertion(t, key, did, nodeOrigin+tokenPath, "two", time.Now().Add(time.Minute))))
	assert.NotEqual(t, http.StatusOK, again.Code, again.Body.String())
}

// A client a person is sent through asks for no token of its own.
func TestAnAppClientGetsNoTokenOfItsOwn(t *testing.T) {
	h, store, did := authorizingHandler(t)
	w := httptest.NewRecorder()
	form := url.Values{}
	form.Set("grant_type", grantClientCredentials)
	r := httptest.NewRequest(http.MethodPost, tokenPath, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.SetBasicAuth(url.QueryEscape(did), url.QueryEscape(clientSecret(t, store, did)))
	h.handleToken(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code, w.Body.String())
}

// A node is sent no code, so nobody is sent home on its behalf.
func TestNobodyIsSentHomeForANode(t *testing.T) {
	h, _, _, did := registered(t)
	_, challenge := pkcePair()
	w := httptest.NewRecorder()
	h.handleAuthorize(w, authorizeRequest(did, appReturn, challenge))
	assert.NotEqual(t, http.StatusFound, w.Code, w.Body.String())
}

// What registering a node refuses.
func TestRegisteringANodeSaysWhatItRefuses(t *testing.T) {
	h, _, _ := authorizingHandler(t)
	_, did := otherNode(t)
	session, err := h.sessions.create(mastodonAccount, User{ID: "US-1", DisplayName: "onf"})
	require.NoError(t, err)

	for name, body := range map[string]string{
		"not a did:key":    `{"label":"n","level":"OAUTH","did":"did:web:kapteyn.example","namespaces":["ground"],"expires_at":"never"}`,
		"a return address": `{"label":"n","level":"OAUTH","did":"` + did + `","namespaces":["ground"],"return_address":"https://kapteyn.example/cb","expires_at":"never"}`,
		"two namespaces":   `{"label":"n","level":"OAUTH","did":"` + did + `","namespaces":["ground","default"],"expires_at":"never"}`,
		"not a client":     `{"label":"n","level":"ATTESTOR","did":"` + did + `","namespaces":["ground"],"expires_at":"never"}`,
		"no stated expiry": `{"label":"n","level":"OAUTH","did":"` + did + `","namespaces":["ground"]}`,
	} {
		rec := httptest.NewRecorder()
		mint(h, rec, mintRequest(body, session))
		assert.Equal(t, http.StatusBadRequest, rec.Code, name+": "+rec.Body.String())
	}
}

// The node's key is the only credential: registering it hands nothing back.
func TestRegisteringANodeHandsBackNoSecret(t *testing.T) {
	h, _, _ := authorizingHandler(t)
	_, did := otherNode(t)
	session, err := h.sessions.create(mastodonAccount, User{ID: "US-1", DisplayName: "onf"})
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	mint(h, rec, mintRequest(`{"label":"kapteyn","level":"OAUTH","did":"`+did+`","namespaces":["ground"],"expires_at":"never"}`, session))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var answer map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &answer))
	assert.NotContains(t, answer, "token")
	assert.Equal(t, did, answer["did"])

	again := httptest.NewRecorder()
	mint(h, again, mintRequest(`{"label":"kapteyn-again","level":"OAUTH","did":"`+did+`","namespaces":["ground"],"expires_at":"never"}`, session))
	assert.Equal(t, http.StatusConflict, again.Code, again.Body.String())
}
