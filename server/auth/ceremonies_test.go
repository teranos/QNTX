package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Two people signing in at once each keep the challenge they were issued
// (§13.4.3). One browser beginning again does not unseat the other's.
func TestTwoBrowsersSigningInKeepTheirOwnCeremonies(t *testing.T) {
	h := handlerWithDoors(t, garden())
	h.users = &memUsers{held: []User{{ID: "US-ONE", Level: LevelRoot, Accounts: []UserAccount{{Provider: "mastodon", CanonicalID: mastodonAccount}}}}}
	require.NoError(t, h.creds.saveAt(credential("laptop"), "did:key:zlaptop", mastodonAccount, "garden", "US-ONE"))

	first, firstChallenge := beginSignIn(t, h)
	second, secondChallenge := beginSignIn(t, h)
	require.NotEqual(t, firstChallenge, secondChallenge)

	held, ok := h.ceremonies.finish(signingIn(Presented{pendingToken: first.Value}))
	require.True(t, ok, "the first browser's ceremony was unseated by the second")
	assert.Equal(t, firstChallenge, held.Challenge)

	held, ok = h.ceremonies.finish(signingIn(Presented{pendingToken: second.Value}))
	require.True(t, ok)
	assert.Equal(t, secondChallenge, held.Challenge)

	_, again := h.ceremonies.finish(signingIn(Presented{pendingToken: first.Value}))
	assert.False(t, again, "a challenge answers one response")
}

// A ceremony is valid for the timeout WebAuthn recommends (§13.4.3, §15.1),
// and one nobody finished is gone after it.
func TestACeremonyEndsWithItsTimeout(t *testing.T) {
	h := handlerWithDoors(t, garden())
	h.users = &memUsers{held: []User{{ID: "US-ONE", Level: LevelRoot, Accounts: []UserAccount{{Provider: "mastodon", CanonicalID: mastodonAccount}}}}}
	require.NoError(t, h.creds.saveAt(credential("laptop"), "did:key:zlaptop", mastodonAccount, "garden", "US-ONE"))

	browser, _ := beginSignIn(t, h)
	key := signingIn(Presented{pendingToken: browser.Value})

	h.ceremonies.sweep(time.Now())
	held, ok := h.ceremonies.finish(key)
	require.True(t, ok, "a ceremony inside its timeout was swept")
	assert.WithinDuration(t, time.Now().Add(ceremonyTimeout), held.Expires, time.Minute)

	h.ceremonies.begin(key, held)
	h.ceremonies.sweep(time.Now().Add(ceremonyTimeout + time.Second))
	_, ok = h.ceremonies.finish(key)
	assert.False(t, ok, "an abandoned ceremony outlived its timeout")
}
