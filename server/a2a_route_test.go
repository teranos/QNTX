package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/teranos/QNTX/server/auth"
)

// The binding is served at /a2a/ behind its reach line: ROOT reaches every
// operation in scope and is told what the node does not do, a message to a
// node that names no Node Agent and its tasks among them, and a tenant no
// agent is served at is not found (§3.3.2). SUPER is refused at the gate
// before any operation is asked.
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
	unsupported, noOperation := "UNSUPPORTED_OPERATION", "is no A2A operation"
	for _, route := range []struct {
		method, path, body string
		code               int
		says               string
	}{
		{http.MethodPost, "/a2a/message:send", sent, http.StatusBadRequest, unsupported},
		{http.MethodPost, "/a2a/message:stream", sent, http.StatusBadRequest, unsupported},
		{http.MethodGet, "/a2a/tasks/abc", "", http.StatusBadRequest, unsupported},
		{http.MethodGet, "/a2a/tasks", "", http.StatusBadRequest, unsupported},
		{http.MethodPost, "/a2a/tasks/abc:cancel", "", http.StatusBadRequest, unsupported},
		{http.MethodPost, "/a2a/garden/tasks/abc:cancel", "", http.StatusNotFound, noOperation},
		{http.MethodPost, "/a2a/tasks/abc/pushNotificationConfigs", `{"url": "https://example.org/hook"}`, http.StatusBadRequest, unsupported},
		{http.MethodGet, "/a2a/garden/extendedAgentCard", "", http.StatusNotFound, noOperation},
	} {
		r := asked(route.method, route.path, route.body, tokens[auth.LevelRoot])
		r.Header.Set("A2A-Version", "1.0")
		w := httptest.NewRecorder()
		srv.served.ServeHTTP(w, r)
		assert.Equal(t, route.code, w.Code, route.path+" for ROOT: "+w.Body.String())
		assert.Contains(t, w.Body.String(), route.says, route.path+" for ROOT")

		r = asked(route.method, route.path, route.body, tokens[auth.LevelSuper])
		r.Header.Set("A2A-Version", "1.0")
		w = httptest.NewRecorder()
		srv.served.ServeHTTP(w, r)
		assert.Equal(t, http.StatusForbidden, w.Code, route.path+" for SUPER: "+w.Body.String())
		assert.False(t, strings.Contains(w.Body.String(), route.says), route.path+" for SUPER reached an operation")
		// §11.6: the gate's refusal is a google.rpc.Status too.
		var said struct {
			Error struct {
				Code   int    `json:"code"`
				Status string `json:"status"`
			} `json:"error"`
		}
		assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &said), route.path+" for SUPER: "+w.Body.String())
		assert.Equal(t, http.StatusForbidden, said.Error.Code, route.path+" for SUPER")
		assert.Equal(t, "PERMISSION_DENIED", said.Error.Status, route.path+" for SUPER")
	}
}
