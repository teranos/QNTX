package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The way home is a navigation from a door, so it carries a Referer and no
// Origin, the way a top-level GET does.
func wayHome(referer string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, homewardPath, nil)
	if referer != "" {
		r.Header.Set("Referer", referer)
	}
	return r
}

func homewardCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == homewardCookieName {
			return c
		}
	}
	t.Fatal("the way home set no ticket")
	return nil
}

// A door sends a root identity home for the passkey. Home is this node's own
// web origin, and the browser leaves carrying a ticket that says which door.
func TestTheWayHomeLeadsToTheNodeAndRemembersTheDoor(t *testing.T) {
	h := handlerWithDoors(t, garden())

	w := httptest.NewRecorder()
	h.handleHomeward(w, wayHome("https://portal.garden.test/"))

	require.Equal(t, http.StatusFound, w.Code, w.Body.String())
	assert.Equal(t, h.webauthn.Config.RPOrigins[0], w.Header().Get("Location"))

	ticket := homewardCookie(t, w)
	assert.True(t, ticket.HttpOnly)
	assert.Equal(t, http.SameSiteLaxMode, ticket.SameSite)

	val, ok := h.homewards.Load(ticket.Value)
	require.True(t, ok, "the ticket names no journey")
	assert.Equal(t, "https://portal.garden.test", val.(homeward).door)
}

// A stranger's page can link here too. It is told only that it is refused,
// because a refusal naming the doors would be a directory of them.
func TestTheWayHomeIsOnlyFromADoor(t *testing.T) {
	h := handlerWithDoors(t, garden())

	for _, referer := range []string{"https://stranger.test/", ""} {
		w := httptest.NewRecorder()
		h.handleHomeward(w, wayHome(referer))
		assert.Equal(t, http.StatusUnauthorized, w.Code, referer)
		assert.NotContains(t, w.Body.String(), "garden")
	}
}

// The passkey finished at home. The session it made is held under the ticket
// and the browser is told where to go: the door, with the ticket on the URL.
func TestAPasskeyDoneAtHomeSendsThePersonBackToTheDoor(t *testing.T) {
	h := handlerWithDoors(t, garden())
	h.homewards.Store("the-ticket", homeward{door: "https://portal.garden.test", startedAt: time.Now()})

	r := httptest.NewRequest(http.MethodPost, "/auth/login/finish", nil)
	r.AddCookie(&http.Cookie{Name: homewardCookieName, Value: "the-ticket"})
	w := httptest.NewRecorder()

	back := h.sentHome(w, r, "s3ss", mastodonAccount)

	assert.Equal(t, "https://portal.garden.test?home=the-ticket", back)
	val, ok := h.heldSessions.Load("the-ticket")
	require.True(t, ok, "the session was not held for the door")
	assert.Equal(t, "s3ss", val.(heldSession).token)
	_, open := h.homewards.Load("the-ticket")
	assert.False(t, open, "a journey is spent by arriving")
}

// A finish with no ticket is an ordinary login at home, and goes nowhere.
func TestAFinishWithNoTicketStaysHome(t *testing.T) {
	h := handlerWithDoors(t, garden())
	r := httptest.NewRequest(http.MethodPost, "/auth/login/finish", nil)
	w := httptest.NewRecorder()
	assert.Equal(t, "", h.sentHome(w, r, "s3ss", mastodonAccount))
}

// Only the door the journey started from collects the session, and once.
func TestAHeldSessionIsCollectedOnceAndOnlyByItsDoor(t *testing.T) {
	h := handlerWithDoors(t, garden())
	h.heldSessions.Store("the-ticket", heldSession{
		token: "s3ss", identity: mastodonAccount, door: "https://portal.garden.test", heldAt: time.Now(),
	})

	collect := func(origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, homewardResultPath+"?home=the-ticket", nil)
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		h.handleHomewardResult(w, r)
		return w
	}

	elsewhere := collect("https://app.garden.test")
	assert.Equal(t, http.StatusUnauthorized, elsewhere.Code)
	assert.NotContains(t, elsewhere.Body.String(), "s3ss")

	atDoor := collect("https://portal.garden.test")
	require.Equal(t, http.StatusOK, atDoor.Code, atDoor.Body.String())
	var got struct {
		Session    string `json:"session"`
		AdmittedAs string `json:"admitted_as"`
		Next       string `json:"next"`
	}
	require.NoError(t, json.Unmarshal(atDoor.Body.Bytes(), &got))
	assert.Equal(t, "s3ss", got.Session)
	assert.Equal(t, mastodonAccount, got.AdmittedAs)
	assert.Equal(t, "nothing", got.Next)

	again := collect("https://portal.garden.test")
	assert.Equal(t, http.StatusNotFound, again.Code)
}
