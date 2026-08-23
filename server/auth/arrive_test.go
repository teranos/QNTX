package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// A handler holding a session for mastodonAccount, which is the state a person
// is in once a device has answered for the route they proved.
func arrivingHandler(t *testing.T) (*Handler, *memUsers, string) {
	t.Helper()
	store := &memUsers{}
	h := &Handler{
		users:         store,
		sessions:      newSessionStore(24),
		pendingLogins: pendingLogins{},
		logger:        zap.NewNop().Sugar(),
	}
	u, err := h.joinUser(mastodonAccount, mastodonBinding("@tim@mastodon.example"), "did:key:zBrowser")
	require.NoError(t, err)

	session, err := h.sessions.create(mastodonAccount, u)
	require.NoError(t, err)
	return h, store, session
}

func arrive(h *Handler, session, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/auth/user/arrive", strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	rec := httptest.NewRecorder()
	h.HandleArrive(rec, req)
	return rec
}

// What a person chose to say lands on the User.
func TestArrivingRecordsTheNameAndEmail(t *testing.T) {
	h, store, ticket := arrivingHandler(t)

	rec := arrive(h, ticket, `{"display_name":"tim","email":"tim@example.com"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	require.Len(t, store.held, 1)
	assert.Equal(t, "tim", store.held[0].DisplayName)
	assert.Equal(t, []string{"tim@example.com"}, store.held[0].EmailAddresses)
}

// Neither field is required. The User already exists — proving a listed route
// is what made them — so a person who says nothing is answered, not refused.
func TestArrivingRequiresNothing(t *testing.T) {
	h, store, ticket := arrivingHandler(t)

	for _, body := range []string{
		`{}`,
		`{"display_name":"","email":""}`,
		`{"display_name":"tim"}`,
	} {
		rec := arrive(h, ticket, body)
		assert.Equal(t, http.StatusOK, rec.Code, body)
	}

	require.Len(t, store.held, 1)
	assert.Equal(t, "tim", store.held[0].DisplayName)
	assert.Empty(t, store.held[0].EmailAddresses)
}

// A display_name can never be taken back, so an admission nobody finished must
// not be able to leave one. The half-admission laye issues is not a session.
func TestAHalfAdmissionCannotName(t *testing.T) {
	h, store, _ := arrivingHandler(t)
	ticket, err := h.pendingLogins.open(mastodonAccount)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/auth/user/arrive",
		strings.NewReader(`{"display_name":"tim"}`))
	req.AddCookie(&http.Cookie{Name: pendingCookieName, Value: ticket})
	rec := httptest.NewRecorder()
	h.HandleArrive(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Empty(t, store.held[0].DisplayName)
}

// A name is settled once, so whoever holds a session cannot rename someone who
// is already called something.
func TestANameIsSettledOnce(t *testing.T) {
	h, store, ticket := arrivingHandler(t)

	require.Equal(t, http.StatusOK, arrive(h, ticket, `{"display_name":"tim"}`).Code)
	rec := arrive(h, ticket, `{"display_name":"someone-else"}`)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "settled once")
	assert.Equal(t, "tim", store.held[0].DisplayName)
}

// An email that arrives after a name is added rather than refused, because a
// User has any number of them and only the name is settled once.
func TestAnEmailArrivesAfterTheName(t *testing.T) {
	h, store, ticket := arrivingHandler(t)

	require.Equal(t, http.StatusOK, arrive(h, ticket, `{"display_name":"tim"}`).Code)
	require.Equal(t, http.StatusOK, arrive(h, ticket, `{"email":"tim@example.com"}`).Code)
	require.Equal(t, http.StatusOK, arrive(h, ticket, `{"email":"tim@example.com"}`).Code)

	assert.Equal(t, []string{"tim@example.com"}, store.held[0].EmailAddresses)
}

// root is what the ROOT User is called without setting it, so it is not a name
// anyone takes — the ROOT User least of all.
func TestRootIsNotANameToTake(t *testing.T) {
	h, store, ticket := arrivingHandler(t)

	for _, body := range []string{`{"display_name":"root"}`, `{"display_name":"ROOT"}`} {
		rec := arrive(h, ticket, body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, body)
	}
	assert.Empty(t, store.held[0].DisplayName)
}

// An address this node cannot read is refused, and refusing it writes nothing.
func TestAnUnreadableEmailIsRefused(t *testing.T) {
	h, store, ticket := arrivingHandler(t)

	for _, body := range []string{
		`{"display_name":"tim","email":"tim-at-example"}`,
		`{"display_name":"tim","email":"@example.com"}`,
		`{"display_name":"tim","email":"tim@example"}`,
	} {
		rec := arrive(h, ticket, body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, body)
	}

	require.Len(t, store.held, 1)
	assert.Empty(t, store.held[0].DisplayName)
	assert.Empty(t, store.held[0].EmailAddresses)
}

// The gate is the admission, not the request. Nothing that has not proved a
// route gets to name a User.
func TestArrivingNeedsAnAdmission(t *testing.T) {
	h, _, _ := arrivingHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/auth/user/arrive",
		strings.NewReader(`{"display_name":"tim","email":"tim@example.com"}`))
	rec := httptest.NewRecorder()
	h.HandleArrive(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// The first User is the ROOT User, and the ROOT User is called root before
// saying anything.
func TestAFreshRootIsCalledRoot(t *testing.T) {
	_, store, _ := arrivingHandler(t)

	require.Len(t, store.held, 1)
	assert.Empty(t, store.held[0].DisplayName)
	assert.Equal(t, RootName, store.held[0].Name())
}
