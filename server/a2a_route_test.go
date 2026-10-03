package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/teranos/QNTX/server/auth"
)

// The binding is served at /a2a/ behind its reach line: ROOT reaches every
// operation in scope, with a tenant and without, and is told it is not one the
// node does yet; SUPER is refused at the gate before any operation is asked.
func TestA2AIsServedToRootAlone(t *testing.T) {
	srv, tokens := pluginServingServer(t, "fake")
	// A SendMessageRequest that sets what the spec requires.
	sent := `{"message": {"messageId": "m1", "role": "ROLE_USER", "parts": [{"text": "hi"}]}}`
	asked := func(method, path, body, token string) *http.Request {
		r := asBearer(method, path, token)
		if body != "" {
			r.Body = io.NopCloser(strings.NewReader(body))
			r.ContentLength = int64(len(body))
		}
		return r
	}
	for _, route := range []struct{ method, path, body string }{
		{http.MethodPost, "/a2a/message:send", sent},
		{http.MethodPost, "/a2a/message:stream", sent},
		{http.MethodGet, "/a2a/tasks/abc", ""},
		{http.MethodGet, "/a2a/tasks", ""},
		{http.MethodPost, "/a2a/tasks/abc:cancel", ""},
		{http.MethodGet, "/a2a/extendedAgentCard", ""},
		{http.MethodPost, "/a2a/garden/tasks/abc:cancel", ""},
	} {
		r := asked(route.method, route.path, route.body, tokens[auth.LevelRoot])
		r.Header.Set("A2A-Version", "1.0")
		w := httptest.NewRecorder()
		srv.served.ServeHTTP(w, r)
		assert.Equal(t, http.StatusBadRequest, w.Code, route.path+" for ROOT: "+w.Body.String())
		assert.Contains(t, w.Body.String(), "UNSUPPORTED_OPERATION", route.path+" for ROOT")

		r = asked(route.method, route.path, route.body, tokens[auth.LevelSuper])
		r.Header.Set("A2A-Version", "1.0")
		w = httptest.NewRecorder()
		srv.served.ServeHTTP(w, r)
		assert.Equal(t, http.StatusForbidden, w.Code, route.path+" for SUPER: "+w.Body.String())
		assert.False(t, strings.Contains(w.Body.String(), "UNSUPPORTED_OPERATION"), route.path+" for SUPER reached an operation")
	}
}
