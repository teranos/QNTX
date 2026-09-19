package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/reach"
	"github.com/teranos/QNTX/server/sigil"
)

// "and we can expose this as a functionality as well so i can do this from my
// phone if i like (via the MCP that is now actually functional)"
func TestAGrantIsALineAndTheListSaysIt(t *testing.T) {
	s := rootKnowingServer(t)
	s.pluginRoutes.Store("hello-world", true)
	root := auth.Admitted(auth.LevelRoot, "garden")
	root.Identity = rootAccount
	asked := auth.WithAdmission(context.Background(), root)

	require.NoError(t, s.reachSignum().Check())

	granted, refusal := s.reachGrant(asked, sigil.Sent{"path": "/api/hello-world/{path...}", "to": "public_registration"})
	require.Nil(t, refusal, refusal.GetSays())
	require.NotEmpty(t, granted.(map[string]string)["id"])

	_, refusal = s.reachRevoke(asked, sigil.Sent{"path": "/api/hello-world/{path...}", "to": "PUBLIC_REGISTRATION"})
	require.Nil(t, refusal, refusal.GetSays())

	listed, refusal := s.reachList(asked, sigil.Sent{})
	require.Nil(t, refusal, refusal.GetSays())
	lines := listed.(map[string]any)["lines"].([]reachLine)
	require.Len(t, lines, 2)
	assert.True(t, lines[0].Revokes, "the newest line is the revoke")
	assert.Equal(t, []string{"/api/hello-world/{path...}"}, lines[1].Paths)
	assert.Equal(t, []string{"PUBLIC_REGISTRATION"}, lines[1].To)
	assert.Equal(t, rootAccount, lines[1].By)

	compiled := listed.(map[string]any)["compiled"].(map[string][]string)
	assert.Equal(t, []string{"ROOT"}, compiled["/api/reach"])

	assert.Len(t, systemHolds(t, s), 2, "a line was answered and not written")
}

// "book/new is actually more open than PUBLIC_REGISTRATION, its pretty much
// PUBLIC"
//
// One grant opens one path of a plugin to strangers, from the store. The rest
// of the plugin is ROOT's as before, and the compiled table names nothing.
func TestAGrantOpensOnePluginPathToAStrangerAndNothingElseOfIt(t *testing.T) {
	s := pluginNode(t, newRateLimitGroup(100, 100))
	openTo(t, s, "/api/hello-world/book/new", "ANYONE")

	opened := httptest.NewRecorder()
	s.served.ServeHTTP(opened, httptest.NewRequest(http.MethodPost, "/api/hello-world/book/new", strings.NewReader(`{}`)))
	assert.NotEqual(t, http.StatusUnauthorized, opened.Code, opened.Body.String())
	assert.NotEqual(t, http.StatusForbidden, opened.Code, opened.Body.String())

	closed := httptest.NewRecorder()
	s.served.ServeHTTP(closed, httptest.NewRequest(http.MethodPost, "/api/hello-world/book/cancel", strings.NewReader(`{}`)))
	assert.NotEqual(t, http.StatusTeapot, closed.Code, "the rest of the plugin was opened too")

	big := httptest.NewRecorder()
	s.served.ServeHTTP(big, httptest.NewRequest(http.MethodPost, "/api/hello-world/book/new",
		strings.NewReader(strings.Repeat("x", pluginPathBody+1))))
	assert.Equal(t, http.StatusRequestEntityTooLarge, big.Code, big.Body.String())
}

// "yes, per caller"
//
// A path a line opened to a stranger answers each caller once a second, per
// path. Past it that caller is refused and counted; nobody else is touched.
func TestAStrangerOnAnOpenedPathIsHeldToTheFloor(t *testing.T) {
	s := pluginNode(t, newRateLimitGroup(1, 1))
	openTo(t, s, "/api/hello-world/book/new", "ANYONE")
	openTo(t, s, "/api/hello-world/book/save", "ANYONE")
	openTo(t, s, "/api/hello-world/book/cancel", "WORKER")

	ask := func(path, from string) int {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		req.RemoteAddr = from + ":1234"
		w := httptest.NewRecorder()
		s.served.ServeHTTP(w, req)
		return w.Code
	}

	assert.NotEqual(t, http.StatusTooManyRequests, ask("/api/hello-world/book/new", "192.0.2.1"))
	assert.Equal(t, http.StatusTooManyRequests, ask("/api/hello-world/book/new", "192.0.2.1"),
		"a second start from one caller in the same second was answered")
	assert.NotEqual(t, http.StatusTooManyRequests, ask("/api/hello-world/book/save", "192.0.2.1"),
		"a caller's start spent their save")
	assert.NotEqual(t, http.StatusTooManyRequests, ask("/api/hello-world/book/new", "192.0.2.2"),
		"one caller's flood spent another caller's second")

	assert.True(t, s.openToStrangers("/api/hello-world/book/new"))
	assert.False(t, s.openToStrangers("/api/hello-world/book/cancel"),
		"a path opened to a role is held to the strangers' floor")
}

// pluginNode is a node serving the reach table and one loaded plugin, with
// auth on, whose paths opened at runtime are held to opened.
func pluginNode(t *testing.T, opened *rateLimitGroup) *QNTXServer {
	t.Helper()
	s := rootKnowingServer(t)
	s.authEnabled = true
	s.rlAuth, s.rlWS, s.rlWrite, s.rlRead, s.rlPublic = newRateLimitGroup(100, 100),
		newRateLimitGroup(100, 100), newRateLimitGroup(100, 100), newRateLimitGroup(100, 100), newRateLimitGroup(100, 100)
	s.rlOpened = opened
	answered := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) }
	s.answering = map[string]reach.Answering{}
	for _, path := range reach.Paths() {
		s.answer(path, answered)
	}
	s.pluginRoutes.Store("hello-world", true)
	s.answer("/api/hello-world/{path...}", answered)
	require.NoError(t, s.open())
	return s
}

// openTo writes, as ROOT, the line opening path to whom.
func openTo(t *testing.T, s *QNTXServer, path, whom string) {
	t.Helper()
	root := auth.Admitted(auth.LevelRoot, "garden")
	root.Identity = rootAccount
	_, refusal := s.reachGrant(auth.WithAdmission(context.Background(), root),
		sigil.Sent{"path": path, "to": whom})
	require.Nil(t, refusal, refusal.GetSays())
}

// "and runtime can never supersede the coompiled in reach table"
func TestAGrantOverTheCompiledTableIsRefused(t *testing.T) {
	s := rootKnowingServer(t)
	root := auth.Admitted(auth.LevelRoot, "garden")
	root.Identity = rootAccount

	_, refusal := s.reachGrant(auth.WithAdmission(context.Background(), root),
		sigil.Sent{"path": "/api/staands", "to": "PUBLIC_REGISTRATION"})
	require.NotNil(t, refusal)
	assert.Equal(t, sigil.NotAllowed, refusal.GetWhy())
	assert.Equal(t, "path", refusal.GetParam())
	assert.Empty(t, systemHolds(t, s), "a refused line was stored")
}

// A level other than PUBLIC_REGISTRATION is the compiled table's alone.
func TestAGrantToAnotherLevelIsRefused(t *testing.T) {
	s := rootKnowingServer(t)
	root := auth.Admitted(auth.LevelRoot, "garden")
	root.Identity = rootAccount

	_, refusal := s.reachGrant(auth.WithAdmission(context.Background(), root),
		sigil.Sent{"path": "/api/staands", "to": "SUPER"})
	require.NotNil(t, refusal)
	assert.Equal(t, sigil.Invalid, refusal.GetWhy())
	assert.Equal(t, "to", refusal.GetParam())
}

// Writing a line is ROOT's, whoever else the gate lets through.
func TestOnlyRootGrants(t *testing.T) {
	s := rootKnowingServer(t)

	_, refusal := s.reachGrant(auth.WithAdmission(context.Background(), auth.Admitted(auth.LevelSuper, "garden")),
		sigil.Sent{"path": "/api/staands", "to": "WORKER"})
	require.NotNil(t, refusal)
	assert.Equal(t, sigil.NotAllowed, refusal.GetWhy())
	assert.Contains(t, refusal.GetSays(), reach.Subject)
}
