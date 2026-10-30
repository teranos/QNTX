package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/auth"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// "how would we know what things like chatGPT are trying to do and call?
// Things it might expect": an A2A request the node answers with an error is
// written down as the client sent it.
func TestARefusedA2ARequestIsWrittenDownAsTheClientSentIt(t *testing.T) {
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	core, written := observer.New(zapcore.InfoLevel)
	srv.logger = zap.New(core).Sugar()

	r := asBearer(http.MethodPost, "/a2a/", tokens[auth.LevelRoot])
	body := `{"jsonrpc": "2.0", "id": 1, "method": "SendMessage", "params": {}}`
	r.Body = io.NopCloser(strings.NewReader(body))
	r.ContentLength = int64(len(body))
	r.Header.Set("User-Agent", "a-client/1")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, r)
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())

	refused := written.FilterMessage("An A2A request was refused").All()
	require.Len(t, refused, 1)
	said := refused[0].ContextMap()
	assert.Equal(t, "POST", said["method"])
	assert.Equal(t, "/a2a/", said["path"])
	assert.Equal(t, "", said["a2a_version"], "no A2A-Version is 0.3 (§3.6.2), and is written as the client sent it")
	assert.Equal(t, "a-client/1", said["user_agent"])
	assert.Equal(t, "application/json", said["content_type"])
	assert.Equal(t, "NOT_FOUND", said["reason"])
	assert.True(t, strings.Contains(said["says"].(string), `JSON-RPC "SendMessage"`), said["says"])
}
