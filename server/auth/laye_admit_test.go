package auth

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "CAN WE PLEASE STOP EXPECTING AN AGENT TO REQUEST IT OWN PERMISSIONS AND ALSO BE PERFECT?"

// servedNode is a handler serving its own routes, and where.
func servedNode(t *testing.T, h *Handler) string {
	t.Helper()
	mux := http.NewServeMux()
	for path, handle := range h.Routes() {
		mux.HandleFunc(path, handle)
	}
	served := httptest.NewServer(mux)
	t.Cleanup(served.Close)
	return served.URL
}

// layeAsks is laye's request to the primary for what its person approved.
func layeAsks(t *testing.T, primary *Handler, laye ed25519.PrivateKey, layeDID string, asked layeAdmit) *httptest.ResponseRecorder {
	t.Helper()
	primaryDID, err := primary.ownDID()
	require.NoError(t, err)
	body, err := json.Marshal(asked)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, layeAdmitPath, bytes.NewReader(body))
	req.Header.Set("Authorization", signedScheme+carriedBy(t, laye, layeDID, layeDID, primaryDID))
	w := httptest.NewRecorder()
	primary.handleLayeAdmit(w, req)
	return w
}

// The person approved ramen's key at laye: laye asks the primary, the primary
// carries the person to the peer, and the peer holds the key as theirs.
func TestAKeyApprovedAtLayeIsAdmittedOnThePeer(t *testing.T) {
	primary, _, _ := authorizingHandler(t)
	primaryURL := servedNode(t, primary)
	primary.configuredOrigin = primaryURL
	_, laye, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	layeDID := EncodeDIDKey(laye.Public().(ed25519.PublicKey))
	require.NoError(t, primary.SetLaye(layeDID))

	nodeKey, nodeDID := otherNode(t)
	node := handlerWithDoors(t)
	node.tokens = newMemTokenStore()
	node.nodeKey = nodeKey
	require.NoError(t, node.SetPrimary(primaryURL))
	nodeURL := servedNode(t, node)
	require.NoError(t, primary.DeclarePeer(peerName, nodeDID, []string{"ground"}, nodeURL, nodeURL))

	ramen, ramenDID := aSessionsKey(t)
	w := layeAsks(t, primary, laye, layeDID, layeAdmit{Person: mastodonAccount, Peer: nodeDID, DID: ramenDID, Label: "ramen"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	admitted, ok := presentedSigned(node, calling(t, ramen, ramenDID, "ramen", node.publicOrigin()))
	require.True(t, ok, "the key approved at laye is not admitted on the peer")
	assert.Equal(t, mastodonAccount, admitted.Identity)
	assert.Equal(t, string(LevelSuper), admitted.LevelName())
	assert.Equal(t, []string{"ground"}, admitted.Namespaces)
}

// Only ROOT here admits a key on a peer.
func TestSomebodyNotRootAdmitsNoKeyOnAPeer(t *testing.T) {
	primary, _, _ := authorizingHandler(t)
	_, laye, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	layeDID := EncodeDIDKey(laye.Public().(ed25519.PublicKey))
	require.NoError(t, primary.SetLaye(layeDID))
	_, ramenDID := aSessionsKey(t)
	w := layeAsks(t, primary, laye, layeDID, layeAdmit{Person: "https://mastodon.example/@nobody", Peer: peerName, DID: ramenDID, Label: "ramen"})
	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
}

// What laye did not sign admits nothing.
func TestWhatLayeDidNotSignAdmitsNoKey(t *testing.T) {
	primary, _, _ := authorizingHandler(t)
	_, laye, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	require.NoError(t, primary.SetLaye(EncodeDIDKey(laye.Public().(ed25519.PublicKey))))
	other, otherDID := aSessionsKey(t)
	_, ramenDID := aSessionsKey(t)
	w := layeAsks(t, primary, other, otherDID, layeAdmit{Person: mastodonAccount, Peer: peerName, DID: ramenDID, Label: "ramen"})
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
}
