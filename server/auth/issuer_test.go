package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Two nodes over HTTP: the issuer, and a node that accepts its tokens by
// asking it, signing as its own did:key. Nothing about the network is trusted.

// issuerAndNode is the issuer serving its routes, with the node declared at
// it, and the node configured to accept the issuer's tokens.
func issuerAndNode(t *testing.T) (issuer *Handler, store *memTokenStore, node *Handler) {
	t.Helper()
	issuer, store, key, _ := registered(t)
	mux := http.NewServeMux()
	for path, handle := range issuer.Routes() {
		mux.HandleFunc(path, handle)
	}
	served := httptest.NewServer(mux)
	t.Cleanup(served.Close)
	issuer.configuredOrigin = served.URL

	node = handlerWithDoors(t)
	node.tokens = newMemTokenStore()
	node.nodeKey = key
	node.SetIssuers([]string{served.URL})
	return issuer, store, node
}

func bearing(token string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

// A token the issuer minted for ground is admitted at the node, by the node.
func TestANodeAdmitsATokenItsIssuerVouchesFor(t *testing.T) {
	_, store, node := issuerAndNode(t)
	token := mintedIn(t, store, "ground-writer", NamespaceDefault)

	admission, admitted := node.admissionOf(node.presented(bearing(token)))
	require.True(t, admitted, "the issuer's token was not admitted at the node")
	assert.Equal(t, []string{NamespaceDefault}, admission.Namespaces)
	assert.Equal(t, mastodonAccount, admission.Identity)
	require.NotNil(t, admission.Grant)
	assert.Equal(t, "ground-writer", admission.Grant.Label)
}

// What the issuer revoked, the node does not admit.
func TestANodeDoesNotAdmitWhatItsIssuerRevoked(t *testing.T) {
	_, store, node := issuerAndNode(t)
	token := mintedIn(t, store, "ground-writer", NamespaceDefault)
	grant, live := store.Lookup(sha256Hex(token))
	require.True(t, live)
	require.NoError(t, store.Revoke(grant.ID))

	_, admitted := node.admissionOf(node.presented(bearing(token)))
	assert.False(t, admitted)
}

// A token for another namespace is not the node's to know of.
func TestANodeDoesNotAdmitATokenForElsewhere(t *testing.T) {
	_, store, node := issuerAndNode(t)
	token := mintedIn(t, store, "vak-writer", "vakconnectie")

	_, admitted := node.admissionOf(node.presented(bearing(token)))
	assert.False(t, admitted)
}

// The node decides who is in: the issuer vouching for a token whose minter
// this node does not admit is not enough.
func TestTheNodesOwnListDecides(t *testing.T) {
	_, store, node := issuerAndNode(t)
	token := mintedIn(t, store, "ground-writer", NamespaceDefault)
	node.SetIdentities(nil, nil)

	_, admitted := node.admissionOf(node.presented(bearing(token)))
	assert.False(t, admitted)
}

// A node the issuer does not hold as a client learns nothing.
func TestANodeTheIssuerDoesNotKnowAdmitsNothing(t *testing.T) {
	_, store, node := issuerAndNode(t)
	stranger, _ := otherNode(t)
	node.nodeKey = stranger
	token := mintedIn(t, store, "ground-writer", NamespaceDefault)

	_, admitted := node.admissionOf(node.presented(bearing(token)))
	assert.False(t, admitted)
}
