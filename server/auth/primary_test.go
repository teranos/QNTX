package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "goes to their primary email address if there are multiple."
//
// Primary is the first address a User supplied. Nothing reorders the list, so
// an address arriving later never takes its place.
func TestThePrimaryEmailIsTheFirstSupplied(t *testing.T) {
	tim := User{EmailAddresses: []string{"tim@defacile.nl", "tim@werk.nl"}}
	assert.Equal(t, "tim@defacile.nl", tim.PrimaryEmail())

	assert.Empty(t, User{}.PrimaryEmail(), "a User who gave no address has no primary")
}

// The mail service names a User by id, the way an admission does.
func TestUserByIDIsTheRecordAnIDNames(t *testing.T) {
	h, store, _ := switchingHandler(t)
	tim := store.held[0]

	found, ok, err := h.UserByID(tim.ID)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, tim.ID, found.ID)

	_, ok, err = h.UserByID("USnobody")
	require.NoError(t, err)
	assert.False(t, ok, "an id no User holds is nobody, and not an error")
}

// A node without login keeps no Users, and asking it for one says so rather
// than answering nobody.
func TestANodeKeepingNoUsersSaysSo(t *testing.T) {
	var h *Handler
	_, _, err := h.UserByID("UStim")
	assert.Error(t, err)
	_, err = h.Users()
	assert.Error(t, err)
	_, _, err = h.RootUser()
	assert.Error(t, err)
}

// "for a user that is one of the root identities, i want to receive a weekly report."
func TestTheRootUserIsTheOneTheRootIdentitiesReach(t *testing.T) {
	h := &Handler{users: &memUsers{held: []User{
		{ID: "USspike", Level: LevelPublicRegistration, Namespace: "garden"},
		{ID: "USroot", Level: LevelRoot, EmailAddresses: []string{"root@garden.test"}},
	}}}

	root, found, err := h.RootUser()
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "USroot", root.ID)

	everyone, err := h.Users()
	require.NoError(t, err)
	assert.Len(t, everyone, 2)
}

// An unclaimed node has no ROOT User yet, and that is not an error.
func TestAnUnclaimedNodeHasNoRootUser(t *testing.T) {
	h := &Handler{users: &memUsers{}}
	_, found, err := h.RootUser()
	require.NoError(t, err)
	assert.False(t, found)
}
