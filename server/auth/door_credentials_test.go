package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	qntxtest "github.com/teranos/QNTX/internal/testing"
)

// A login at one door is offered the credentials made there and no others.
// Handing a browser keys from another door offers it what it will refuse, and
// says out loud that an account exists somewhere else — which is the one thing
// a door must not disclose.
func TestCredentialsComeBackPerDoor(t *testing.T) {
	store := credentialStoreForTest(t)

	require.NoError(t, store.saveAt(credential("at-default"), "did:key:zone", mastodonAccount, NamespaceDefault))
	require.NoError(t, store.saveAt(credential("at-garden"), "did:key:ztwo", mastodonAccount, "garden"))

	atDefault, err := store.doorCredentials(NamespaceDefault)
	require.NoError(t, err)
	require.Len(t, atDefault, 1)
	assert.Equal(t, []byte("at-default"), atDefault[0].ID)

	atVak, err := store.doorCredentials("garden")
	require.NoError(t, err)
	require.Len(t, atVak, 1)
	assert.Equal(t, []byte("at-garden"), atVak[0].ID)
}

// A door nobody has registered at has no credentials, which is an answer and
// not a failure.
func TestADoorWithNoRegistrationsIsEmpty(t *testing.T) {
	store := credentialStoreForTest(t)

	require.NoError(t, store.saveAt(credential("at-default"), "did:key:zone", mastodonAccount, NamespaceDefault))

	none, err := store.doorCredentials("pond")
	require.NoError(t, err)
	assert.Empty(t, none)
}

// Every credential enrolled before there were doors was made at the node's own
// relying party, and that is the door onto default. The migration says so
// rather than leaving them at a door that names nothing.
func TestCredentialsFromBeforeDoorsSitAtDefault(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := newCredentialStore(db, testLogger())

	// save is the shape the node wrote with before a door was a thing.
	require.NoError(t, store.save(credential("enrolled-before-doors"), "did:key:zone", mastodonAccount))

	atDefault, err := store.doorCredentials(NamespaceDefault)
	require.NoError(t, err)
	require.Len(t, atDefault, 1)
	assert.Equal(t, []byte("enrolled-before-doors"), atDefault[0].ID)
}

// The door a credential was made at is readable on its own, so a ceremony can
// ask where a key belongs rather than inferring it.
func TestTheDoorACredentialWasMadeAtIsReadable(t *testing.T) {
	store := credentialStoreForTest(t)

	cred := credential("at-garden")
	require.NoError(t, store.saveAt(cred, "did:key:ztwo", mastodonAccount, "garden"))

	door, err := store.doorOf(cred.ID)
	require.NoError(t, err)
	assert.Equal(t, "garden", door)
}

// An unknown key has no door. Empty is an answer, the way ownerOf already
// treats a credential nobody holds.
func TestAnUnknownCredentialHasNoDoor(t *testing.T) {
	store := credentialStoreForTest(t)

	door, err := store.doorOf([]byte("never-enrolled"))
	require.NoError(t, err)
	assert.Equal(t, "", door)
}
