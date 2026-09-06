package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "could you create a ts Users glyph to let us do the minimal management of users as ROOT ?"

// ROOT with a session, and Tim de Facile as a second User in the store.
func rootAndTim(t *testing.T) (*Handler, *memUsers, string, User) {
	t.Helper()
	h, store, rootSession := switchingHandler(t)
	tim := User{
		ID: "US-TIM-1", DisplayName: "Tim de Facile", Level: LevelAttestor,
		Keys: []UserKey{{DID: "did:key:zTim", Origin: OriginBrowser}}, CreatedAt: 2,
	}
	require.NoError(t, store.Put(tim))
	return h, store, rootSession, tim
}

func asRoot(h *Handler, session, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	rec := httptest.NewRecorder()
	if path == "/auth/users" {
		h.sessionOnly(h.usersCollection)(rec, req)
	} else {
		h.sessionOnly(h.handleUserByID)(rec, req)
	}
	return rec
}

// ROOT sees every User as the record holds them.
func TestRootListsEveryUser(t *testing.T) {
	h, store, rootSession, tim := rootAndTim(t)

	rec := asRoot(h, rootSession, http.MethodGet, "/auth/users")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var listed []User
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listed))
	require.Len(t, listed, 2)
	assert.Equal(t, store.held[0].ID, listed[0].ID)
	assert.Equal(t, tim.DisplayName, listed[1].DisplayName)
}

// ROOT switches Tim off, and it is ROOT's name on the record. Tim cannot
// reawaken; ROOT switches him on again.
func TestRootSwitchesTimOffAndTimStaysOffUntilRootSaysOtherwise(t *testing.T) {
	h, store, rootSession, tim := rootAndTim(t)
	root := store.held[0]

	rec := asRoot(h, rootSession, http.MethodPost, "/auth/users/"+tim.ID+"/disable")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, root.ID, store.held[1].DisabledBy)

	timSession, err := h.sessions.create("did:key:zTim", tim)
	require.NoError(t, err)
	rec = flip(h, timSession, "enable")
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Equal(t, root.ID, store.held[1].DisabledBy)

	rec = asRoot(h, rootSession, http.MethodPost, "/auth/users/"+tim.ID+"/enable")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Empty(t, store.held[1].DisabledBy)
}

// A User nobody holds is said so, and a verb that is not the switch is refused.
func TestRootNamesAUserThatIsNotThere(t *testing.T) {
	h, _, rootSession, _ := rootAndTim(t)

	rec := asRoot(h, rootSession, http.MethodPost, "/auth/users/US-NOBODY-1/disable")
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "US-NOBODY-1")

	rec = asRoot(h, rootSession, http.MethodPost, "/auth/users/US-TIM-1/erase")
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}
