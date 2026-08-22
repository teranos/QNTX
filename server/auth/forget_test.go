package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// Forgetting a device is destructive, so what it reaches and what it leaves
// alone are both worth stating.

// The credential goes. A device nobody can assert is a row that admits nobody.
func TestForgettingDeletesTheCredential(t *testing.T) {
	store := credentialStoreForTest(t)
	require.NoError(t, store.save(credential("laptop"), "did:key:zlaptop", mastodonAccount))
	require.NoError(t, store.save(credential("phone"), "did:key:zphone", mastodonAccount))

	require.NoError(t, store.forget([]byte("laptop")))

	owner, err := store.ownerOf([]byte("laptop"))
	require.NoError(t, err)
	assert.Empty(t, owner)

	// The other device is still a way in. Forgetting is one device, not all.
	kept, err := store.ownerOf([]byte("phone"))
	require.NoError(t, err)
	assert.Equal(t, "did:key:zphone", kept)
}

// Deleting nothing is a failure, not a success. Reporting it as done would let
// a forget that reached no row read as a device that is gone.
func TestForgettingWhatIsNotThereFails(t *testing.T) {
	store := credentialStoreForTest(t)

	assert.Error(t, store.forget([]byte("never-registered")))
}

// The keys the device stood on come off the User, and nothing else does. What
// reaches a person through a provider is not a device.
func TestForgettingDropsOnlyTheDeviceKeys(t *testing.T) {
	users := &memUsers{}
	h := &Handler{users: users, logger: zap.NewNop().Sugar()}
	h.joinUser(mastodonAccount, mastodonBinding("@tim@mastodon.example"), "did:key:zBrowser")
	h.joinDeviceKey(mastodonAccount, "did:key:zDevice")

	require.Len(t, users.held, 1)
	require.Len(t, users.held[0].Keys, 2)

	h.dropKeys(mastodonAccount, "did:key:zDevice", "did:key:zBrowser")

	assert.Empty(t, users.held[0].Keys)
	assert.Len(t, users.held[0].Accounts, 1)
}

// A key this User never held is not an error and not a write. Forgetting a
// device twice reaches the second one having nothing to do.
func TestDroppingAKeyNobodyHoldsChangesNothing(t *testing.T) {
	users := &memUsers{}
	h := &Handler{users: users, logger: zap.NewNop().Sugar()}
	h.joinUser(mastodonAccount, mastodonBinding("@tim@mastodon.example"), "did:key:zBrowser")

	h.dropKeys(mastodonAccount, "did:key:zSomeoneElse")

	require.Len(t, users.held, 1)
	assert.Len(t, users.held[0].Keys, 1)
}
