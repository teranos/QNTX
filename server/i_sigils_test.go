package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/reach"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
)

// Reading where you stand and stepping are two sigils on one path. Before,
// GET answered 405, and where you stood was only learned by stepping.
func TestStandingIsReadAndSteppedOnOnePath(t *testing.T) {
	signum := bareNode().iSignum()
	require.NoError(t, signum.Check())

	methods := map[string]string{}
	for _, held := range signum.GetSigils() {
		methods[held.GetName()] = held.GetHttp().GetMethod() + " " + held.GetHttp().GetPath()
	}
	assert.Equal(t, map[string]string{
		"standing": http.MethodGet + " /i/standing",
		"step":     http.MethodPost + " /i/standing",
		"reaches":  http.MethodGet + " /i/reaches",
	}, methods)
}

// "an Element should know about its intended audience"

// A USER reaches i and the canvas, and not approvals: the gate's own answer.
func TestIReachesIsTheGatesAnswerForEachPathAsked(t *testing.T) {
	s := rootKnowingServer(t)
	s.answering = map[string]reach.Answering{}
	for _, path := range reach.Paths() {
		s.answer(path, func(http.ResponseWriter, *http.Request) {})
	}
	served, _, err := reach.Open(s.answering, s.wrapping(), s.runtime())
	require.NoError(t, err)
	s.served = served

	asked := sigil.Sent{"paths": "/i/,/api/canvas,/api/approvals,/.well-known/did.json"}
	for level, want := range map[auth.Level][]string{
		auth.LevelUser: {"/i/", "/api/canvas", "/.well-known/did.json"},
		auth.LevelRoot: {"/i/", "/api/canvas", "/api/approvals", "/.well-known/did.json"},
	} {
		ctx := auth.WithAdmission(context.Background(), auth.Admitted(level, "garden"))
		answered, refusal := s.iReaches(ctx, asked)
		require.Nil(t, refusal, "%s", level)
		assert.Equal(t, want, answered.(*protocol.Reaches).GetPaths(), "%s", level)
	}
}

// A node with no login serves every route to whoever asks, and says so.
func TestANodeWithNoLoginReachesEveryPathAsked(t *testing.T) {
	srv := servedForTest(t)

	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/i/reaches?paths=/api/approvals,/i/", nil))

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.JSONEq(t, `{"paths":["/api/approvals","/i/"]}`, w.Body.String())
}

// A path starts at /. Anything else asked, nothing included, is refused.
func TestIReachesRefusesWhatIsNoPath(t *testing.T) {
	s := rootKnowingServer(t)
	ctx := auth.WithAdmission(context.Background(), auth.Admitted(auth.LevelUser, "garden"))
	for _, asked := range []string{" , ", "/i/,", "api/approvals"} {
		_, refusal := s.iReaches(ctx, sigil.Sent{"paths": asked})
		require.NotNil(t, refusal, asked)
		assert.Equal(t, sigil.Invalid, refusal.GetWhy(), asked)
	}
}

// A node with no login has no person to say where they stand, and says so
// the way its other i paths do.
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
