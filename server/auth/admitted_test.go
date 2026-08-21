package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	mastodonAccount = "https://mastodon.example/@tim"
	atprotoAccount  = "did:plc:examplexamplexamplexampl"
)

func handlerAdmitting(t *testing.T, identities ...string) *Handler {
	t.Helper()
	h := handlerWithCreds(t)
	h.SetIdentities(identities, nil)
	return h
}

// A passkey is a fast way back in as an account, so which account it speaks
// for has to survive the enrolment. Without this it proves only that some
// authenticator was present.
func TestPasskeyRemembersWhoEnrolledIt(t *testing.T) {
	h := handlerAdmitting(t, mastodonAccount)
	cred := credential("laptop")
	require.NoError(t, h.creds.save(cred, "did:key:zdevice", mastodonAccount))

	admitted, err := h.creds.admittedAs(cred.ID)
	require.NoError(t, err)
	assert.Equal(t, mastodonAccount, admitted)
}

// am.toml is asked at login rather than trusted from enrolment, so striking an
// account out of the list is what takes its devices with it.
func TestStrikingAnAccountRevokesItsPasskey(t *testing.T) {
	h := handlerAdmitting(t, mastodonAccount)
	assert.True(t, h.stillAdmitted(mastodonAccount))

	// No restart between these two lines: this is the whole property.
	h.SetIdentities([]string{atprotoAccount}, nil)
	assert.False(t, h.stillAdmitted(mastodonAccount))
}

// A deployment that names nobody is the passkey-only world, where a credential
// answers to itself. Enforcing an identity there would lock out every install
// that never had one.
func TestIdentitiesGovernOnlyWhenListed(t *testing.T) {
	assert.False(t, handlerAdmitting(t).identitiesGovern())
	assert.True(t, handlerAdmitting(t, mastodonAccount).identitiesGovern())
}

// The session is where the enrolling identity comes from, so a passkey added
// from a laye login carries that login's account.
func TestEnrollingIdentityComesFromTheSession(t *testing.T) {
	h := handlerAdmitting(t, mastodonAccount)
	token, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/auth/register/finish", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})

	assert.Equal(t, mastodonAccount, h.enrollingIdentity(req))
}

// No cookie is no identity, which is what makes a governed deployment refuse
// the first enrolment rather than minting an ownerless credential.
func TestEnrollingIdentityIsEmptyWithoutASession(t *testing.T) {
	h := handlerAdmitting(t, mastodonAccount)
	req := httptest.NewRequest(http.MethodPost, "/auth/register/finish", nil)

	assert.Equal(t, "", h.enrollingIdentity(req))
}

// An expired session has no identity to lend, and it is not the same answer as
// a session that logged in without one.
func TestAnExpiredSessionLendsNoIdentity(t *testing.T) {
	h := handlerAdmitting(t, mastodonAccount)
	h.sessions = newSessionStore(0)
	token, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)

	identity, ok := h.sessions.identityOf(token)
	assert.False(t, ok)
	assert.Equal(t, "", identity)
}

// A live session with no identity is a valid session. Conflating the two would
// make every passkey-only deployment look expired.
func TestASessionWithoutAnIdentityIsStillValid(t *testing.T) {
	h := handlerAdmitting(t)
	token, err := h.sessions.create("", User{})
	require.NoError(t, err)

	identity, ok := h.sessions.identityOf(token)
	assert.True(t, ok)
	assert.Equal(t, "", identity)
}
