package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func turnedAway(h *Handler, authorize string) {
	guarded := h.Middleware(func(http.ResponseWriter, *http.Request) {})
	req := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	if authorize != "" {
		req.Header.Set("Authorization", "Bearer "+authorize)
	}
	guarded(httptest.NewRecorder(), req)
}

// A browser that has not signed in is not a fault. Counting it is still worth
// doing, because a count going nowhere is how you tell nothing is trying.
func TestARefusedCallerIsCounted(t *testing.T) {
	h := testHandler()

	turnedAway(h, "")
	turnedAway(h, "")

	away, stale := h.Refusals()
	assert.Equal(t, int64(2), away)
	assert.Equal(t, int64(0), stale, "a caller with no token was counted as one")
}

// The signal worth seeing: something presenting a credential that resolves to
// nothing. A person retries once; this retries until somebody replaces it.
func TestATokenThatResolvesToNothingIsCountedApart(t *testing.T) {
	h := testHandler()
	h.tokens = newMemTokenStore()

	turnedAway(h, "a-token-this-node-never-minted")

	away, stale := h.Refusals()
	assert.Equal(t, int64(1), away)
	assert.Equal(t, int64(1), stale)
}

// A token that resolves and is then refused is the same problem wearing a
// different failure: the machine holding it cannot fix itself either.
func TestATokenWhoseMinterWasStruckOutIsCountedApart(t *testing.T) {
	h := testHandler()
	store := newMemTokenStore()
	h.tokens = store

	raw, _, err := store.Create(NewToken{Label: "ci", MintedBy: mastodonAccount, ScopeRead: []string{"reads"}})
	require.NoError(t, err)

	h.SetIdentities([]string{atprotoAccount}, nil)
	turnedAway(h, raw)

	away, stale := h.Refusals()
	assert.Equal(t, int64(1), away)
	assert.Equal(t, int64(1), stale)
}

// Minting is session-only, so a token reaching it is refused there rather than
// in Middleware — and that refusal has to reach the same count.
func TestARefusalAtTheMintIsCounted(t *testing.T) {
	h := testHandler()
	store := newMemTokenStore()
	h.tokens = store

	raw, _, err := store.Create(NewToken{Label: "ci", MintedBy: mastodonAccount, ScopeRead: []string{"reads"}})
	require.NoError(t, err)

	gated := h.sessionOnly(func(http.ResponseWriter, *http.Request, Presented) {})
	req := httptest.NewRequest(http.MethodPost, "/auth/tokens", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	gated(httptest.NewRecorder(), req)

	away, stale := h.Refusals()
	assert.Equal(t, int64(1), away)
	assert.Equal(t, int64(1), stale)
}

// An admitted caller is not a refusal, or the count says more is wrong than is.
func TestAnAdmittedCallerIsNotCounted(t *testing.T) {
	h := testHandler()
	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)

	guarded := h.Middleware(func(http.ResponseWriter, *http.Request) {})
	req := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	guarded(httptest.NewRecorder(), req)

	away, _ := h.Refusals()
	assert.Equal(t, int64(0), away)
}
