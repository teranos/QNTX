package server

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teranos/QNTX/server/reach"
)

// An enabled plugin's paths are answered once they are registered, whether the
// plugin has loaded yet or not: a request that arrives while it loads is told
// so rather than finding nothing there.
func TestAnEnabledPluginsPathsAreAnswered(t *testing.T) {
	s := rootKnowingServer(t)
	s.answering = map[string]reach.Answering{}
	for _, path := range reach.Paths() {
		s.answer(path, func(http.ResponseWriter, *http.Request) {})
	}
	served, _, err := reach.Open(s.answering, s.wrapping(), s.runtime())
	require.NoError(t, err)
	s.served = served

	s.RegisterPluginRoutes("pyre")

	for _, path := range []string{"/api/pyre", "/api/pyre/{path...}", "/ws/pyre"} {
		_, answered := s.answering[path]
		assert.True(t, answered, path)
	}
	assert.True(t, s.pluginRoute("/api/pyre/book/new"))
}
