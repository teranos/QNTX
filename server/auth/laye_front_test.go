package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "AND LAYE OWNS IDENTITY"
// "BUT LAYE DOES NOT REALLY OWN USER, THE QNTX WAY"

// behindLaye is a signing node with laye in front of it: the node's own key,
// and laye's.
func behindLaye(t *testing.T) (h *Handler, sessionDID string, laye ed25519.PrivateKey, layeDID string, nodeDID string) {
	t.Helper()
	h, _, _, sessionDID, _ = signingNode(t)
	_, nodeKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	h.SetNodeKey(nodeKey)
	nodeDID, err = h.ownDID()
	require.NoError(t, err)
	_, laye, err = ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	layeDID = EncodeDIDKey(laye.Public().(ed25519.PublicKey))
	require.NoError(t, h.SetLaye(layeDID))
	return h, sessionDID, laye, layeDID, nodeDID
}

// carriedBy is what laye hands the node for a session it checked: laye's own
// signature, naming the session, for this node alone.
func carriedBy(t *testing.T, key ed25519.PrivateKey, issuer, subject, audience string) string {
	t.Helper()
	jti := make([]byte, assertionIDBytes)
	_, err := rand.Read(jti)
	require.NoError(t, err)
	signed, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, carriedClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: issuer, Subject: subject, Audience: jwt.ClaimStrings{audience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)), ID: hex.EncodeToString(jti),
		},
		// What laye might say of the session is not what the node goes by.
		Level:      string(LevelRoot),
		Namespaces: []string{"everything"},
		Label:      "laye",
	}).SignedString(key)
	require.NoError(t, err)
	return signed
}

func TestLayeCarriesASessionOnTheGrantTheNodeHoldsForIt(t *testing.T) {
	h, session, laye, layeDID, nodeDID := behindLaye(t)
	admitted, ok := presentedSigned(h, carriedBy(t, laye, layeDID, session, nodeDID))
	require.True(t, ok, "a session laye carried was not admitted")
	assert.Equal(t, session, admitted.ActsAs())
	assert.Equal(t, mastodonAccount, admitted.Identity)
	assert.Equal(t, string(LevelAttestor), admitted.LevelName(), "the level is the node's grant's, not laye's word")
	assert.Equal(t, []string{"ground"}, admitted.Namespaces, "the namespaces are the node's grant's, not laye's word")
}

func TestLayeCarryingASessionTheNodeHoldsNoGrantForIsRefused(t *testing.T) {
	h, _, laye, layeDID, nodeDID := behindLaye(t)
	_, stranger, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	strangerDID := EncodeDIDKey(stranger.Public().(ed25519.PublicKey))
	_, ok := presentedSigned(h, carriedBy(t, laye, layeDID, strangerDID, nodeDID))
	assert.False(t, ok, "laye carried a key the node grants nothing, and it was admitted")
}

func TestAKeyThatIsNotLayeCarriesNobody(t *testing.T) {
	h, session, _, _, nodeDID := behindLaye(t)
	_, other, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	otherDID := EncodeDIDKey(other.Public().(ed25519.PublicKey))
	_, ok := presentedSigned(h, carriedBy(t, other, otherDID, session, nodeDID))
	assert.False(t, ok, "a key that is not laye carried a session in")
}

func TestLayeCarriesOnlyToThisNode(t *testing.T) {
	h, session, laye, layeDID, _ := behindLaye(t)
	_, ok := presentedSigned(h, carriedBy(t, laye, layeDID, session, signedNodeOrigin))
	assert.False(t, ok, "laye's signature for another audience was admitted")
}

func TestLayeCarriesEachRequestOnce(t *testing.T) {
	h, session, laye, layeDID, nodeDID := behindLaye(t)
	signed := carriedBy(t, laye, layeDID, session, nodeDID)
	_, first := presentedSigned(h, signed)
	require.True(t, first)
	_, again := presentedSigned(h, signed)
	assert.False(t, again, "laye's signature for one request was admitted twice")
}

func TestLayeIsNamedByADIDKey(t *testing.T) {
	h := &Handler{}
	assert.Error(t, h.SetLaye("https://laye.example"))
	assert.NoError(t, h.SetLaye(""), "no laye in front is a node standing on its own")
}
