package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// everyLevel is for tests about who is presenting rather than about what a
// route lets in. Reach is what those other tests below are for.
var everyLevel = Also(LevelSuper, LevelToken, LevelAttestor, LevelPublicRegistration)

// A line that grants ROOT and nobody else lets in ROOT and nobody else.
func TestALineGrantingOnlyRootLetsInOnlyRoot(t *testing.T) {
	h := testHandler()
	store := newMemTokenStore()
	h.tokens = store

	raw, _, err := store.Create(NewToken{Label: "ci", MintedBy: mastodonAccount, Level: LevelToken})
	require.NoError(t, err)

	reached := false
	guarded := h.Middleware(Also(), func(http.ResponseWriter, *http.Request) { reached = true })

	req := httptest.NewRequest(http.MethodGet, "/pond/ledger", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	w := httptest.NewRecorder()
	guarded(w, req)

	assert.False(t, reached, "a token reached a route granted to ROOT alone")
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// 401 says the credential is the problem, and a caller told that presents it
// again. This caller is admitted and the route is not theirs.
func TestBeingTurnedAwayFromARouteIsNotBeingTurnedAwayFromTheNode(t *testing.T) {
	h := testHandler()
	store := newMemTokenStore()
	h.tokens = store

	raw, _, err := store.Create(NewToken{Label: "ci", MintedBy: mastodonAccount, Level: LevelToken})
	require.NoError(t, err)

	guarded := h.Middleware(Also(), func(http.ResponseWriter, *http.Request) {})
	req := httptest.NewRequest(http.MethodGet, "/pond/ledger", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	w := httptest.NewRecorder()
	guarded(w, req)

	assert.NotEqual(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// ROOT is never listed on a Reach, and reaches everything anyway.
func TestRootReachesARouteThatNamesNobody(t *testing.T) {
	h := testHandler()
	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)

	reached := false
	guarded := h.Middleware(Also(), func(http.ResponseWriter, *http.Request) { reached = true })

	req := httptest.NewRequest(http.MethodGet, "/pond/ledger", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	w := httptest.NewRecorder()
	guarded(w, req)

	assert.True(t, reached, "ROOT was turned away from a route")
	assert.Equal(t, http.StatusOK, w.Code)
}

// A level goes through because the route named it, and only the one named.
func TestALevelGoesThroughTheRouteThatNamesIt(t *testing.T) {
	h := testHandler()
	store := newMemTokenStore()
	h.tokens = store

	forToken, _, err := store.Create(NewToken{Label: "writer", MintedBy: mastodonAccount, Level: LevelToken})
	require.NoError(t, err)
	forSuper, _, err := store.Create(NewToken{Label: "operator", MintedBy: mastodonAccount, Level: LevelSuper})
	require.NoError(t, err)

	onlySuper := Also(LevelSuper)

	for _, c := range []struct {
		what    string
		raw     string
		through bool
	}{
		{"the level the route names", forSuper, true},
		{"a level it does not", forToken, false},
	} {
		reached := false
		guarded := h.Middleware(onlySuper, func(http.ResponseWriter, *http.Request) { reached = true })

		req := httptest.NewRequest(http.MethodPost, "/pond/keeper", nil)
		req.Header.Set("Authorization", "Bearer "+c.raw)
		guarded(httptest.NewRecorder(), req)

		assert.Equal(t, c.through, reached, c.what)
	}
}

// The levels a Reach names are readable, so what a node serves can be printed
// rather than inferred.
func TestAReachSaysWhatItNames(t *testing.T) {
	assert.Empty(t, Also().Beyond())
	assert.Equal(t, []Level{LevelSuper, LevelToken}, Also(LevelSuper, LevelToken).Beyond())
}
