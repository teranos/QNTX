package server

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/server/auth"
)

// "NODE is ROOT": a message sent to the node at no tenant is said to the Node
// Agent, and the turn it is is a Task (ADR-048).

// nodeAgentOverA2A is a node serving A2A whose Node Agent runs a stand-in for
// Claude Code, which answers once held is gone, when held names a file.
func nodeAgentOverA2A(t *testing.T, held string) (*QNTXServer, map[auth.Level]string, string) {
	t.Helper()
	srv, tokens := pluginServingServer(t, "fake")
	binary, ran := claudeStandIn(t, claudeAnswered)
	if held != "" {
		waits := filepath.Join(filepath.Dir(binary), "claude-held")
		require.NoError(t, os.WriteFile(waits, []byte("#!/bin/sh\nwhile [ -f '"+held+"' ]; do sleep 0.02; done\nexec '"+binary+"' \"$@\"\n"), 0o755))
		binary = waits
	}
	fetched := make(chan struct{})
	close(fetched)
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = 7
	agent, err := theNodeAgent(ed25519.NewKeyFromSeed(seed), t.TempDir())
	require.NoError(t, err)
	underGround(t, agent.home)
	t.Setenv("QNTX_TEST_PLAN_TOKEN", "the-plan-token")
	named := opusLow
	named.TokenRef = "env:QNTX_TEST_PLAN_TOKEN"

	store, db := createTestStore(t)
	srv.held = servingOne(db, store)
	system, _ := createTestStore(t)
	srv.held.SetSystem(oneNamespace("system", system))
	srv.deps = &serverDependencies{cfg: &appcfg.Config{Agent: appcfg.AgentConfig{Root: named}}}
	srv.nodeAgent, srv.ctx, srv.ownURL = agent, context.Background(), "http://127.0.0.1:8770"
	srv.holdHarness("claude", &harnessHeld{fetched: fetched, path: binary})
	return srv, tokens, ran
}

// a2aAsked is an A2A request as the bearer of token, its answer and its body.
func a2aAsked(t *testing.T, srv *QNTXServer, method, path, body, token string) (int, map[string]any) {
	t.Helper()
	r := asBearer(method, path, token)
	if body != "" {
		r.Body = io.NopCloser(strings.NewReader(body))
		r.ContentLength = int64(len(body))
	}
	r.Header.Set("A2A-Version", "1.0")
	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, r)
	var answered map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &answered), w.Body.String())
	return w.Code, answered
}

// saidToTheNode is a SendMessageRequest saying says, configured as configured.
func saidToTheNode(says, configured string) string {
	sent := `{"message": {"messageId": "m1", "role": "ROLE_USER", "parts": [{"text": ` + quoted(says) + `}]}`
	if configured != "" {
		sent += `, "configuration": ` + configured
	}
	return sent + `}`
}

func quoted(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// at is what lies at a dotted path in v: task.status.state, history.0.taskId.
func at(v any, dotted string) any {
	for _, step := range strings.Split(dotted, ".") {
		switch held := v.(type) {
		case map[string]any:
			v = held[step]
		case []any:
			i := 0
			for _, c := range step {
				i = i*10 + int(c-'0')
			}
			if i >= len(held) {
				return nil
			}
			v = held[i]
		default:
			return nil
		}
	}
	return v
}

// SendMessage waits for its Task to end by default (§3.2.2), and answers the
// Task: one turn, completed, the answer its artifact, the message sent its
// history, in the Node Agent's one session.
func TestAMessageToTheNodeIsATaskOfTheNodeAgent(t *testing.T) {
	srv, tokens, ran := nodeAgentOverA2A(t, "")

	code, sent := a2aAsked(t, srv, http.MethodPost, "/a2a/message:send", saidToTheNode("how long has the box been up?", ""), tokens[auth.LevelRoot])
	require.Equal(t, http.StatusOK, code, sent)
	task := at(sent, "task")
	assert.Equal(t, "TASK_STATE_COMPLETED", at(task, "status.state"))
	assert.Equal(t, "Up 3 days.", at(task, "artifacts.0.parts.0.text"))
	assert.Equal(t, "text/plain", at(task, "artifacts.0.parts.0.mediaType"))
	assert.Equal(t, "how long has the box been up?", at(task, "history.0.parts.0.text"))
	assert.Equal(t, "ROLE_USER", at(task, "history.0.role"))
	id := at(task, "id").(string)
	assert.Equal(t, id, at(task, "history.0.taskId"))

	args := ranWith(t, ran, "0", "args")
	assert.Equal(t, "how long has the box been up?", after(args, "-p"))
	assert.Equal(t, after(args, "--session-id"), at(task, "contextId"), "the context is the Node Agent's one session")

	// And it is the Task GetTask answers, by its id (§3.1.3).
	code, got := a2aAsked(t, srv, http.MethodGet, "/a2a/tasks/"+id, "", tokens[auth.LevelRoot])
	require.Equal(t, http.StatusOK, code, got)
	assert.Equal(t, task, any(got))

	// "A value of zero is a request to not include any messages."
	code, got = a2aAsked(t, srv, http.MethodGet, "/a2a/tasks/"+id+"?historyLength=0", "", tokens[auth.LevelRoot])
	require.Equal(t, http.StatusOK, code, got)
	assert.Nil(t, at(got, "history"))
}

// A second message is a new Task in the same context: the session continues,
// and the Task it is in is its own (§3.4.3).
func TestTheNextMessageIsANewTaskInTheSameContext(t *testing.T) {
	srv, tokens, ran := nodeAgentOverA2A(t, "")

	_, first := a2aAsked(t, srv, http.MethodPost, "/a2a/message:send", saidToTheNode("remember 7", ""), tokens[auth.LevelRoot])
	context := at(first, "task.contextId").(string)
	code, second := a2aAsked(t, srv, http.MethodPost, "/a2a/message:send",
		`{"message": {"messageId": "m2", "role": "ROLE_USER", "contextId": "`+context+`", "parts": [{"text": "what did I ask you to remember?"}]}}`, tokens[auth.LevelRoot])
	require.Equal(t, http.StatusOK, code, second)
	assert.Equal(t, context, at(second, "task.contextId"))
	assert.Equal(t, context, at(second, "task.history.0.contextId"))
	assert.NotEqual(t, at(first, "task.id"), at(second, "task.id"))
	assert.Equal(t, context, after(ranWith(t, ran, "1", "args"), "--resume"))
}

// With return_immediately the Task is answered as it stands while its turn
// runs, and GetTask answers it when the turn has ended (§3.2.2).
func TestATaskReturnedImmediatelyIsAnsweredByItsIdWhenItEnds(t *testing.T) {
	held := filepath.Join(t.TempDir(), "held")
	require.NoError(t, os.WriteFile(held, nil, 0o600))
	srv, tokens, _ := nodeAgentOverA2A(t, held)

	code, sent := a2aAsked(t, srv, http.MethodPost, "/a2a/message:send", saidToTheNode("how long has the box been up?", `{"returnImmediately": true}`), tokens[auth.LevelRoot])
	require.Equal(t, http.StatusOK, code, sent)
	assert.Equal(t, "TASK_STATE_SUBMITTED", at(sent, "task.status.state"))
	assert.Nil(t, at(sent, "task.artifacts"))
	id := at(sent, "task.id").(string)

	require.NoError(t, os.Remove(held))
	require.Eventually(t, func() bool {
		_, got := a2aAsked(t, srv, http.MethodGet, "/a2a/tasks/"+id, "", tokens[auth.LevelRoot])
		return at(got, "status.state") == "TASK_STATE_COMPLETED"
	}, 10*time.Second, 20*time.Millisecond)
	_, got := a2aAsked(t, srv, http.MethodGet, "/a2a/tasks/"+id, "", tokens[auth.LevelRoot])
	assert.Equal(t, "Up 3 days.", at(got, "artifacts.0.parts.0.text"))
}

// What the Node Agent does not take is refused before anything is said to it.
func TestWhatTheNodeAgentDoesNotTakeIsRefused(t *testing.T) {
	srv, tokens, ran := nodeAgentOverA2A(t, "")
	_, done := a2aAsked(t, srv, http.MethodPost, "/a2a/message:send", saidToTheNode("remember 7", ""), tokens[auth.LevelRoot])
	finished := at(done, "task.id").(string)

	for _, refused := range []struct {
		what, body string
		code       int
		says       string
	}{
		{"a part that is not text",
			`{"message": {"messageId": "m", "role": "ROLE_USER", "parts": [{"url": "https://example.org/a.png", "mediaType": "image/png"}]}}`,
			http.StatusBadRequest, "CONTENT_TYPE_NOT_SUPPORTED"},
		{"text in another media type",
			`{"message": {"messageId": "m", "role": "ROLE_USER", "parts": [{"text": "{}", "mediaType": "application/json"}]}}`,
			http.StatusBadRequest, "CONTENT_TYPE_NOT_SUPPORTED"},
		{"a message that is the agent's",
			`{"message": {"messageId": "m", "role": "ROLE_AGENT", "parts": [{"text": "hi"}]}}`,
			http.StatusBadRequest, "is the user's"},
		{"a context that is not its session",
			`{"message": {"messageId": "m", "role": "ROLE_USER", "contextId": "another", "parts": [{"text": "hi"}]}}`,
			http.StatusBadRequest, "is one session"},
		{"a task that does not exist",
			`{"message": {"messageId": "m", "role": "ROLE_USER", "taskId": "nothing", "parts": [{"text": "hi"}]}}`,
			http.StatusNotFound, "TASK_NOT_FOUND"},
		{"a task that ended",
			`{"message": {"messageId": "m", "role": "ROLE_USER", "taskId": "` + finished + `", "parts": [{"text": "hi"}]}}`,
			http.StatusBadRequest, "UNSUPPORTED_OPERATION"},
		{"a push notification",
			saidToTheNode("hi", `{"taskPushNotificationConfig": {"url": "https://example.org/hook"}}`),
			http.StatusBadRequest, "PUSH_NOTIFICATION_NOT_SUPPORTED"},
	} {
		code, answered := a2aAsked(t, srv, http.MethodPost, "/a2a/message:send", refused.body, tokens[auth.LevelRoot])
		assert.Equal(t, refused.code, code, refused.what)
		said, _ := json.Marshal(answered)
		assert.Contains(t, string(said), refused.says, refused.what)
	}
	entries, err := os.ReadDir(ran)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "something refused was said to the Node Agent")
}

// A task no id names is not found, and nor is one at a tenant (§3.3.2).
func TestATaskNoIdNamesIsNotFound(t *testing.T) {
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	code, answered := a2aAsked(t, srv, http.MethodGet, "/a2a/tasks/nothing", "", tokens[auth.LevelRoot])
	assert.Equal(t, http.StatusNotFound, code)
	said, _ := json.Marshal(answered)
	assert.Contains(t, string(said), "TASK_NOT_FOUND")
}

// The card says the Node Agent takes text and gives text: what a client reads
// before it sends (§3.3.4).
func TestTheNodeAgentsSkillTakesText(t *testing.T) {
	namedNode(t)
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	code, card := a2aAsked(t, srv, http.MethodGet, "/a2a/extendedAgentCard", "", tokens[auth.LevelRoot])
	require.Equal(t, http.StatusOK, code, card)
	skills, _ := at(card, "skills").([]any)
	var agent any
	for _, skill := range skills {
		if at(skill, "id") == nodeAgentSignumName {
			agent = skill
		}
	}
	require.NotNil(t, agent, "ROOT's card has no skill for the Node Agent")
	assert.Equal(t, []any{"text/plain"}, at(agent, "inputModes"))
	assert.Equal(t, []any{"text/plain"}, at(agent, "outputModes"))
}
