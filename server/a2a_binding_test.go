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

