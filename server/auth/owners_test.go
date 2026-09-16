package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A passkey stands on several devices. Apple copies one from a phone to a
// laptop, and each device derives its own key from it. The key of the device
// that enrolled it is the first owner; every device it stands on after is one
// more.
//
// "Clearly I want the honest model."
func TestAPasskeyStandsOnTheDeviceThatEnrolledIt(t *testing.T) {
	store := credentialStoreForTest(t)
	require.NoError(t, store.save(credential("phone"), "did:key:zphone", mastodonAccount))

	owners, err := store.ownersOf([]byte("phone"))
	require.NoError(t, err)
	assert.Equal(t, []string{"did:key:zphone"}, owners)
}

func TestAPasskeyStandsOnEveryDeviceItAnsweredFrom(t *testing.T) {
	store := credentialStoreForTest(t)
	require.NoError(t, store.save(credential("phone"), "did:key:zphone", mastodonAccount))

	require.NoError(t, store.addOwner([]byte("phone"), "did:key:zlaptop", mastodonAccount))
	// The same device answering again is the same device.
	require.NoError(t, store.addOwner([]byte("phone"), "did:key:zlaptop", mastodonAccount))

	owners, err := store.ownersOf([]byte("phone"))
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"did:key:zphone", "did:key:zlaptop"}, owners)
}

// Forgetting the passkey forgets every device it stood on.
func TestForgettingAPasskeyForgetsEveryDeviceItStoodOn(t *testing.T) {
	store := credentialStoreForTest(t)
	require.NoError(t, store.save(credential("phone"), "did:key:zphone", mastodonAccount))
	require.NoError(t, store.addOwner([]byte("phone"), "did:key:zlaptop", mastodonAccount))

	require.NoError(t, store.forget([]byte("phone")))

	owners, err := store.ownersOf([]byte("phone"))
	require.NoError(t, err)
	assert.Empty(t, owners)
}

// A key nobody enrolled has no owners, which is an answer and not a failure.
func TestAnUnknownPasskeyStandsNowhere(t *testing.T) {
	store := credentialStoreForTest(t)

	owners, err := store.ownersOf([]byte("never-enrolled"))
	require.NoError(t, err)
	assert.Empty(t, owners)
}
