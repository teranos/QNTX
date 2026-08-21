package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// statusWithCookie is what the glyph asks on load. identity is the field it
// reads to decide whether anyone is signed in.
func statusWithCookie(t *testing.T, h *Handler, token string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/auth/status", nil)
	if token != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	}
	rec := httptest.NewRecorder()
	h.handleStatus(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body
}

// The one the UI got wrong: the session was invalidated and the cookie cleared,
// and the glyph still showed signed in because it asked something else.
func TestStatusForgetsTheIdentityAfterLogout(t *testing.T) {
	h := handlerWithCreds(t)

	token, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)
	assert.Equal(t, mastodonAccount, statusWithCookie(t, h, token)["identity"])

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec := httptest.NewRecorder()
	h.handleLogout(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	assert.Equal(t, "", statusWithCookie(t, h, token)["identity"])
}

// A browser that presents nothing is nobody, which is what it looks like once
// the cleared cookie has been accepted.
func TestStatusNamesNobodyWithoutACookie(t *testing.T) {
	h := handlerWithCreds(t)

	_, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)

	assert.Equal(t, "", statusWithCookie(t, h, "")["identity"])
}

// The deletion only lands if the browser accepts it, and it accepts it only
// when the flags match the cookie that was set.
func TestLogoutClearsTheCookieItSet(t *testing.T) {
	h := handlerWithCreds(t)
	h.secureCookies = true

	setRec := httptest.NewRecorder()
	h.setSessionCookie(setRec, "a-token")
	set := setRec.Result().Cookies()[0]

	clearRec := httptest.NewRecorder()
	h.clearSessionCookie(clearRec)
	cleared := clearRec.Result().Cookies()[0]

	assert.Equal(t, set.Name, cleared.Name)
	assert.Equal(t, set.Path, cleared.Path)
	assert.Equal(t, set.Secure, cleared.Secure)
	assert.Equal(t, set.HttpOnly, cleared.HttpOnly)
	assert.Equal(t, set.SameSite, cleared.SameSite)
	assert.True(t, cleared.MaxAge < 0)
}

// A token that logged out cannot be presented again by anyone who kept it.
func TestASpentSessionStopsNamingAnyone(t *testing.T) {
	h := handlerWithCreds(t)

	token, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	h.handleLogout(httptest.NewRecorder(), req)

	_, ok := h.sessions.identityOf(token)
	assert.False(t, ok)
}
