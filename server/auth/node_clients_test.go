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
	require.NoError(t, h.DeclarePeer(peerName, did, NamespaceDefault,elsewhereOrigin, elsewhereOrigin))
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

// An app asks for a token of its own (RFC 6749 §4.4); a node does not. It is
// sent codes for the people it sends here, and has never asked for itself.
func TestANodeGetsNoTokenOfItsOwn(t *testing.T) {
	h, _, _ := authorizingHandler(t)
	key, nodeDID := otherNode(t)
	require.NoError(t, h.DeclarePeer(peerName, nodeDID, NamespaceDefault, elsewhereOrigin, elsewhereOrigin))

	node := url.Values{}
	node.Set("grant_type", "client_credentials")
	node.Set("client_id", nodeDID)
	node.Set("client_assertion_type", clientAssertionType)
	node.Set("client_assertion", assertion(t, key, nodeDID, nodeOrigin+tokenPath, "one", time.Now().Add(time.Minute)))
	r := httptest.NewRequest(http.MethodPost, tokenPath, strings.NewReader(node.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
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

// A peer reached over a tunnel is opened at a loopback host by its own browser,
// and a code goes to plain http only there (RFC 8252 §8.3).
func TestAPeerOverATunnelIsSentPeopleBackAtItsReturnAddress(t *testing.T) {
	h, _, _ := authorizingHandler(t)
	_, did := otherNode(t)
	const tunnel, loopback = "http://10.0.0.2:8770", "http://peer.localhost:8770"
	require.NoError(t, h.DeclarePeer(peerName, did, NamespaceDefault,tunnel, loopback))
	_, challenge := pkcePair()

	refused := httptest.NewRecorder()
	h.handleAuthorize(refused, authorizeRequest(did, tunnel+elsewhereBackPath, challenge))
	assert.NotEqual(t, http.StatusFound, refused.Code, refused.Body.String())

	home := httptest.NewRecorder()
	h.handleAuthorize(home, authorizeRequest(did, loopback+elsewhereBackPath, challenge))
	assert.Equal(t, http.StatusFound, home.Code, home.Body.String())

	client, ok := h.clientByDID(did)
	require.True(t, ok)
	assert.Equal(t, tunnel, client.Origin.String(), "requests are still carried to the tunnel")
}

// What nodes.peers refuses, at startup.
func TestDeclaringAPeerSaysWhatItRefuses(t *testing.T) {
	h, _, _ := authorizingHandler(t)
	_, did := otherNode(t)
	_, other := otherNode(t)
	assert.Error(t, h.DeclarePeer(peerName, "did:web:node.example", NamespaceDefault, elsewhereOrigin, elsewhereOrigin), "not a did:key")
	assert.Error(t, h.DeclarePeer(peerName, "", NamespaceDefault, elsewhereOrigin, elsewhereOrigin), "no did")
	assert.Error(t, h.DeclarePeer(peerName, did, "", elsewhereOrigin, elsewhereOrigin), "no namespace")
	assert.Error(t, h.DeclarePeer(peerName, did, NamespaceDefault,"", elsewhereOrigin), "no origin")
	assert.Error(t, h.DeclarePeer(peerName, did, NamespaceDefault,"elsewhere.node.test", elsewhereOrigin), "not an absolute origin")
	assert.Error(t, h.DeclarePeer(peerName, did, NamespaceDefault,elsewhereOrigin+"/somewhere", elsewhereOrigin), "a path is no origin")
	assert.Error(t, h.DeclarePeer("", did, NamespaceDefault, elsewhereOrigin, elsewhereOrigin), "no name")
	assert.Error(t, h.DeclarePeer("a/b", did, NamespaceDefault, elsewhereOrigin, elsewhereOrigin), "a name a path cannot carry")
	assert.Error(t, h.DeclarePeer(peerName, did, NamespaceDefault,elsewhereOrigin, ""), "no return address")
	assert.Error(t, h.DeclarePeer(peerName, did, NamespaceDefault,elsewhereOrigin, "peer.localhost"), "a return address that is not an origin")
	assert.Error(t, h.DeclarePeer(peerName, did, NamespaceDefault,elsewhereOrigin, "http://peer.localhost/back"), "a return address with a path")
	require.NoError(t, h.DeclarePeer(peerName, did, NamespaceDefault,elsewhereOrigin+"/", elsewhereOrigin))
	assert.Error(t, h.DeclarePeer("again", did, NamespaceDefault, elsewhereOrigin, elsewhereOrigin), "the same peer twice")
	assert.Error(t, h.DeclarePeer(peerName, other, NamespaceDefault, elsewhereOrigin, elsewhereOrigin), "two peers by one name")

	client, ok := h.clientByDID(did)
	require.True(t, ok)
	assert.Equal(t, nodeReturn, client.ReturnAddress)
}
