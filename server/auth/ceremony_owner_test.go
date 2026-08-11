package auth

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testChallenge = "Y2hhbGxlbmdlLWZvci10aGlzLWNlcmVtb255"

func proofBody(t *testing.T, did string, sig []byte) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]string{
		"id":                 "credential-id",
		"user_did":           did,
		"user_did_signature": base64.RawURLEncoding.EncodeToString(sig),
	})
	require.NoError(t, err)
	return body
}

// The browser proves it holds the key its DID names, so the server records an
// owner it has verified rather than one it was told.
func TestOwnerIsTakenFromAProvenDID(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	did := EncodeDIDKey(pub)

	owner, err := verifiedOwnerDID(proofBody(t, did, ed25519.Sign(priv, []byte(testChallenge))), testChallenge)
	require.NoError(t, err)
	assert.Equal(t, did, owner)
}

// PRF is not universally supported. A browser without it sends no DID and
// registers ownerless — degraded, not refused.
func TestNoDIDMeansNoOwnerAndNoError(t *testing.T) {
	owner, err := verifiedOwnerDID([]byte(`{"id":"credential-id"}`), testChallenge)
	require.NoError(t, err)
	assert.Empty(t, owner)
}

// A DID the client cannot sign for must fail the ceremony. Falling back to
// ownerless here would let anyone claim any identity and still get a session.
func TestAnUnprovenDIDFailsTheCeremony(t *testing.T) {
	claimed, _, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	_, otherPriv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	_, err = verifiedOwnerDID(proofBody(t, EncodeDIDKey(claimed), ed25519.Sign(otherPriv, []byte(testChallenge))), testChallenge)
	require.Error(t, err)
}

// A DID with no signature at all is the same claim, made more cheaply.
func TestADIDWithoutASignatureFailsTheCeremony(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	body, err := json.Marshal(map[string]string{"user_did": EncodeDIDKey(pub)})
	require.NoError(t, err)

	_, err = verifiedOwnerDID(body, testChallenge)
	require.Error(t, err)
}

// A proof from an earlier ceremony must not carry into this one.
func TestAProofFromAnotherCeremonyIsRefused(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	body := proofBody(t, EncodeDIDKey(pub), ed25519.Sign(priv, []byte("an-earlier-challenge")))

	_, err = verifiedOwnerDID(body, testChallenge)
	require.Error(t, err)
}
