package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func carrying(t *testing.T, session, pending string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/auth/register/begin", nil)
	if session != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	}
	if pending != "" {
		req.AddCookie(&http.Cookie{Name: pendingCookieName, Value: pending})
	}
	return req
}

// A session that names nobody is still a session. An ungoverned deployment
// issues only that kind, so folding the two questions together would refuse
// every second device on every passkey-only install.
func TestASessionNamingNobodyIsStillPresented(t *testing.T) {
	h := handlerWithCreds(t)
	token, err := h.sessions.create("", User{})
	require.NoError(t, err)

	p := h.presented(carrying(t, token, ""))

	identity, signedIn := p.Admitted()
	assert.True(t, signedIn)
	assert.Equal(t, "", identity)

	// Nobody is not somebody to enrol on behalf of.
	_, enrolling := p.Enrolling()
	assert.False(t, enrolling)
}

// A session answers for the request whatever it is called. Falling through to a
// pending cookie beside it would let a half-admission speak over a live one.
func TestASessionAnswersOverAPendingBesideIt(t *testing.T) {
	h := handlerAdmitting(t, mastodonAccount)
	token, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)
	pending, err := h.pendingLogins.open(atprotoAccount)
	require.NoError(t, err)

	enrolling, ok := h.presented(carrying(t, token, pending)).Enrolling()
	assert.True(t, ok)
	assert.Equal(t, mastodonAccount, enrolling)
}

// Expiry is part of reading the cookie, not a check each gate remembers to
// make. A session past its end has to present as no session at all.
func TestAnExpiredSessionPresentsNothing(t *testing.T) {
	h := handlerAdmitting(t, mastodonAccount)
	h.sessions = newSessionStore(0)
	token, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)

	p := h.presented(carrying(t, token, ""))

	_, signedIn := p.Admitted()
	assert.False(t, signedIn)
	assert.Equal(t, "", p.UserID)
}

// The half-admission is the whole of what a login ceremony stands on, and it is
// not a session — nothing a gate asks Admitted() for may be answered by one.
func TestAPendingIsNotASession(t *testing.T) {
	h := handlerAdmitting(t, mastodonAccount)
	pending, err := h.pendingLogins.open(mastodonAccount)
	require.NoError(t, err)

	p := h.presented(carrying(t, "", pending))

	half, held := p.HalfAdmitted()
	assert.True(t, held)
	assert.Equal(t, mastodonAccount, half)

	_, signedIn := p.Admitted()
	assert.False(t, signedIn)
}

// A session beside a client's bearer is still a session past the gate: what the
// gate read is read once, the session with it.
func TestASessionBesideABearerIsStillASessionPastTheGate(t *testing.T) {
	h := handlerAdmitting(t, mastodonAccount)
	store := newMemTokenStore()
	h.tokens = store
	raw, _, err := store.issueThroughClient(NewToken{Label: "connector", MintedBy: mastodonAccount, Level: LevelToken, ExpiresAt: NeverEnds()})
	require.NoError(t, err)
	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)

	reached := false
	gate := h.Middleware("/auth/tokens", everyLevel, h.sessionOnly(func(http.ResponseWriter, *http.Request, Presented) { reached = true }))
	req := carrying(t, session, "")
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	gate(rec, req)
	assert.True(t, reached, "the session was not a session past the gate: %d %s", rec.Code, rec.Body.String())
}
