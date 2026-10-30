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
	"github.com/teranos/QNTX/server/a2a"
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

	r := asBearer(http.MethodPost, "/a2a/", tokens[auth.LevelRoot])
	body := `{"jsonrpc": "2.0", "id": 1, "method": "SendMessage", "params": {"secret": "in-the-body"}}`
	r.Body = io.NopCloser(strings.NewReader(body))
	r.ContentLength = int64(len(body))
	r.Header.Set("User-Agent", "a-client/1")
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
		"a2a.reason": "NOT_FOUND", "a2a.kind": "expected", "a2a.binding": "jsonrpc", "a2a.jsonrpc": "SendMessage",
		"a2a.method": "POST", "a2a.path": "/a2a/", "user_agent.untrusted": "a-client/1", "request_id": id,
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
	srv.a2aRefused(r, a2a.Refusal{Operation: "SendMessage", Err: &a2a.Error{Name: "InvalidAgentResponseError", Message: "the store did not answer"}})

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
		srv.a2aRefused(r, a2a.Refusal{Err: &a2a.Error{Name: "TaskNotFoundError", Message: "no task"}})
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
