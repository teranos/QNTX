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
	// "it is approved, get it the permission it needs"
	// 「[Opus 5.5] The key stands as the person who approved it, at their level: ROOT, as」
	// 「[Opus 5.5] their approve at the passkey would have made it.」
	assert.Equal(t, string(LevelRoot), admitted.LevelName())
}

// "ok, but the fix can you please do it now"
// 「[Opus 5.5] A key already held, approved again by the person at laye, is held as the」
// 「[Opus 5.5] person from then on: on kapteyn on 2026-11-28 Keeper's key stood at SUPER and」
// 「[Opus 5.5] the vault, ROOT's, said "this route is not yours"; the approve came back 409.」
func TestAKeyApprovedAgainAtLayeIsHeldAsThePerson(t *testing.T) {
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

	keeper, keeperDID := aSessionsKey(t)
	_, err = node.tokens.Issue(IssuedToken{Hash: signedOnly(keeperDID), DID: keeperDID, Label: "Keeper",
		MintedBy: mastodonAccount, Level: LevelSuper, Namespaces: []string{"ground"}, ExpiresAt: NeverEnds()})
	require.NoError(t, err)
	held, ok := presentedSigned(node, calling(t, keeper, keeperDID, "Keeper", node.publicOrigin()))
	require.True(t, ok)
	require.Equal(t, string(LevelSuper), held.LevelName())

	w := layeAsks(t, primary, laye, layeDID, layeAdmit{Person: mastodonAccount, Peer: nodeDID, DID: keeperDID, Label: "Keeper"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	admitted, ok := presentedSigned(node, calling(t, keeper, keeperDID, "Keeper", node.publicOrigin()))
	require.True(t, ok, "the key approved again is not admitted")
	assert.Equal(t, mastodonAccount, admitted.Identity)
	assert.Equal(t, string(LevelRoot), admitted.LevelName())
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
