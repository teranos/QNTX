package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/sigil"
)

// Reading where you stand and stepping are two sigils on one path. Before,
// GET answered 405, and where you stood was only learned by stepping.
func TestStandingIsReadAndSteppedOnOnePath(t *testing.T) {
	signum := (&QNTXServer{}).iSignum()
	require.NoError(t, signum.Check())

	methods := map[string]string{}
	for _, held := range signum.GetSigils() {
		assert.Equal(t, "/i/standing", held.GetHttp().GetPath())
		methods[held.GetName()] = held.GetHttp().GetMethod()
	}
	assert.Equal(t, map[string]string{"standing": http.MethodGet, "step": http.MethodPost}, methods)
}

// A node with no login has no person to say where they stand, and says so
// the way its other ⍟ paths do.
func TestANodeWithNoLoginSaysSoWhenAskedWhereYouStand(t *testing.T) {
	srv := servedForTest(t)

	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/i/standing", nil))

	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "this node has no login")
}

// What server/auth answers keeps its meaning as a refusal: a namespace that
// is off is a step nobody may take.
func TestWhatAuthRefusesIsTheSigilsRefusal(t *testing.T) {
	said := errors.New("pond is disabled")
	for status, why := range map[int]string{
		http.StatusBadRequest:          sigil.Invalid,
		http.StatusNotFound:            sigil.NotFound,
		http.StatusConflict:            sigil.NotAllowed,
		http.StatusInternalServerError: sigil.Failed,
	} {
		refusal := refusedAs(status, said)
		assert.Equal(t, why, refusal.GetWhy(), "%d", status)
		assert.Equal(t, "pond is disabled", refusal.GetSays())
	}
}
