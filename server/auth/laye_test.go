package auth

import (
	"crypto/ed25519"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAChallengeIsGoodOnce(t *testing.T) {
	var c layeChallenges
	challenge, err := c.issue()
	require.NoError(t, err)

	assert.True(t, c.redeem(challenge))
	assert.False(t, c.redeem(challenge), "a spent challenge lets a captured signature replay")
}

func TestAChallengeNobodyIssuedIsRefused(t *testing.T) {
	var c layeChallenges
	assert.False(t, c.redeem("challenge-from-somewhere-else"))
}

func TestTwoChallengesDiffer(t *testing.T) {
	var c layeChallenges
	first, err := c.issue()
	require.NoError(t, err)
	second, err := c.issue()
	require.NoError(t, err)
	assert.NotEqual(t, first, second)
}

// laye signs with the key its did:key names, which is the whole of what the
// server can check about a key it never holds.
func TestALayeSignatureVerifiesAgainstItsDID(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	var c layeChallenges
	challenge, err := c.issue()
	require.NoError(t, err)

	signature := ed25519.Sign(priv, []byte(challenge))
	require.NoError(t, VerifyUserDID(EncodeDIDKey(pub), []byte(challenge), signature))
}

func TestASignatureFromAnotherKeyIsRefused(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	_, otherPriv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	var c layeChallenges
	challenge, err := c.issue()
	require.NoError(t, err)

	signature := ed25519.Sign(otherPriv, []byte(challenge))
	assert.Error(t, VerifyUserDID(EncodeDIDKey(pub), []byte(challenge), signature))
}

func TestASignatureOverADifferentChallengeIsRefused(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	var c layeChallenges
	mine, err := c.issue()
	require.NoError(t, err)
	theirs, err := c.issue()
	require.NoError(t, err)

	signature := ed25519.Sign(priv, []byte(theirs))
	assert.Error(t, VerifyUserDID(EncodeDIDKey(pub), []byte(mine), signature))
}
