package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin"
	"go.uber.org/zap/zaptest"
)

// Every line grants reach to a path this node answers. A line naming a path
// nothing answers is a grant to nowhere, and the node does not start on one.
func TestEveryLineNamesSomethingTheNodeAnswers(t *testing.T) {
	servedForTest(t)
}

// A handler no line names is compiled and unreachable. This is the shape of
// "not defined is no access": there is nothing to call, not a check that says no.
func TestAHandlerNoLineNamesIsNotServed(t *testing.T) {
	srv := servedForTest(t)

	answers := false
	srv.answer("/api/invented", func(http.ResponseWriter, *http.Request) { answers = true })
	require.NoError(t, srv.open())

	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/invented", nil))

	assert.False(t, answers, "a handler no line grants reach to was reached")
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, srv.Unspoken(), "/api/invented")
}

// The node stops rather than serve a surface that disagrees with the page
// describing it.
func TestAGrantToNowhereStopsTheNode(t *testing.T) {
	srv := servedForTest(t)
	delete(srv.answering, "/api/config")

	err := srv.open()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "/api/config")
}

// servedForTest builds a node that answers and opens what the table grants.
func servedForTest(t *testing.T) *QNTXServer {
	t.Helper()

	logger := zaptest.NewLogger(t).Sugar()
	srv := &QNTXServer{
		pluginRegistry: plugin.NewRegistry("test-version", logger),
		logger:         logger,
		rlAuth:         newRateLimitGroup(100, 100),
		rlWS:           newRateLimitGroup(100, 100),
		rlWrite:        newRateLimitGroup(100, 100),
		rlRead:         newRateLimitGroup(100, 100),
		rlPublic:       newRateLimitGroup(100, 100),
	}
	srv.setupHTTPRoutes()
	require.NoError(t, srv.open())
	return srv
}
