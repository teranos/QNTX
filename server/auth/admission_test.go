package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func testHandler() *Handler {
	h := &Handler{sessions: newSessionStore(24), logger: zap.NewNop().Sugar()}
	h.SetIdentities([]string{mastodonAccount}, nil)
	return h
}

// A handler behind Middleware has to be able to tell who called it. Without
// this the only thing that reaches a handler is "someone authenticated".
func TestMiddlewarePutsTheCallerInContext(t *testing.T) {
	h := testHandler()
	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)

	var seen Admission
	var ok bool
	guarded := h.Middleware(func(_ http.ResponseWriter, r *http.Request) {
		seen, ok = AdmissionFrom(r.Context())
	})

	req := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	guarded(httptest.NewRecorder(), req)

	require.True(t, ok, "no caller reached the handler")
	// Being listed is what admits and what makes SUPER, so a session that got
	// this far is SUPER by the same fact (ADR-027).
	assert.Equal(t, LevelSuper, seen.Level)
	assert.Equal(t, NamespaceDefault, seen.Namespace)
}

// A token reaches what its minter reaches, so it arrives at the minter's
// level. The grant is what still tells the two credentials apart, and minting
// stays out of reach because /auth/tokens is gated on the cookie.
func TestABearerTokenReachesWhatItsMinterReaches(t *testing.T) {
	h := testHandler()
	store := newMemTokenStore()
	h.tokens = store

	raw, _, err := store.Create(NewToken{Label: "ci", ExpiresAt: nil, MintedBy: mastodonAccount, ScopeRead: []string{"reads"}, ScopeWrite: []string{"writes"}})
	require.NoError(t, err)

	var seen Admission
	guarded := h.Middleware(func(_ http.ResponseWriter, r *http.Request) {
		seen, _ = AdmissionFrom(r.Context())
	})

	req := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	guarded(httptest.NewRecorder(), req)

	assert.Equal(t, LevelSuper, seen.Level)
	assert.Equal(t, mastodonAccount, seen.Identity)
	assert.NotNil(t, seen.Grant, "a session carries no grant; a token does")
}

// The one thing a token may never do, and it is the credential type that stops
// it rather than the level.
func TestABearerTokenCannotMintAToken(t *testing.T) {
	h := testHandler()
	store := newMemTokenStore()
	h.tokens = store

	raw, _, err := store.Create(NewToken{Label: "ci", MintedBy: mastodonAccount, ScopeRead: []string{"*"}})
	require.NoError(t, err)

	reached := false
	gated := h.sessionOnly(func(http.ResponseWriter, *http.Request, Presented) { reached = true })

	req := httptest.NewRequest(http.MethodPost, "/auth/tokens", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	gated(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.False(t, reached)
}

// Striking an account out of am.toml is the revocation (ADR-030). A session
// that outlives the list would make it a revocation with a 24-hour delay.
func TestASessionEndsWhenItsIdentityIsStruckOut(t *testing.T) {
	h := testHandler()
	h.logger = zap.NewNop().Sugar()
	h.SetIdentities([]string{"https://mastodon.example/@tim"}, nil)
	session, err := h.sessions.create("https://mastodon.example/@tim", User{})
	require.NoError(t, err)

	reached := false
	guarded := h.Middleware(func(http.ResponseWriter, *http.Request) { reached = true })

	call := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
		rec := httptest.NewRecorder()
		guarded(rec, req)
		return rec
	}

	require.Equal(t, http.StatusOK, call().Code)
	require.True(t, reached, "an admitted session was refused")

	reached = false
	h.SetIdentities([]string{"https://mastodon.example/@someone-else"}, nil)

	assert.Equal(t, http.StatusUnauthorized, call().Code)
	assert.False(t, reached, "the session outlived the list that admitted it")
}

// A token speaks for whoever minted it, so revoking them has to reach it —
// otherwise revoking an account leaves behind credentials nothing enumerates.
func TestATokenDiesWithTheIdentityThatMintedIt(t *testing.T) {
	h := testHandler()
	h.logger = zap.NewNop().Sugar()
	h.SetIdentities([]string{"https://mastodon.example/@tim"}, nil)
	store := newMemTokenStore()
	h.tokens = store

	raw, _, err := store.Create(NewToken{
		Label:     "ci",
		MintedBy:  "https://mastodon.example/@tim",
		ScopeRead: []string{"reads"},
	})
	require.NoError(t, err)

	reached := false
	guarded := h.Middleware(func(http.ResponseWriter, *http.Request) { reached = true })

	call := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
		req.Header.Set("Authorization", "Bearer "+raw)
		rec := httptest.NewRecorder()
		guarded(rec, req)
		return rec
	}

	require.Equal(t, http.StatusOK, call().Code)
	require.True(t, reached, "a token minted by a listed identity was refused")

	reached = false
	h.SetIdentities([]string{"https://mastodon.example/@someone-else"}, nil)

	assert.Equal(t, http.StatusUnauthorized, call().Code)
	assert.False(t, reached, "the token outlived the identity that minted it")
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
