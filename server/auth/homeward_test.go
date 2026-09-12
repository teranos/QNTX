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
	// Marked, because the ticket is HttpOnly and the page at home cannot read
	// it. Without the mark a browser already signed in here draws no door, runs
	// no passkey, and nothing ever calls sentHome.
	assert.Equal(t, h.webauthn.Config.RPOrigins[0]+"?homeward=1", w.Header().Get("Location"))

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

// An app's page is at a scheme, and no fetch carries a scheme as its Origin.
// So the app names the door it collects for, the way its navigation named it,
// and the ticket is the secret. A name that is not the journey's own is no
// better than a stranger's Origin.
func TestAnAppCollectsItsHeldSessionByNamingItsDoor(t *testing.T) {
	h := handlerWithDoors(t, gardenWithAnApp())
	h.heldSessions.Store("the-ticket", heldSession{
		token: "s3ss", identity: mastodonAccount, door: "garden://door", heldAt: time.Now(),
	})

	collect := func(named string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, homewardResultPath+"?home=the-ticket&door="+named, nil)
		r.Header.Set("Origin", "tauri://localhost")
		w := httptest.NewRecorder()
		h.handleHomewardResult(w, r)
		return w
	}

	elsewhere := collect("https%3A%2F%2Fportal.garden.test")
	assert.Equal(t, http.StatusUnauthorized, elsewhere.Code)
	assert.NotContains(t, elsewhere.Body.String(), "s3ss")

	atDoor := collect("garden%3A%2F%2Fdoor")
	require.Equal(t, http.StatusOK, atDoor.Code, atDoor.Body.String())
	assert.Contains(t, atDoor.Body.String(), "s3ss")
}

// An app that already proved a route carries its half-admission home, so home
// asks for the passkey and not for the provider a second time. The way home
// hands Safari the same cookie the app could not hold. A ticket this node
// never opened hands it nothing.
func TestTheWayHomeCarriesTheAppsHalfAdmission(t *testing.T) {
	h := handlerWithDoors(t, gardenWithAnApp())
	pending, err := h.pendingLogins.open(mastodonAccount)
	require.NoError(t, err)

	r := httptest.NewRequest(http.MethodGet, homewardPath+"?door=garden%3A%2F%2Fdoor&pending="+pending, nil)
	w := httptest.NewRecorder()
	h.handleHomeward(w, r)
	require.Equal(t, http.StatusFound, w.Code, w.Body.String())

	var carried *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == pendingCookieName {
			carried = c
		}
	}
	require.NotNil(t, carried, "the way home set no half-admission")
	assert.Equal(t, pending, carried.Value)

	stranger := httptest.NewRequest(http.MethodGet, homewardPath+"?door=garden%3A%2F%2Fdoor&pending=not-a-ticket", nil)
	w = httptest.NewRecorder()
	h.handleHomeward(w, stranger)
	require.Equal(t, http.StatusFound, w.Code)
	for _, c := range w.Result().Cookies() {
		assert.NotEqual(t, pendingCookieName, c.Name, "a ticket this node never opened was handed over")
	}
}

// Home, holding a half-admission, says so: the press there is the passkey.
func TestStatusNamesAHeldHalfAdmission(t *testing.T) {
	h := handlerWithDoors(t, garden())
	pending, err := h.pendingLogins.open(mastodonAccount)
	require.NoError(t, err)

	r := httptest.NewRequest(http.MethodGet, "/auth/status", nil)
	r.AddCookie(&http.Cookie{Name: pendingCookieName, Value: pending})
	w := httptest.NewRecorder()
	h.handleStatus(w, r)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var said struct {
		HalfAdmitted string `json:"half_admitted"`
		Next         string `json:"next"`
		Identity     string `json:"identity"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &said))
	assert.Equal(t, mastodonAccount, said.HalfAdmitted)
	assert.Equal(t, "enrol", said.Next)
	assert.Empty(t, said.Identity, "a half-admission is not a session")
}
