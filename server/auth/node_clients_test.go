package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
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

// A peer is a client of the primary, declared in nodes.peers by its own
// did:key, and it proves itself by signing with that key. No secret exists anywhere.

// otherNode is a node's DID key: what it publishes at /.well-known/did.json.
func otherNode(t *testing.T) (ed25519.PrivateKey, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	return priv, EncodeDIDKey(pub)
}

// registered is a node with one other node declared as its client.
func registered(t *testing.T) (*Handler, *memTokenStore, ed25519.PrivateKey, string) {
	t.Helper()
	h, store, _ := authorizingHandler(t)
	key, did := otherNode(t)
	require.NoError(t, h.DeclarePeer(peerName, did, elsewhereOrigin))
	return h, store, key, did
}

// peerName is what the primary calls its peer.
const peerName = "elsewhere"

// nodeReturn is where a node is sent back the people it sent to log in.
const nodeReturn = elsewhereOrigin + elsewhereBackPath

// assertion is what a node signs to prove itself: about itself, by itself,
// for this node's token endpoint.
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

// asking is the node asking about a token nobody issued, signed as given.
func asking(h *Handler, did, signed string) int {
	w := httptest.NewRecorder()
	h.handleIntrospect(w, introspectRequest(did, signed, "qntx_"+strings.Repeat("a", 64)))
	return w.Code
}

// A captured assertion is not asked with twice.
func TestAnAssertionPresentedTwiceIsRefused(t *testing.T) {
	h, _, key, did := registered(t)
	signed := assertion(t, key, did, nodeOrigin+tokenPath, "one", time.Now().Add(time.Minute))
	require.Equal(t, http.StatusOK, asking(h, did, signed))
	assert.Equal(t, http.StatusUnauthorized, asking(h, did, signed))
}

// An assertion made out to another node is not spent here.
func TestAnAssertionForAnotherNodeIsRefused(t *testing.T) {
	h, _, key, did := registered(t)
	assert.Equal(t, http.StatusUnauthorized, asking(h, did, assertion(t, key, did, "https://other.example"+tokenPath, "one", time.Now().Add(time.Minute))))
}

// A signature that lasts an hour is an hour to replay it in.
func TestAnAssertionLastingPastItsWindowIsRefused(t *testing.T) {
	h, _, key, did := registered(t)
	assert.Equal(t, http.StatusUnauthorized, asking(h, did, assertion(t, key, did, nodeOrigin+tokenPath, "one", time.Now().Add(time.Hour))))
}

// A node not in nodes.peers is nobody here, whatever it signs.
func TestANodeNotDeclaredIsRefused(t *testing.T) {
	h, _, _ := authorizingHandler(t)
	key, did := otherNode(t)
	assert.Equal(t, http.StatusUnauthorized, asking(h, did, assertion(t, key, did, nodeOrigin+tokenPath, "one", time.Now().Add(time.Minute))))
}

// No client asks for a token of its own, a node or an app.
func TestNoClientGetsATokenOfItsOwn(t *testing.T) {
	h, store, appDID := authorizingHandler(t)
	key, nodeDID := otherNode(t)
	require.NoError(t, h.DeclarePeer(peerName, nodeDID, elsewhereOrigin))

	app := url.Values{}
	app.Set("grant_type", "client_credentials")
	r := httptest.NewRequest(http.MethodPost, tokenPath, strings.NewReader(app.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.SetBasicAuth(url.QueryEscape(appDID), url.QueryEscape(clientSecret(t, store, appDID)))
	w := httptest.NewRecorder()
	h.handleToken(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code, w.Body.String())

	node := url.Values{}
	node.Set("grant_type", "client_credentials")
	node.Set("client_id", nodeDID)
	node.Set("client_assertion_type", clientAssertionType)
	node.Set("client_assertion", assertion(t, key, nodeDID, nodeOrigin+tokenPath, "one", time.Now().Add(time.Minute)))
	r = httptest.NewRequest(http.MethodPost, tokenPath, strings.NewReader(node.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	h.handleToken(w, r)
	assert.NotEqual(t, http.StatusOK, w.Code, w.Body.String())
}

// A node is sent the people it sent to log in, and only at its own origin.
func TestANodeIsSentPeopleBackOnlyAtItsOrigin(t *testing.T) {
	h, _, _, did := registered(t)
	_, challenge := pkcePair()
	w := httptest.NewRecorder()
	h.handleAuthorize(w, authorizeRequest(did, appReturn, challenge))
	assert.NotEqual(t, http.StatusFound, w.Code, w.Body.String())

	home := httptest.NewRecorder()
	h.handleAuthorize(home, authorizeRequest(did, nodeReturn, challenge))
	assert.Equal(t, http.StatusFound, home.Code, home.Body.String())
}

// What nodes.peers refuses, at startup.
func TestDeclaringAPeerSaysWhatItRefuses(t *testing.T) {
	h, _, _ := authorizingHandler(t)
	_, did := otherNode(t)
	_, other := otherNode(t)
	assert.Error(t, h.DeclarePeer(peerName, "did:web:node.example", elsewhereOrigin), "not a did:key")
	assert.Error(t, h.DeclarePeer(peerName, "", elsewhereOrigin), "no did")
	assert.Error(t, h.DeclarePeer(peerName, did, ""), "no origin")
	assert.Error(t, h.DeclarePeer(peerName, did, "elsewhere.node.test"), "not an absolute origin")
	assert.Error(t, h.DeclarePeer(peerName, did, elsewhereOrigin+"/somewhere"), "a path is no origin")
	assert.Error(t, h.DeclarePeer("", did, elsewhereOrigin), "no name")
	assert.Error(t, h.DeclarePeer("a/b", did, elsewhereOrigin), "a name a path cannot carry")
	require.NoError(t, h.DeclarePeer(peerName, did, elsewhereOrigin+"/"))
	assert.Error(t, h.DeclarePeer("again", did, elsewhereOrigin), "the same peer twice")
	assert.Error(t, h.DeclarePeer(peerName, other, elsewhereOrigin), "two peers by one name")

	client, ok := h.clientByDID(did)
	require.True(t, ok)
	assert.Equal(t, nodeReturn, client.ReturnAddress)
}
