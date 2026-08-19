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
	require.NoError(t, h.creds.save(credential("laptop"), EncodeDIDKey(registered), mastodonAccount))

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
	require.NoError(t, h.creds.save(credential("laptop"), did, mastodonAccount))

	body := proofBody(t, did, ed25519.Sign(priv, []byte(testChallenge)))

	assert.NoError(t, h.checkOwnerMatches([]byte("laptop"), body, testChallenge))
}

// A credential that cannot say who enrolled it authenticates whoever holds
// the authenticator. Migration 054 makes the row unwritable rather than
// making the login check compensate for it.
func TestAnOwnerlessCredentialCannotBeStored(t *testing.T) {
	h := handlerWithCreds(t)

	assert.Error(t, h.creds.save(credential("legacy"), "", mastodonAccount))
	assert.Error(t, h.creds.save(credential("legacy"), "did:key:zdevice", ""))
	assert.Error(t, h.creds.save(credential("legacy"), "", ""))
}

// Belt to the migration's braces: if one ever reaches the table, login
// refuses it rather than reading the empty owner as "no opinion".
func TestLoginRefusesAnOwnerlessCredential(t *testing.T) {
	h := handlerWithCreds(t)

	err := h.checkOwnerMatches([]byte("legacy"), []byte(`{"id":"legacy"}`), testChallenge)
	assert.Error(t, err)
}

// An owned credential presented without a proof is a downgrade attempt: drop
// the DID and inherit the session that DID was meant to gate.
func TestLoginRefusesAnOwnedCredentialWithNoProof(t *testing.T) {
	h := handlerWithCreds(t)

	pub, _, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	require.NoError(t, h.creds.save(credential("laptop"), EncodeDIDKey(pub), mastodonAccount))

	err = h.checkOwnerMatches([]byte("laptop"), []byte(`{"id":"laptop"}`), testChallenge)
	require.Error(t, err)
}
