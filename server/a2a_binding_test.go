package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/auth"
)

// What the node asks of every A2A request before a2a-go reads it.

// a2aSaid is a request as the bearer of token, with the A2A-Version header
// version when it is not empty, and the error it was answered with.
func a2aSaid(t *testing.T, srv *QNTXServer, method, path, body, version, token string) (*httptest.ResponseRecorder, a2aErrorSaid) {
	t.Helper()
	r := asBearer(method, path, token)
	if body != "" {
		r.Body = io.NopCloser(strings.NewReader(body))
		r.ContentLength = int64(len(body))
	}
	if version != "" {
		r.Header.Set("A2A-Version", version)
	}
	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, r)
	var said a2aErrorSaid
	_ = json.Unmarshal(w.Body.Bytes(), &said)
	return w, said
}

type a2aErrorSaid struct {
	Error struct {
		Status  string `json:"status"`
		Message string `json:"message"`
		Details []struct {
			Type   string `json:"@type"`
			Reason string `json:"reason"`
			Domain string `json:"domain"`
		} `json:"details"`
	} `json:"error"`
}

func (s a2aErrorSaid) reason() string {
	for _, detail := range s.Error.Details {
		if detail.Type == "type.googleapis.com/google.rpc.ErrorInfo" {
			return detail.Reason
		}
	}
	return ""
}

// §3.6.2: an empty A2A-Version is 0.3, which the node does not speak.
func TestNoVersionIsZeroThreeAndRefused(t *testing.T) {
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	w, said := a2aSaid(t, srv, http.MethodGet, "/a2a/tasks/abc", "", "", tokens[auth.LevelRoot])
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Equal(t, "FAILED_PRECONDITION", said.Error.Status)
	assert.Equal(t, "VERSION_NOT_SUPPORTED", said.reason())
	assert.Equal(t, "a2a-protocol.org", said.Error.Details[0].Domain)
	assert.Contains(t, said.Error.Message, "0.3")
}

// §3.6: Major.Minor alone, from the header or the request parameter.
func TestVersionIsMajorMinor(t *testing.T) {
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	for _, asked := range []struct{ path, version string }{
		{"/a2a/tasks/abc", "1.0"},
		{"/a2a/tasks/abc", "1.0.1"},
		{"/a2a/tasks/abc?A2A-Version=1.0", ""},
	} {
		w, said := a2aSaid(t, srv, http.MethodGet, asked.path, "", asked.version, tokens[auth.LevelRoot])
		assert.Equal(t, http.StatusNotFound, w.Code, asked.path+" "+asked.version+": "+w.Body.String())
		assert.Equal(t, "TASK_NOT_FOUND", said.reason(), asked.path+" "+asked.version)
	}
}

// A route no operation is on is not found, and says so (§3.3.2).
func TestWhatIsNoOperationIsNotFound(t *testing.T) {
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	for _, asked := range []struct{ method, path string }{
		{http.MethodGet, "/a2a/message:send"},
		{http.MethodPost, "/a2a/tasks"},
		{http.MethodGet, "/a2a/nothing/here/at/all"},
		{http.MethodPut, "/a2a/tasks/abc"},
	} {
		w, said := a2aSaid(t, srv, asked.method, asked.path, "", "1.0", tokens[auth.LevelRoot])
		assert.Equal(t, http.StatusNotFound, w.Code, asked.method+" "+asked.path)
		assert.Contains(t, said.Error.Message, "is no A2A operation", asked.method+" "+asked.path)
	}
}

// A body that is not the request message is a validation error (§3.3.2), and
// so is one that does not set what the spec requires, naming each field
// (§5.7).
func TestARequestThatIsNotOrLacksTheRequestIsInvalid(t *testing.T) {
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	for body, names := range map[string][]string{
		`{"message": 7}`:  nil,
		`{}`:              {"message"},
		`{"message": {}}`: {"message.message_id", "message.role", "message.parts"},
	} {
		w, said := a2aSaid(t, srv, http.MethodPost, "/a2a/message:send", body, "1.0", tokens[auth.LevelRoot])
		assert.Equal(t, http.StatusBadRequest, w.Code, body+": "+w.Body.String())
		assert.Equal(t, "INVALID_ARGUMENT", said.Error.Status, body)
		for _, name := range names {
			assert.Contains(t, said.Error.Message, name, body)
		}
	}
}

// "Implement limits on message size, file size, and request complexity"
// (§13.4): the Node Agent takes a body of 2 MiB, an agent at a tenant 1 MiB,
// wherever the tenant was named.
func TestABodyIsHeldToTheMostItsAgentTakes(t *testing.T) {
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	root := tokens[auth.LevelRoot]
	over := func(most int, tenant string) string {
		named := ""
		if tenant != "" {
			named = `"tenant": ` + quoted(tenant) + `, `
		}
		return `{` + named + `"message": {"messageId": "m1", "role": "ROLE_USER", "parts": [{"text": "` + strings.Repeat("a", most) + `"}]}}`
	}

	w, said := a2aSaid(t, srv, http.MethodPost, "/a2a/message:send", over(nodeBodyMost, ""), "1.0", root)
	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code, said.Error.Message)
	assert.Contains(t, said.Error.Message, "2097152")

	w, said = a2aSaid(t, srv, http.MethodPost, "/a2a/message:send", over(tenantBodyMost, "garden"), "1.0", root)
	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code, said.Error.Message)
	assert.Contains(t, said.Error.Message, "1048576")

	w, _ = a2aSaid(t, srv, http.MethodPost, "/a2a/message:send", over(tenantBodyMost+1024, ""), "1.0", root)
	assert.NotEqual(t, http.StatusRequestEntityTooLarge, w.Code, "the Node Agent was held to a tenant's most")
}

// What the node answers is A2A's error shape, whoever wrote it: google.rpc.Status
// with an ErrorInfo for an A2A error (§11.6).
func TestARefusalTheNodeWritesIsTheSDKsShape(t *testing.T) {
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	w, said := a2aSaid(t, srv, http.MethodPost, "/a2a/message:stream", saidToTheNode("hi", ""), "1.0", tokens[auth.LevelRoot])
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Equal(t, "FAILED_PRECONDITION", said.Error.Status)
	assert.Equal(t, "UNSUPPORTED_OPERATION", said.reason())
	assert.Equal(t, "application/a2a+json", w.Header().Get("Content-Type"))
}

// A path that is an operation without a tenant names none; any other's first
// segment is a tenant, a namespace named after an operation among them.
func TestATenantNamedAfterAnOperationIsATenant(t *testing.T) {
	for _, asked := range []struct {
		method, path, tenant, rest string
	}{
		{http.MethodPost, "/message:send", "", "/message:send"},
		{http.MethodGet, "/tasks/abc", "", "/tasks/abc"},
		{http.MethodGet, "/extendedAgentCard", "", "/extendedAgentCard"},
		{http.MethodPost, "/garden/message:send", "garden", "/message:send"},
		{http.MethodPost, "/tasks/message:send", "tasks", "/message:send"},
		{http.MethodGet, "/extendedAgentCard/extendedAgentCard", "extendedAgentCard", "/extendedAgentCard"},
		{http.MethodPost, "/message:send/tasks/abc:cancel", "message:send", "/tasks/abc:cancel"},
	} {
		tenant, rest, named := tenantOf(asked.method, asked.path)
		assert.Equal(t, asked.tenant, tenant, asked.method+" "+asked.path)
		assert.Equal(t, asked.rest, rest, asked.method+" "+asked.path)
		assert.Equal(t, asked.tenant != "", named, asked.method+" "+asked.path)
	}
}
