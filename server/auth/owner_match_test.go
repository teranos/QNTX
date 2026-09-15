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

// A passkey stands on several devices, and each derives its own key. A login
// proving a key the passkey has not stood on before is not another person; it
// is the same passkey on one more device, and it says so rather than refusing.
//
// "Clearly I want the honest model."
func TestAKeyThePasskeyHasNotStoodOnDoesNotStandYet(t *testing.T) {
	h := handlerWithCreds(t)

	enrolled, _, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	require.NoError(t, h.creds.save(credential("phone"), EncodeDIDKey(enrolled), mastodonAccount))

	laptopPub, laptopPriv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	body := proofBody(t, EncodeDIDKey(laptopPub), ed25519.Sign(laptopPriv, []byte(testChallenge)))

	proven, stands, err := h.ownerStands([]byte("phone"), body, testChallenge)
	require.NoError(t, err)
	assert.False(t, stands)
	assert.Equal(t, EncodeDIDKey(laptopPub), proven)
}

// Proving a key the passkey already stands on is the ordinary login.
func TestAKeyThePasskeyStandsOnStands(t *testing.T) {
	h := handlerWithCreds(t)

	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	did := EncodeDIDKey(pub)
	require.NoError(t, h.creds.save(credential("phone"), did, mastodonAccount))

	body := proofBody(t, did, ed25519.Sign(priv, []byte(testChallenge)))

	proven, stands, err := h.ownerStands([]byte("phone"), body, testChallenge)
	require.NoError(t, err)
	assert.True(t, stands)
	assert.Equal(t, did, proven)
}

// Once a device has answered, the passkey stands on it and the key is one the
// User holds, recorded as a device (ADR-031).
func TestAnsweringFromANewDeviceRecordsIt(t *testing.T) {
	h := handlerWithCreds(t)
	users := &memUsers{held: []User{{ID: "US-TIM", Level: LevelRoot,
		Accounts: []UserAccount{{Provider: "mastodon", CanonicalID: mastodonAccount}}}}}
	h.users = users
	require.NoError(t, h.creds.save(credential("phone"), "did:key:zphone", mastodonAccount))

	require.NoError(t, h.standOn([]byte("phone"), "did:key:zlaptop", mastodonAccount))

	owners, err := h.creds.ownersOf([]byte("phone"))
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"did:key:zphone", "did:key:zlaptop"}, owners)
	require.Len(t, users.held[0].Keys, 1)
	assert.Equal(t, UserKey{DID: "did:key:zlaptop", Origin: OriginDevice}, users.held[0].Keys[0])
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

// Belt to the migration's braces: a key nobody enrolled stands nowhere, and
// a login on it is refused rather than read as "stands on this device now".
func TestLoginRefusesAnUnknownCredential(t *testing.T) {
	h := handlerWithCreds(t)

	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	body := proofBody(t, EncodeDIDKey(pub), ed25519.Sign(priv, []byte(testChallenge)))

	_, _, err = h.ownerStands([]byte("never-enrolled"), body, testChallenge)
	assert.Error(t, err)
}

// An owned credential presented without a proof is a downgrade attempt: drop
// the DID and inherit the session that DID was meant to gate.
func TestLoginRefusesAnOwnedCredentialWithNoProof(t *testing.T) {
	h := handlerWithCreds(t)

	pub, _, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	require.NoError(t, h.creds.save(credential("phone"), EncodeDIDKey(pub), mastodonAccount))

	_, _, err = h.ownerStands([]byte("phone"), []byte(`{"id":"phone"}`), testChallenge)
	require.Error(t, err)
}
