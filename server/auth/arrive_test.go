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

// A handler whose pending ticket admits mastodonAccount, which is the state a
// person is in between proving a route and answering for a device.
func arrivingHandler(t *testing.T) (*Handler, *memUsers, string) {
	t.Helper()
	store := &memUsers{}
	h := &Handler{
		users:         store,
		sessions:      newSessionStore(24),
		pendingLogins: pendingLogins{},
		logger:        zap.NewNop().Sugar(),
	}
	h.joinUser(mastodonAccount, mastodonBinding("@tim@mastodon.example"), "did:key:zBrowser")

	ticket, err := h.pendingLogins.open(mastodonAccount)
	require.NoError(t, err)
	return h, store, ticket
}

func arrive(h *Handler, ticket, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/auth/user/arrive", strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: pendingCookieName, Value: ticket})
	rec := httptest.NewRecorder()
	h.HandleArrive(rec, req)
	return rec
}

// Every User has a username and an email, and this is where they get one.
func TestArrivingRecordsTheNameAndEmail(t *testing.T) {
	h, store, ticket := arrivingHandler(t)

	rec := arrive(h, ticket, `{"username":"tim","email":"tim@example.com"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	require.Len(t, store.held, 1)
	assert.Equal(t, "tim", store.held[0].Username)
	assert.Equal(t, []string{"tim@example.com"}, store.held[0].EmailAddresses)
	assert.True(t, store.held[0].Arrived())
}

// A User that has said who they are is not renamed by whoever holds a ticket.
func TestArrivingTwiceIsRefused(t *testing.T) {
	h, store, ticket := arrivingHandler(t)

	require.Equal(t, http.StatusOK, arrive(h, ticket, `{"username":"tim","email":"tim@example.com"}`).Code)
	rec := arrive(h, ticket, `{"username":"someone-else","email":"other@example.com"}`)

	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, "tim", store.held[0].Username)
}

// Half an answer is not an answer. A User with a name and no email has not
// arrived, so nothing is written.
func TestArrivingNeedsBoth(t *testing.T) {
	h, store, ticket := arrivingHandler(t)

	for _, body := range []string{
		`{"username":"tim","email":""}`,
		`{"username":"","email":"tim@example.com"}`,
		`{"username":"t","email":"tim@example.com"}`,
		`{"username":"tim","email":"tim-at-example"}`,
		`{"username":"tim","email":"@example.com"}`,
	} {
		rec := arrive(h, ticket, body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, body)
	}

	require.Len(t, store.held, 1)
	assert.False(t, store.held[0].Arrived())
}

// The gate is the admission, not the request. Nothing that has not proved a
// route gets to name a User.
func TestArrivingNeedsAnAdmission(t *testing.T) {
	h, _, _ := arrivingHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/auth/user/arrive",
		strings.NewReader(`{"username":"tim","email":"tim@example.com"}`))
	rec := httptest.NewRecorder()
	h.HandleArrive(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// The admission says whether the glyph has to ask, and a fresh User has not
// chosen anything yet.
func TestAFreshUserHasNotArrived(t *testing.T) {
	_, store, _ := arrivingHandler(t)

	require.Len(t, store.held, 1)
	assert.False(t, store.held[0].Arrived())
	assert.Empty(t, store.held[0].Username)
}
