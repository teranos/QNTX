package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "a stall under load should shed or throttle the heaviest caller, not end
// the process." — "Yes"
//
// The token that sent the most is turned away with a 429 that says when to
// come back, before anything about it is looked up; the one that sent less is
// still let in; and lifting lets both in.
func TestTheHeaviestTokenIsTurnedAwayAndLetBackIn(t *testing.T) {
	store := newMemTokenStore()
	heavy, _, err := store.Create(NewToken{Label: "ground", MintedBy: mastodonAccount, Level: LevelAttestor})
	require.NoError(t, err)
	light, _, err := store.Create(NewToken{Label: "laptop-cron", MintedBy: mastodonAccount, Level: LevelAttestor})
	require.NoError(t, err)

	shed := NewShed(5 * time.Second)
	h := &Handler{users: &memUsers{}, sessions: newSessionStore(1), tokens: store, logger: testLogger()}
	h.SetIdentities([]string{mastodonAccount}, nil)
	h.SetShed(shed)
	handler := h.Middleware("/test", everyLevel, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	send := func(raw string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/attestations", nil)
		req.Header.Set("Authorization", "Bearer "+raw)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	for range 5 {
		require.Equal(t, http.StatusOK, send(heavy).Code)
	}
	require.Equal(t, http.StatusOK, send(light).Code)

	label, sent, found := shed.Heaviest()
	require.True(t, found)
	assert.Equal(t, "ground", label)
	assert.Equal(t, 5, sent)

	refused := send(heavy)
	assert.Equal(t, http.StatusTooManyRequests, refused.Code)
	assert.Equal(t, "5", refused.Header().Get("Retry-After"))
	assert.Equal(t, http.StatusOK, send(light).Code, "a token that was not the heaviest was turned away")

	assert.Equal(t, []string{"ground"}, shed.Lift())
	assert.Equal(t, http.StatusOK, send(heavy).Code, "a token was still turned away once lifted")
}

// A person at the node is never turned away, whatever else the request
// carries.
func TestASessionIsNeverTurnedAway(t *testing.T) {
	shed := NewShed(5 * time.Second)
	shed.presented(sha256Hex("qntx_the_session"))
	_, _, found := shed.Heaviest()
	require.True(t, found)

	sessions := newSessionStore(1)
	token, err := sessions.create(mastodonAccount, User{ID: "UStim"})
	require.NoError(t, err)

	h := &Handler{users: &memUsers{}, sessions: sessions, tokens: newMemTokenStore(), logger: testLogger()}
	h.SetIdentities([]string{mastodonAccount}, nil)
	h.SetShed(shed)

	req := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	req.Header.Set("Authorization", "Bearer qntx_the_session")
	assert.False(t, h.presented(req).turnedAway)
}
