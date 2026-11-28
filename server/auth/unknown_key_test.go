package auth

import (
	"crypto/ed25519"
	"crypto/rand"
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

// "CAN WE PLEASE STOP EXPECTING AN AGENT TO REQUEST IT OWN PERMISSIONS AND ALSO BE PERFECT?"
// "yeah, it jus tfeels like this should happen for sure"

// calling signs as did for audience, under the name label.
func calling(t *testing.T, key ed25519.PrivateKey, did, label, audience string) string {
	t.Helper()
	jti := make([]byte, assertionIDBytes)
	_, err := rand.Read(jti)
	require.NoError(t, err)
	signed, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, carriedClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: did, Subject: did, Audience: jwt.ClaimStrings{audience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)), ID: hex.EncodeToString(jti),
		},
		Label: label,
	}).SignedString(key)
	require.NoError(t, err)
	return signed
}

func TestAKeyNobodyHoldsIsToldOnceAsAKeyToAdmit(t *testing.T) {
	h, _, _, _ := saidYesTo(t)
	var told []string
	h.SetUnknownKey(func(did, label string) { told = append(told, did, label) })
	ramen, ramenDID := aSessionsKey(t)

	_, ok := presentedSigned(h, calling(t, ramen, ramenDID, "ramen", signedNodeOrigin))
	assert.False(t, ok, "a key the node holds nothing for was admitted")
	_, ok = presentedSigned(h, calling(t, ramen, ramenDID, "ramen", signedNodeOrigin))
	assert.False(t, ok)

	require.Equal(t, []string{ramenDID, "ramen"}, told, "the same key was told twice, or not at all")
}

func TestASignatureThatIsNotForThisNodeIsNotTold(t *testing.T) {
	h, _, _, _ := saidYesTo(t)
	var told []string
	h.SetUnknownKey(func(did, label string) { told = append(told, did) })
	ramen, ramenDID := aSessionsKey(t)

	_, ok := presentedSigned(h, calling(t, ramen, ramenDID, "ramen", "https://elsewhere.example"))
	assert.False(t, ok)
	assert.Empty(t, told, "a signature for another node put a key before the person")
}

// admitAs asks node's /auth/admit for did under label, with authorization as it is.
func admitAs(node *Handler, authorization, did, label string) *httptest.ResponseRecorder {
	form := url.Values{"did": {did}, "label": {label}}
	req := httptest.NewRequest(http.MethodPost, admitPath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", authorization)
	w := httptest.NewRecorder()
	node.handleAdmit(w, req)
	return w
}

// The person approved the key at laye: the primary carries them to the node,
// 「[Opus 5.5] and the node holds the key as theirs, at their level.」
//
// "which should be obvious considereing it uses my finger and considering i am ROOT for real"
func TestThePersonThePrimaryCarriesAdmitsAKey(t *testing.T) {
	nodeKey, nodeDID := otherNode(t)
	primary, served, _ := primaryAndPeer(t, nodeDID)
	peer, known := primary.peerNamed(peerName)
	require.True(t, known)
	carried, err := primary.carriedSignature(mastodonAccount, "", LevelSuper, peer, peer.Namespaces, time.Now())
	require.NoError(t, err)

	node := handlerWithDoors(t)
	node.tokens = newMemTokenStore()
	node.nodeKey = nodeKey
	require.NoError(t, node.SetPrimary(served))
	ramen, ramenDID := aSessionsKey(t)

	w := admitAs(node, carried, ramenDID, "ramen")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	admitted, ok := presentedSigned(node, calling(t, ramen, ramenDID, "ramen", node.publicOrigin()))
	require.True(t, ok, "the key the person admitted is not admitted")
	assert.Equal(t, mastodonAccount, admitted.Identity)
	assert.Equal(t, string(LevelRoot), admitted.LevelName())
	// 「[Opus 5.5] It stands by its own name, at the level of the person who approved it.」
	require.NotNil(t, admitted.Grant)
	assert.Equal(t, "ramen", admitted.Grant.Label)
	assert.Equal(t, ramenDID, admitted.Grant.DID)
}

// A SUPER token ROOT minted is not the person: it admits no key.
func TestATokenAdmitsNoKey(t *testing.T) {
	node := handlerWithDoors(t)
	store := newMemTokenStore()
	node.tokens = store
	_, err := store.Issue(IssuedToken{Hash: sha256Hex("SUPER14SEPT-raw"), Label: "SUPER14SEPT", MintedBy: mastodonAccount,
		Level: LevelSuper, Namespaces: []string{"ground"}, ExpiresAt: NeverEnds()})
	require.NoError(t, err)
	_, ramenDID := aSessionsKey(t)

	w := admitAs(node, "Bearer SUPER14SEPT-raw", ramenDID, "ramen")
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
}
