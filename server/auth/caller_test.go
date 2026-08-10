package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testHandler() *Handler {
	return &Handler{sessions: newSessionStore(24)}
}

// A handler behind Middleware has to be able to tell who called it. Without
// this the only thing that reaches a handler is "someone authenticated".
func TestMiddlewarePutsTheCallerInContext(t *testing.T) {
	h := testHandler()
	session, err := h.sessions.create()
	require.NoError(t, err)

	var seen Caller
	var ok bool
	guarded := h.Middleware(func(_ http.ResponseWriter, r *http.Request) {
		seen, ok = CallerFrom(r.Context())
	})

	req := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	guarded(httptest.NewRecorder(), req)

	require.True(t, ok, "no caller reached the handler")
	assert.Equal(t, LevelUser, seen.Level)
	assert.Equal(t, NamespaceDefault, seen.Namespace)
}

// ADR-027 separates TOKEN from USER, so the two credentials cannot arrive
// indistinguishable — a token cannot mint tokens, and only a level says so.
func TestABearerTokenArrivesAsTokenNotUser(t *testing.T) {
	h := testHandler()
	store := newMemTokenStore()
	h.tokens = store

	raw, _, err := store.Create("ci", nil)
	require.NoError(t, err)

	var seen Caller
	guarded := h.Middleware(func(_ http.ResponseWriter, r *http.Request) {
		seen, _ = CallerFrom(r.Context())
	})

	req := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	guarded(httptest.NewRecorder(), req)

	assert.Equal(t, LevelToken, seen.Level)
}

// An unauthenticated request never reaches a handler, so nothing downstream
// has to consider a caller that is not there.
func TestNoCallerWithoutAuthentication(t *testing.T) {
	h := testHandler()

	reached := false
	guarded := h.Middleware(func(http.ResponseWriter, *http.Request) { reached = true })

	rec := httptest.NewRecorder()
	guarded(rec, httptest.NewRequest(http.MethodGet, "/api/attestations", nil))

	assert.False(t, reached)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
