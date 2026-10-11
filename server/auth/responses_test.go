package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// created is a registration response as the library parsed it.
func created(crossOrigin bool) *protocol.ParsedCredentialCreationData {
	var parsed protocol.ParsedCredentialCreationData
	parsed.Response.CollectedClientData.CrossOrigin = crossOrigin
	return &parsed
}

// §7.1 step 10: a passkey made inside a frame of another origin is not one the
// node expects, because the node never frames a ceremony.
func TestAPasskeyMadeInAFrameOfAnotherOriginIsRefused(t *testing.T) {
	assert.Error(t, createdAsExpected(created(true), &webauthn.Credential{}))
	assert.NoError(t, createdAsExpected(created(false), &webauthn.Credential{}))
}

// §7.2 step 13: a sign-in run inside a frame of another origin is refused.
func TestASignInInAFrameOfAnotherOriginIsRefused(t *testing.T) {
	h := handlerWithDoors(t, garden())
	h.users = &memUsers{held: []User{{ID: "US-ONE", Level: LevelRoot, Accounts: []UserAccount{{Provider: "mastodon", CanonicalID: mastodonAccount}}}}}
	laptop := newAuthenticator(t, "laptop")
	require.NoError(t, h.creds.saveAt(laptop.credential(t), "did:key:zlaptop", mastodonAccount, "garden", "US-ONE"))

	browser, challenge := beginSignIn(t, h)
	session, ok := h.ceremonies.finish(signingIn(Presented{pendingToken: browser.Value}))
	require.True(t, ok)
	r := httptest.NewRequest(http.MethodPost, "/auth/login/finish", nil)
	r.Header.Set("Origin", gardenOrigin)
	arrived, ok := h.doorFor(r)
	require.True(t, ok)
	creds, err := h.devicesOf(arrived.namespace, mastodonAccount)
	require.NoError(t, err)

	framed := laptop.answer(t, "garden.test", challenge, "US-ONE")
	framed.Response.CollectedClientData.CrossOrigin = true
	_, err = h.validateAssertion(arrived.rp, *session, framed, creds)
	assert.Error(t, err)
}

// §7.1 step 17: an authenticator saying a passkey is backed up and cannot be
// is refused at enrolment, not at every sign-in after it.
func TestAPasskeyBackedUpThatCannotBeIsRefused(t *testing.T) {
	contradicts := &webauthn.Credential{Flags: webauthn.CredentialFlags{BackupEligible: false, BackupState: true}}
	assert.Error(t, createdAsExpected(created(false), contradicts))

	synced := &webauthn.Credential{Flags: webauthn.CredentialFlags{BackupEligible: true, BackupState: true}}
	assert.NoError(t, createdAsExpected(created(false), synced))
}
