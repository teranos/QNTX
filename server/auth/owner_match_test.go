package auth

import (
	"crypto/ed25519"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	qntxtest "github.com/teranos/QNTX/internal/testing"
)

func handlerWithCreds(t *testing.T) *Handler {
	t.Helper()
	return &Handler{
		creds:    newCredentialStore(qntxtest.CreateTestDB(t), zap.NewNop().Sugar()),
		sessions: newSessionStore(24),
		logger:   zap.NewNop().Sugar(),
	}
}

// The same biometric derives the same key, so a login that proves a different
// DID than the credential was registered with is a different person or a
// different authenticator.
func TestLoginRefusesADifferentOwner(t *testing.T) {
	h := handlerWithCreds(t)

	registered, _, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	require.NoError(t, h.creds.save(credential("laptop"), EncodeDIDKey(registered), ""))

	imposterPub, imposterPriv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	body := proofBody(t, EncodeDIDKey(imposterPub), ed25519.Sign(imposterPriv, []byte(testChallenge)))

	err = h.checkOwnerMatches([]byte("laptop"), body, testChallenge)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "owned by")
}

// Proving the DID the credential was registered with is the whole point.
func TestLoginAcceptsTheRegisteredOwner(t *testing.T) {
	h := handlerWithCreds(t)

	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	did := EncodeDIDKey(pub)
	require.NoError(t, h.creds.save(credential("laptop"), did, ""))

	body := proofBody(t, did, ed25519.Sign(priv, []byte(testChallenge)))

	assert.NoError(t, h.checkOwnerMatches([]byte("laptop"), body, testChallenge))
}

// Credentials registered before PRF existed have no owner. They must keep
// working, or shipping this would lock out every passkey already enrolled.
func TestLoginAllowsAnOwnerlessCredential(t *testing.T) {
	h := handlerWithCreds(t)
	require.NoError(t, h.creds.save(credential("legacy"), "", ""))

	assert.NoError(t, h.checkOwnerMatches([]byte("legacy"), []byte(`{"id":"legacy"}`), testChallenge))
}

// An owned credential presented without a proof is a downgrade attempt: drop
// the DID and inherit the session that DID was meant to gate.
func TestLoginRefusesAnOwnedCredentialWithNoProof(t *testing.T) {
	h := handlerWithCreds(t)

	pub, _, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	require.NoError(t, h.creds.save(credential("laptop"), EncodeDIDKey(pub), ""))

	err = h.checkOwnerMatches([]byte("laptop"), []byte(`{"id":"laptop"}`), testChallenge)
	require.Error(t, err)
}
