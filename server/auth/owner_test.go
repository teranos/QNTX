package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/go-webauthn/webauthn/webauthn"
	qntxtest "github.com/teranos/QNTX/internal/testing"
)

func credentialStoreForTest(t *testing.T) *credentialStore {
	t.Helper()
	return newCredentialStore(qntxtest.CreateTestDB(t), zap.NewNop().Sugar())
}

func credential(id string) webauthn.Credential {
	return webauthn.Credential{
		ID:              []byte(id),
		PublicKey:       []byte("pubkey-" + id),
		AttestationType: "none",
	}
}

// A passkey belongs to someone. Without an owner every credential is
// interchangeable and a session can only say "somebody authenticated".
func TestACredentialRemembersItsOwner(t *testing.T) {
	store := credentialStoreForTest(t)

	require.NoError(t, store.save(credential("laptop"), "did:key:zowner", mastodonAccount))

	owner, err := store.ownerOf([]byte("laptop"))
	require.NoError(t, err)
	assert.Equal(t, "did:key:zowner", owner)
}

// Several devices, one person — which is what makes the account the identity
// and each key a device credential under it.
func TestOneOwnerCanHoldSeveralCredentials(t *testing.T) {
	store := credentialStoreForTest(t)

	require.NoError(t, store.save(credential("laptop"), "did:key:zowner", mastodonAccount))
	require.NoError(t, store.save(credential("phone"), "did:key:zowner", mastodonAccount))

	laptop, err := store.ownerOf([]byte("laptop"))
	require.NoError(t, err)
	phone, err := store.ownerOf([]byte("phone"))
	require.NoError(t, err)
	assert.Equal(t, laptop, phone)
}

// An unknown credential has no owner, and that is not a failure to read.
func TestAnUnknownCredentialHasNoOwner(t *testing.T) {
	store := credentialStoreForTest(t)

	owner, err := store.ownerOf([]byte("never-registered"))
	require.NoError(t, err)
	assert.Empty(t, owner)
}
