package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
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
	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	registerAt(h, rec, nodeRegistration("kapteyn", did), session)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	return h, store, key, did
}

// nodeReturn is where a node is sent back the people it sent to log in.
const nodeReturn = kapteynOrigin + elsewhereBackPath

func nodeRegistration(label, did string) string {
	return `{"label":"` + label + `","did":"` + did + `","namespace":"ground","return_address":"` + nodeReturn + `","expires_at":"never"}`
}

// registerAt is POST /auth/nodes, behind the session gate it is routed behind.
func registerAt(h *Handler, rec *httptest.ResponseRecorder, body, session string) {
	req := httptest.NewRequest(http.MethodPost, nodesPath, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	h.sessionOnly(h.handleRegisterNode)(rec, req)
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
		ID:        assertionID(jti),
	}).SignedString(key)
	require.NoError(t, err)
	return signed
}

// assertionID is the 16 bytes a node draws for an assertion, made from a name
// here so a test can present the same one twice.
func assertionID(name string) string {
	sum := sha256.Sum256([]byte(name))
	return hex.EncodeToString(sum[:assertionIDBytes])
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

// A node is sent the people it sent to log in, and only at its own return address.
func TestANodeIsSentPeopleBackOnlyWhereItWasRegistered(t *testing.T) {
	h, _, _, did := registered(t)
	_, challenge := pkcePair()
	w := httptest.NewRecorder()
	h.handleAuthorize(w, authorizeRequest(did, appReturn, challenge))
	assert.NotEqual(t, http.StatusFound, w.Code, w.Body.String())

	home := httptest.NewRecorder()
	h.handleAuthorize(home, authorizeRequest(did, nodeReturn, challenge))
	assert.Equal(t, http.StatusFound, home.Code, home.Body.String())
}

// What registering a node refuses.
func TestRegisteringANodeSaysWhatItRefuses(t *testing.T) {
	h, _, _ := authorizingHandler(t)
	_, did := otherNode(t)
	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)

	back := `"return_address":"` + nodeReturn + `",`
	for name, body := range map[string]string{
		"not a did:key":       `{"label":"n","did":"did:web:kapteyn.example","namespace":"ground",` + back + `"expires_at":"never"}`,
		"no did":              `{"label":"n","namespace":"ground",` + back + `"expires_at":"never"}`,
		"no namespace":        `{"label":"n","did":"` + did + `",` + back + `"expires_at":"never"}`,
		"no label":            `{"did":"` + did + `","namespace":"ground",` + back + `"expires_at":"never"}`,
		"no stated expiry":    `{"label":"n","did":"` + did + `","namespace":"ground",` + back[:len(back)-1] + `}`,
		"no return address":   `{"label":"n","did":"` + did + `","namespace":"ground","expires_at":"never"}`,
		"a relative return":   `{"label":"n","did":"` + did + `","namespace":"ground","return_address":"/auth/elsewhere/back","expires_at":"never"}`,
	} {
		rec := httptest.NewRecorder()
		registerAt(h, rec, body, session)
		assert.Equal(t, http.StatusBadRequest, rec.Code, name+": "+rec.Body.String())
	}
}

// Registering a node is a session's, and a bearer token registers nothing.
func TestABearerRegistersNoNode(t *testing.T) {
	h, store, _ := authorizingHandler(t)
	_, did := otherNode(t)
	token := mintedIn(t, store, "ingest", "ground")
	req := httptest.NewRequest(http.MethodPost, nodesPath, strings.NewReader(nodeRegistration("kapteyn", did)))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.sessionOnly(h.handleRegisterNode)(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
}

// The node's key is the only credential: registering it hands nothing back.
func TestRegisteringANodeHandsBackNoSecret(t *testing.T) {
	h, _, _ := authorizingHandler(t)
	_, did := otherNode(t)
	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	registerAt(h, rec, nodeRegistration("kapteyn", did), session)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var answer map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &answer))
	assert.NotContains(t, answer, "token")
	assert.Equal(t, did, answer["did"])
	assert.Equal(t, string(LevelNode), answer["level"])

	again := httptest.NewRecorder()
	registerAt(h, again, nodeRegistration("kapteyn-again", did), session)
	assert.Equal(t, http.StatusConflict, again.Code, again.Body.String())
}
