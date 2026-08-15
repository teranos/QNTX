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

// First enrolment on a fresh deployment has nobody to ask for permission.
func TestFirstPasskeyEnrolsWithoutASession(t *testing.T) {
	h := handlerWithCreds(t)

	assert.NoError(t, h.mayRegister(registerRequest(t, "")))
}

// A second device is added by the person who already holds one, so proving a
// session is what separates "my phone" from a stranger at an open endpoint.
func TestASecondPasskeyEnrolsWithASession(t *testing.T) {
	h := handlerWithCreds(t)
	require.NoError(t, h.creds.save(credential("laptop"), "did:key:zowner", mastodonAccount))

	session, err := h.sessions.create("")
	require.NoError(t, err)

	assert.NoError(t, h.mayRegister(registerRequest(t, session)))
}

// Without a session an already-owned deployment must refuse, or anyone
// reaching the endpoint could enrol their own passkey and become an owner.
func TestASecondPasskeyIsRefusedWithoutASession(t *testing.T) {
	h := handlerWithCreds(t)
	require.NoError(t, h.creds.save(credential("laptop"), "did:key:zowner", mastodonAccount))

	require.Error(t, h.mayRegister(registerRequest(t, "")))
}

// A cookie that is not a live session is the same as no session.
func TestAnInvalidSessionDoesNotEnrol(t *testing.T) {
	h := handlerWithCreds(t)
	require.NoError(t, h.creds.save(credential("laptop"), "did:key:zowner", mastodonAccount))

	require.Error(t, h.mayRegister(registerRequest(t, "not-a-real-session")))
}
