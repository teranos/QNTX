package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "user should be able to disable their acc, but reawaken (enable) it later as well"

// Tim de Facile, listed and holding a session, with a store to switch.
func switchingHandler(t *testing.T) (*Handler, *memUsers, string) {
	t.Helper()
	h, store, session := arrivingHandler(t)
	h.SetIdentities([]string{mastodonAccount}, nil)
	require.Equal(t, http.StatusOK, arrive(h, session, `{"display_name":"Tim de Facile"}`).Code)
	return h, store, session
}

func flip(h *Handler, session, verb string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/auth/user/"+verb, nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	rec := httptest.NewRecorder()
	switch verb {
	case "disable":
		h.HandleDisable(rec, req)
	case "enable":
		h.HandleEnable(rec, req)
	}
	return rec
}

// A gated request with this session, answered by the table's own gate.
func gatedWith(h *Handler, session string) *httptest.ResponseRecorder {
	guarded := h.Middleware(everyLevel, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	rec := httptest.NewRecorder()
	guarded(rec, req)
	return rec
}

// Tim switches himself off, is admitted nowhere, and switches himself back on.
func TestTimDeFacileSwitchesHimselfOffAndOnAgain(t *testing.T) {
	h, store, session := switchingHandler(t)
	tim := store.held[0]
	require.Equal(t, http.StatusOK, gatedWith(h, session).Code)

	rec := flip(h, session, "disable")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, tim.ID, store.held[0].DisabledBy)

	rec = gatedWith(h, session)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "switched off")

	rec = flip(h, session, "enable")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Empty(t, store.held[0].DisabledBy)
	assert.Equal(t, http.StatusOK, gatedWith(h, session).Code)
}

// Switched off, Tim mints nothing: what a token does outlives the session,
// and a token he minted before stops with him.
func TestASwitchedOffTimMintsNothingAndHisTokensStop(t *testing.T) {
	h, store, session := switchingHandler(t)
	tim := store.held[0]
	require.Equal(t, http.StatusOK, flip(h, session, "disable").Code)

	reached := false
	minting := h.sessionOnly(func(http.ResponseWriter, *http.Request, Presented) { reached = true })
	req := httptest.NewRequest(http.MethodPost, "/auth/tokens", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	rec := httptest.NewRecorder()
	minting(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.False(t, reached)

	admitted, ok := h.admissionOf(Presented{Bearer: &Grant{
		Level: LevelToken, MintedBy: mastodonAccount, MintedByUser: tim.ID,
	}})
	require.True(t, ok)
	by, err := h.switchedOff(admitted)
	require.NoError(t, err)
	assert.Equal(t, tim.ID, by)
}

// What ROOT switched off is not Tim's to switch on. The record says who did
// it, and only they undo it.
func TestWhatRootSwitchedOffStaysOffForTim(t *testing.T) {
	h, store, session := switchingHandler(t)
	store.held[0].DisabledBy = "US-ROOT-1"

	rec := flip(h, session, "enable")
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "US-ROOT-1")
	assert.Equal(t, "US-ROOT-1", store.held[0].DisabledBy)
}

// Switching needs a session. A half-admission is not a person deciding.
func TestSwitchingNeedsASession(t *testing.T) {
	h, store, _ := switchingHandler(t)
	for _, verb := range []string{"disable", "enable"} {
		rec := flip(h, "", verb)
		assert.Equal(t, http.StatusForbidden, rec.Code, verb)
	}
	assert.Empty(t, store.held[0].DisabledBy)
}
