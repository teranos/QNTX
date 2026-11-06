package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/auth"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	"golang.org/x/time/rate"
)

// sentrySent is what the node handed Sentry while a test ran: its logs and its issues.
type sentrySent struct {
	sync.Mutex
	logs   []*sentry.Log
	issues []*sentry.Event
}

func (s *sentrySent) said() ([]*sentry.Log, []*sentry.Event) {
	s.Lock()
	defer s.Unlock()
	return append([]*sentry.Log(nil), s.logs...), append([]*sentry.Event(nil), s.issues...)
}

// toSentry binds a Sentry client that keeps what it is handed, and fresh limits.
func toSentry(t *testing.T) *sentrySent {
	t.Helper()
	sent := &sentrySent{}
	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn: "https://tim@example.com/0", Release: "test-release", EnableLogs: true,
		Transport: &sentry.MockTransport{},
		BeforeSendLog: func(l *sentry.Log) *sentry.Log {
			sent.Lock()
			defer sent.Unlock()
			sent.logs = append(sent.logs, l)
			return nil
		},
		BeforeSend: func(e *sentry.Event, _ *sentry.EventHint) *sentry.Event {
			sent.Lock()
			defer sent.Unlock()
			sent.issues = append(sent.issues, e)
			return nil
		},
	})
	require.NoError(t, err)
	hub := sentry.CurrentHub()
	before := hub.Client()
	hub.BindClient(client)
	limits := refusalsWritten
	refusalsWritten = map[refusalKind]*rate.Limiter{
		refusedExpected: rate.NewLimiter(rate.Every(time.Second), 20),
		refusedDefect:   rate.NewLimiter(rate.Every(6*time.Second), 5),
	}
	t.Cleanup(func() { hub.BindClient(before); refusalsWritten = limits })
	return sent
}

func logAttr(l *sentry.Log, key string) (string, bool) {
	v, held := l.Attributes[key]
	if !held {
		return "", false
	}
	return v.AsString(), true
}

// A client asking what the node does not answer is a Sentry log, and nothing in the node's own log.
func TestARefusedA2ARequestIsASentryLog(t *testing.T) {
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	core, written := observer.New(zapcore.DebugLevel)
	srv.logger = zap.New(core).Sugar()
	sent := toSentry(t)

	r := asBearer(http.MethodPost, "/a2a/tasks/nothing:cancel", tokens[auth.LevelRoot])
	body := `{"secret": "in-the-body"}`
	r.Body = io.NopCloser(strings.NewReader(body))
	r.ContentLength = int64(len(body))
	r.Header.Set("User-Agent", "a-client/1")
	r.Header.Set("A2A-Version", "1.0")
	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, r)
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	id := w.Header().Get("X-Request-Id")
	require.NotEmpty(t, id, "the client is answered no request id to report")

	logs, issues := sent.said()
	require.Len(t, logs, 1)
	assert.Empty(t, issues, "a client's mistake raised an issue")
	l := logs[0]
	assert.Equal(t, "An A2A request was refused", l.Body)
	for key, want := range map[string]string{
		"a2a.reason": "TASK_NOT_FOUND", "a2a.kind": "expected", "a2a.binding": "http+json", "a2a.operation": "CancelTask",
		"a2a.method": "POST", "a2a.path": "/a2a/tasks/nothing:cancel", "user_agent.untrusted": "a-client/1", "request_id": id,
		"sentry.release": "test-release",
	} {
		got, held := logAttr(l, key)
		assert.True(t, held, key)
		assert.Equal(t, want, got, key)
	}
	for key, value := range l.Attributes {
		assert.NotContains(t, value.AsString(), tokens[auth.LevelRoot], "the bearer token is in %s", key)
		assert.NotContains(t, value.AsString(), "in-the-body", "the body is in %s", key)
	}
	assert.Empty(t, written.FilterMessageSnippet("refused").All(), "an expected refusal was written in the node's own log")
	assert.Empty(t, written.FilterMessageSnippet("http POST /a2a/").All(), "the refusal was also an access log line")
}

// A request the gate turns away never reached the binding, and is the access log's to say.
func TestAGateRefusalIsStillAnAccessLogLine(t *testing.T) {
	srv, _, _ := nodeAgentOverA2A(t, "")
	core, written := observer.New(zapcore.DebugLevel)
	srv.logger = zap.New(core).Sugar()
	sent := toSentry(t)
	r := httptest.NewRequest(http.MethodGet, "/a2a/tasks", nil)
	r.Header.Set("A2A-Version", "1.0")
	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, r)
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	assert.Len(t, written.FilterMessage("http GET /a2a/tasks 401").All(), 1)
	logs, _ := sent.said()
	for _, l := range logs {
		assert.NotEqual(t, "An A2A request was refused", l.Body, "the gate's refusal was said as the binding's")
	}
}

// What the client says it is is cut short, and says nothing on more than one line.
func TestAUserAgentIsWrittenBounded(t *testing.T) {
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	sent := toSentry(t)
	r := asBearer(http.MethodGet, "/a2a/nothing", tokens[auth.LevelRoot])
	r.Header.Set("A2A-Version", "1.0")
	r.Header.Set("User-Agent", "evil\nforged: line "+strings.Repeat("x", 10000))
	srv.served.ServeHTTP(httptest.NewRecorder(), r)

	logs, _ := sent.said()
	require.Len(t, logs, 1)
	agent, _ := logAttr(logs[0], "user_agent.untrusted")
	assert.NotContains(t, agent, "\n")
	assert.LessOrEqual(t, len([]rune(agent)), saidUserAgentMost+1)
}

// The node failing what it should answer is a Sentry issue, grouped by operation and reason, and not a log.
func TestAnA2ADefectIsASentryIssue(t *testing.T) {
	srv, _, _ := nodeAgentOverA2A(t, "")
	core, written := observer.New(zapcore.DebugLevel)
	srv.logger = zap.New(core).Sugar()
	sent := toSentry(t)

	r := httptest.NewRequest(http.MethodPost, "/message:send", nil)
	srv.a2aRefused(r, a2aRefusalSaid{Operation: "SendMessage", Status: http.StatusInternalServerError, Reason: "INVALID_AGENT_RESPONSE", Message: "the store did not answer"})

	logs, issues := sent.said()
	require.Len(t, issues, 1)
	assert.Equal(t, []string{"a2a-defect", "SendMessage", "INVALID_AGENT_RESPONSE"}, issues[0].Fingerprint)
	assert.Equal(t, "defect", issues[0].Tags["a2a.kind"])
	for _, l := range logs {
		assert.NotEqual(t, "An A2A request was refused", l.Body, "a defect was written as an expected refusal")
	}
	assert.Len(t, written.FilterMessage("An A2A request failed on the node's side").All(), 1)
}

// A client sending many refusals at once is counted, and only so many are written.
func TestRefusalsWrittenAreBounded(t *testing.T) {
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	sent := toSentry(t)
	r := asBearer(http.MethodGet, "/a2a/nothing", tokens[auth.LevelRoot])
	for range 200 {
		srv.a2aRefused(r, a2aRefusalSaid{Status: http.StatusNotFound, Reason: "TASK_NOT_FOUND", Message: "no task"})
	}
	logs, _ := sent.said()
	assert.GreaterOrEqual(t, len(logs), 20)
	assert.LessOrEqual(t, len(logs), 25, "every refusal of a flood was written")
}

// A request the node answers is written nowhere, and is still answered its id.
func TestAnAnsweredA2ARequestIsNotWritten(t *testing.T) {
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	sent := toSentry(t)
	r := asBearer(http.MethodGet, "/a2a/tasks/none", tokens[auth.LevelRoot])
	r.Header.Set("A2A-Version", "1.0")
	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, r)
	require.Equal(t, http.StatusNotFound, w.Code)
	logs, _ := sent.said()
	require.Len(t, logs, 1, "the control's own refusal")
	operation, _ := logAttr(logs[0], "a2a.operation")
	assert.Equal(t, "GetTask", operation)

	r = asBearer(http.MethodGet, "/a2a/tasks", tokens[auth.LevelRoot])
	r.Header.Set("A2A-Version", "1.0")
	w = httptest.NewRecorder()
	srv.served.ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.NotEmpty(t, w.Header().Get("X-Request-Id"))
	logs, issues := sent.said()
	assert.Len(t, logs, 1, "an answered request was written")
	assert.Empty(t, issues)
}

func TestVersionsAndMethodsAreTheNodesWords(t *testing.T) {
	for said, want := range map[string]string{"": "0.3", "1.0": "1.0", "1.0.1": "1.0", "0.3": "0.3", "9.9": "other", strings.Repeat("z", 99): "other"} {
		assert.Equal(t, want, versionOf(said), said)
	}
	assert.Equal(t, "OTHER", methodOf("BREW"))
	assert.Equal(t, "GET", methodOf("GET"))
	assert.Equal(t, "ab…", bounded("abc", 2))
	assert.Equal(t, "a?b", bounded("a\x00b", 10))
}

// JSON-RPC answers a refusal with HTTP 200 and the error in the body; it is a Sentry log all the same.
func TestARefusedJSONRPCRequestIsASentryLog(t *testing.T) {
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	sent := toSentry(t)

	r := asBearer(http.MethodPost, "/a2a/", tokens[auth.LevelRoot])
	body := `{"jsonrpc":"2.0","id":1,"method":"CancelTask","params":{"id":"nothing"}}`
	r.Body = io.NopCloser(strings.NewReader(body))
	r.ContentLength = int64(len(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("A2A-Version", "1.0")
	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"error"`)

	logs, _ := sent.said()
	require.Len(t, logs, 1)
	for key, want := range map[string]string{"a2a.reason": "TASK_NOT_FOUND", "a2a.binding": "jsonrpc", "a2a.operation": "CancelTask"} {
		got, held := logAttr(logs[0], key)
		assert.True(t, held, key)
		assert.Equal(t, want, got, key)
	}
}

// A message sent to an agent is the user's: one in the agent's role is refused.
func TestAMessageInTheAgentsRoleIsRefused(t *testing.T) {
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	r := asBearer(http.MethodPost, "/a2a/message:send", tokens[auth.LevelRoot])
	body := `{"message":{"messageId":"m1","role":"ROLE_AGENT","parts":[{"text":"hello"}]}}`
	r.Body = io.NopCloser(strings.NewReader(body))
	r.ContentLength = int64(len(body))
	r.Header.Set("Content-Type", "application/a2a+json")
	r.Header.Set("A2A-Version", "1.0")
	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "role")
}

// An agent at a tenant takes less than the Node Agent: a body over tenantBodyMost is refused there.
func TestATenantsAgentTakesLessThanTheNodeAgent(t *testing.T) {
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	text := strings.Repeat("a", tenantBodyMost)
	body := `{"message":{"messageId":"m1","role":"ROLE_USER","parts":[{"text":"` + text + `"}]}}`
	r := asBearer(http.MethodPost, "/a2a/orchard/message:send", tokens[auth.LevelRoot])
	r.Body = io.NopCloser(strings.NewReader(body))
	r.ContentLength = int64(len(body))
	r.Header.Set("Content-Type", "application/a2a+json")
	r.Header.Set("A2A-Version", "1.0")
	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "an agent at a tenant takes")
}

// A JSON-RPC stream that is refused answers its error as an SSE event; it is a Sentry log all the same.
func TestARefusedJSONRPCStreamIsASentryLog(t *testing.T) {
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	sent := toSentry(t)

	r := asBearer(http.MethodPost, "/a2a/", tokens[auth.LevelRoot])
	body := `{"jsonrpc":"2.0","id":1,"method":"SendStreamingMessage","params":{"message":{"messageId":"m1","role":"ROLE_AGENT","parts":[{"text":"hello"}]}}}`
	r.Body = io.NopCloser(strings.NewReader(body))
	r.ContentLength = int64(len(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("A2A-Version", "1.0")
	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, r)
	require.Contains(t, w.Body.String(), `"error"`, w.Body.String())

	logs, _ := sent.said()
	require.Len(t, logs, 1, w.Header().Get("Content-Type")+" "+w.Body.String())
	for key, want := range map[string]string{"a2a.binding": "jsonrpc", "a2a.operation": "SendStreamingMessage"} {
		got, held := logAttr(logs[0], key)
		assert.True(t, held, key)
		assert.Equal(t, want, got, key)
	}
}

// A stream's error is heard however much the stream said before it, and when
// it arrives split across writes.
func TestALateStreamedErrorIsStillARefusal(t *testing.T) {
	answered := &a2aAnswered{ResponseWriter: httptest.NewRecorder(), keepsAll: true}
	answered.Header().Set("Content-Type", "text/event-stream")
	event := `data: {"jsonrpc":"2.0","id":1,"result":{"statusUpdate":{"taskId":"t","status":{"state":"TASK_STATE_WORKING"}}}}` + "\n\n"
	require.Greater(t, len(event)*200, a2aSaidMost, "the stream before the error is not longer than what an answer keeps")
	for range 200 {
		_, _ = answered.Write([]byte(event))
	}
	refusal := `data: {"jsonrpc":"2.0","id":1,"error":{"code":-32001,"message":"no such task"}}` + "\n\n"
	_, _ = answered.Write([]byte(refusal[:30]))
	_, _ = answered.Write([]byte(refusal[30:]))

	said, refused := jsonRPCRefusalOf(&a2aExchange{operation: "SendStreamingMessage", jsonRPC: true}, answered)
	require.True(t, refused, "an error after %d bytes of stream was not heard", len(event)*200)
	assert.Equal(t, "no such task", said.Message)
	assert.Equal(t, "-32001", said.Reason)
}
