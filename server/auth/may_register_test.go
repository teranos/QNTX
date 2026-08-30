package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func registerRequest(t *testing.T, session string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/auth/register/begin", nil)
	if session != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	}
	return req
}

// A node listing nobody has admitted nobody, so there is no account a device
// could speak for. Enrolment is refused rather than claimed by whoever asks.
func TestAnEmptyListEnrolsNobody(t *testing.T) {
	h := handlerWithCreds(t)

	assert.Error(t, h.mayRegister(h.presented(registerRequest(t, ""))))
}

// Empty is not the same as open. A deployment that names who may log in has
// somebody to ask, so the first enrolment is asked too.
func TestAFreshGovernedDeploymentIsNotOpen(t *testing.T) {
	h := handlerWithCreds(t)
	h.SetIdentities([]string{mastodonAccount}, nil)

	assert.Error(t, h.mayRegister(h.presented(registerRequest(t, ""))))
}

// The half-admission laye leaves behind is what a first enrolment stands on,
// since there is no session yet for the account being created.
func TestAGovernedFirstEnrolmentStandsOnAPending(t *testing.T) {
	h := handlerWithCreds(t)
	h.SetIdentities([]string{mastodonAccount}, nil)

	pending, err := h.pendingLogins.open(mastodonAccount)
	require.NoError(t, err)

	req := registerRequest(t, "")
	req.AddCookie(&http.Cookie{Name: pendingCookieName, Value: pending})
	assert.NoError(t, h.mayRegister(h.presented(req)))
}

// A second device is added by the person who already holds one, so proving a
// session is what separates a phone of theirs from a stranger at the endpoint.
func TestASecondPasskeyEnrolsWithASession(t *testing.T) {
	h := handlerWithCreds(t)
	h.SetIdentities([]string{mastodonAccount}, nil)
	require.NoError(t, h.creds.save(credential("laptop"), "did:key:zowner", mastodonAccount))

	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)

	assert.NoError(t, h.mayRegister(h.presented(registerRequest(t, session))))
}

// Without a session an already-owned deployment must refuse, or anyone
// reaching the endpoint could enrol their own passkey and become an owner.
func TestASecondPasskeyIsRefusedWithoutASession(t *testing.T) {
	h := handlerWithCreds(t)
	h.SetIdentities([]string{mastodonAccount}, nil)
	require.NoError(t, h.creds.save(credential("laptop"), "did:key:zowner", mastodonAccount))

	require.Error(t, h.mayRegister(h.presented(registerRequest(t, ""))))
}

// A cookie that is not a live session is the same as no session.
func TestAnInvalidSessionDoesNotEnrol(t *testing.T) {
	h := handlerWithCreds(t)
	h.SetIdentities([]string{mastodonAccount}, nil)
	require.NoError(t, h.creds.save(credential("laptop"), "did:key:zowner", mastodonAccount))

	require.Error(t, h.mayRegister(h.presented(registerRequest(t, "not-a-real-session"))))
}

// Striking an account out revokes what it may still do, not only what it may
// do next. A live session for it stops being able to add a device.
func TestAStruckOutSessionEnrolsNothing(t *testing.T) {
	h := handlerWithCreds(t)
	h.SetIdentities([]string{mastodonAccount}, nil)
	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)
	require.NoError(t, h.mayRegister(h.presented(registerRequest(t, session))))

	// No restart between these two lines: this is the whole property.
	h.SetIdentities([]string{atprotoAccount}, nil)

	err = h.mayRegister(h.presented(registerRequest(t, session)))
	require.Error(t, err)
	// The outcome, and nothing that helps whoever was refused get in next time.
	// Who was refused and why is written down as an attestation (ADR-030).
	assert.Equal(t, "refused", err.Error())
}
