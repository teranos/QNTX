package auth

import (
	"crypto/ed25519"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The browser derives the user DID from the PRF output and never sends the
// seed, so a bare DID would be a claim. It signs the ceremony challenge with
// the derived key and the server checks it holds the private half.
func TestAProvenUserDIDIsAccepted(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	did := EncodeDIDKey(pub)
	challenge := []byte("ceremony-challenge")

	err = VerifyUserDID(did, challenge, ed25519.Sign(priv, challenge))
	assert.NoError(t, err)
}

// Claiming a DID you cannot sign for is how one person registers another
// person's identity against their own passkey.
func TestADIDWithoutTheKeyIsRefused(t *testing.T) {
	claimed, _, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	_, otherPriv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	challenge := []byte("ceremony-challenge")

	err = VerifyUserDID(EncodeDIDKey(claimed), challenge, ed25519.Sign(otherPriv, challenge))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "signature")
}

// A signature over a different challenge is a replay from another ceremony.
func TestASignatureOverAnotherChallengeIsRefused(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	err = VerifyUserDID(EncodeDIDKey(pub), []byte("this-ceremony"), ed25519.Sign(priv, []byte("another-ceremony")))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "signature")
}

// What the browser encodes is what the server decodes back to a verifying key.
func TestDIDKeyRoundTrips(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	decoded, err := DecodeUserDID(EncodeDIDKey(pub))
	require.NoError(t, err)
	assert.Equal(t, ed25519.PublicKey(pub), decoded)
}

// Anything that is not a did:key is refused before it reaches a keypair.
func TestANonDIDKeyIsRefused(t *testing.T) {
	_, err := DecodeUserDID("did:web:example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did:key")
}
