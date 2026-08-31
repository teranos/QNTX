package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func publicHandler(t *testing.T) (*Handler, *memUsers) {
	t.Helper()
	kept := &memUsers{}
	return &Handler{logger: testLogger(), users: kept}, kept
}

// "WHY CANT WE HAVE A PUBLIC USER THAT SIMPLY CANT DO ANYTHING BUT LOGIN"
func TestRegisteringAtADoorMintsAPublicUser(t *testing.T) {
	h, kept := publicHandler(t)

	u, err := h.joinPublic(account{CanonicalID: "google:110", Handle: "a@b.c"}, "garden")
	require.NoError(t, err)

	assert.Equal(t, LevelPublicRegistration, u.Level)
	require.Len(t, kept.held, 1)
	assert.Equal(t, u.ID, kept.held[0].ID)
}

// "no they dont" — the same Google account at two doors is two registrations.
func TestTheSameAccountAtTwoDoorsIsTwoRegistrations(t *testing.T) {
	h, kept := publicHandler(t)

	first, err := h.joinPublic(account{CanonicalID: "google:110"}, "garden")
	require.NoError(t, err)
	second, err := h.joinPublic(account{CanonicalID: "google:110"}, "pond")
	require.NoError(t, err)

	assert.NotEqual(t, first.ID, second.ID, "one registration served two doors")
	assert.Len(t, kept.held, 2)
}

// A public registration belongs to the namespace it arrived in.
func TestAPublicUserCarriesTheDoorItArrivedAt(t *testing.T) {
	h, _ := publicHandler(t)

	u, err := h.joinPublic(account{CanonicalID: "google:110"}, "garden")
	require.NoError(t, err)

	assert.Equal(t, "garden", u.Namespace)
}

// Arriving twice at the same door is the same registration.
func TestArrivingTwiceAtOneDoorIsOneRegistration(t *testing.T) {
	h, kept := publicHandler(t)

	first, err := h.joinPublic(account{CanonicalID: "google:110"}, "garden")
	require.NoError(t, err)
	again, err := h.joinPublic(account{CanonicalID: "google:110"}, "garden")
	require.NoError(t, err)

	assert.Equal(t, first.ID, again.ID)
	assert.Len(t, kept.held, 1)
}

// "when a user registers, we attest it, and in it, an email address may be."
func TestAnAddressTheProviderGaveIsHeld(t *testing.T) {
	h, _ := publicHandler(t)

	u, err := h.joinPublic(account{CanonicalID: "google:110", Handle: "a@b.c"}, "garden")
	require.NoError(t, err)

	assert.Equal(t, []string{"a@b.c"}, u.EmailAddresses)
}
