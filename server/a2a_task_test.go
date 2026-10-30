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
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
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
// runs, working once Claude Code runs it, in the session it runs in, and GetTask
// answers it when the turn has ended (§3.2.2).
func TestATaskReturnedImmediatelyIsAnsweredByItsIdWhenItEnds(t *testing.T) {
	held := filepath.Join(t.TempDir(), "held")
	require.NoError(t, os.WriteFile(held, nil, 0o600))
	srv, tokens, ran := nodeAgentOverA2A(t, held)

	code, sent := a2aAsked(t, srv, http.MethodPost, "/a2a/message:send", saidToTheNode("how long has the box been up?", `{"returnImmediately": true}`), tokens[auth.LevelRoot])
	require.Equal(t, http.StatusOK, code, sent)
	assert.Contains(t, []any{"TASK_STATE_SUBMITTED", "TASK_STATE_WORKING"}, at(sent, "task.status.state"))
	assert.Nil(t, at(sent, "task.artifacts"))
	id := at(sent, "task.id").(string)
	// The first message: the session it generates is in the answer, before its
	// turn has run (§3.4.1).
	context, _ := at(sent, "task.contextId").(string)
	require.NotEmpty(t, context, "a generated contextId MUST be included in the response")

	// "Indicates that a task is actively being processed by the agent."
	var working map[string]any
	require.Eventually(t, func() bool {
		_, working = a2aAsked(t, srv, http.MethodGet, "/a2a/tasks/"+id, "", tokens[auth.LevelRoot])
		return at(working, "status.state") == "TASK_STATE_WORKING"
	}, 10*time.Second, 20*time.Millisecond)
	assert.Nil(t, at(working, "artifacts"))

	require.NoError(t, os.Remove(held))
	require.Eventually(t, func() bool {
		_, got := a2aAsked(t, srv, http.MethodGet, "/a2a/tasks/"+id, "", tokens[auth.LevelRoot])
		return at(got, "status.state") == "TASK_STATE_COMPLETED"
	}, 10*time.Second, 20*time.Millisecond)
	_, got := a2aAsked(t, srv, http.MethodGet, "/a2a/tasks/"+id, "", tokens[auth.LevelRoot])
	assert.Equal(t, "Up 3 days.", at(got, "artifacts.0.parts.0.text"))
	// Working, it was in the session its turn ran in.
	assert.Equal(t, after(ranWith(t, ran, "0", "args"), "--session-id"), at(working, "contextId"))
	assert.Equal(t, context, after(ranWith(t, ran, "0", "args"), "--session-id"), "the turn ran in another session than the one answered")
}

// A first message in a context that is not the agent's is refused, and keeps no session (§3.4.1).
func TestAFirstMessageInAnotherContextKeepsNoSession(t *testing.T) {
	srv, tokens, ran := nodeAgentOverA2A(t, "")
	code, answered := a2aAsked(t, srv, http.MethodPost, "/a2a/message:send",
		`{"message": {"messageId": "m", "role": "ROLE_USER", "contextId": "another", "parts": [{"text": "hi"}]}}`, tokens[auth.LevelRoot])
	assert.Equal(t, http.StatusBadRequest, code, answered)
	_, kept, err := srv.nodeAgent.sessionIn(srv.nodeAgent.in(srv.claudeHarness()).file)
	require.NoError(t, err)
	assert.False(t, kept, "a refused context generated a session")
	entries, err := os.ReadDir(ran)
	require.NoError(t, err)
	assert.Empty(t, entries, "something refused was said to the Node Agent")
}

// Two first messages at once are one session: each Task answers the same context.
func TestTwoFirstMessagesAtOnceAreOneContext(t *testing.T) {
	held := filepath.Join(t.TempDir(), "held")
	require.NoError(t, os.WriteFile(held, nil, 0o600))
	srv, tokens, _ := nodeAgentOverA2A(t, held)

	// Each message is sent from its own goroutine and read here: require stops
	// the test from its own goroutine alone, and a goroutine it stopped would
	// leave this one waiting on its answer until the suite times out.
	answers := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		go func() {
			body := saidToTheNode("hi", `{"returnImmediately": true}`)
			r := asBearer(http.MethodPost, "/a2a/message:send", tokens[auth.LevelRoot])
			r.Body = io.NopCloser(strings.NewReader(body))
			r.ContentLength = int64(len(body))
			r.Header.Set("A2A-Version", "1.0")
			w := httptest.NewRecorder()
			srv.served.ServeHTTP(w, r)
			answers <- w
		}()
	}
	var contexts []any
	for range 2 {
		w := <-answers
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var sent map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &sent), w.Body.String())
		contexts = append(contexts, at(sent, "task.contextId"))
	}
	require.NotEmpty(t, contexts[0])
	assert.Equal(t, contexts[0], contexts[1])
	require.NoError(t, os.Remove(held))
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
		{"a client that takes back no text",
			saidToTheNode("hi", `{"acceptedOutputModes": ["application/json", "image/png"]}`),
			http.StatusBadRequest, "CONTENT_TYPE_NOT_SUPPORTED"},
		{"a push notification to a private address",
			saidToTheNode("hi", `{"taskPushNotificationConfig": {"url": "http://10.0.0.1/hook"}}`),
			http.StatusBadRequest, "no public address"},
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

// leftTurn is a turn the node's last process left in agent's home: its stream,
// printed whole, by a process that is gone.
func leftTurn(t *testing.T, agent *rootAgent, session, printed string) string {
	t.Helper()
	stream := filepath.Join(agent.home, "turns", session+".1"+".stream")
	require.NoError(t, os.MkdirAll(filepath.Dir(stream), 0o700))
	require.NoError(t, os.WriteFile(stream, []byte(printed), 0o600))
	return stream
}

// readingOn is a node reading on what its last process left, as one started
// at since does.
func readingOn(srv *QNTXServer, since time.Time) {
	in := srv.nodeAgent.in(srv.claudeHarness())
	in.turn <- struct{}{}
	srv.followLeftTurns(srv.nodeAgent, in, "low", since)
}

// A Task whose turn was running when the node stopped is settled by the node
// after it, from what the turn printed (ADR-048: the node "starts it, resumes
// it and stops it").
func TestATaskWhoseTurnTheLastNodeLeftIsSettledFromItsStream(t *testing.T) {
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	before := time.Now().Add(-time.Minute)
	stream := leftTurn(t, srv.nodeAgent, "s-left", claudeAnswered)
	require.NoError(t, srv.writeTaskLine(srv.systemTasks(srv.nodeAgent.did), "left", "TASK_STATE_SUBMITTED", "someone", before, map[string]any{"message": `{"messageId":"m","role":"ROLE_USER","parts":[{"text":"up?"}]}`, "context_id": ""}))
	require.NoError(t, srv.writeTaskLine(srv.systemTasks(srv.nodeAgent.did), "left", "TASK_STATE_WORKING", srv.nodeAgent.did, before.Add(time.Second), map[string]any{"context_id": "s-left", "stream": filepath.Base(stream)}, streamContext(stream)))

	readingOn(srv, time.Now())

	code, got := a2aAsked(t, srv, http.MethodGet, "/a2a/tasks/left", "", tokens[auth.LevelRoot])
	require.Equal(t, http.StatusOK, code, got)
	assert.Equal(t, "TASK_STATE_COMPLETED", at(got, "status.state"))
	assert.Equal(t, "Up 3 days.", at(got, "artifacts.0.parts.0.text"))
	assert.Equal(t, "s-left", at(got, "contextId"))
}

// What no turn is left to end is failed, saying why: a Task submitted and never
// started, and one working with no stream left to read. One submitted to this
// node is its own, and is left as it is.
func TestATaskNoTurnIsLeftToEndIsFailed(t *testing.T) {
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	before, since := time.Now().Add(-time.Minute), time.Now()
	message := map[string]any{"message": `{"messageId":"m","role":"ROLE_USER","parts":[{"text":"up?"}]}`, "context_id": ""}
	require.NoError(t, srv.writeTaskLine(srv.systemTasks(srv.nodeAgent.did), "waiting", "TASK_STATE_SUBMITTED", "someone", before, message))
	require.NoError(t, srv.writeTaskLine(srv.systemTasks(srv.nodeAgent.did), "running", "TASK_STATE_SUBMITTED", "someone", before, message))
	require.NoError(t, srv.writeTaskLine(srv.systemTasks(srv.nodeAgent.did), "running", "TASK_STATE_WORKING", srv.nodeAgent.did, before.Add(time.Second), map[string]any{"context_id": "s-gone", "stream": "s-gone.1.stream"}, streamContext("s-gone.1.stream")))
	require.NoError(t, srv.writeTaskLine(srv.systemTasks(srv.nodeAgent.did), "mine", "TASK_STATE_SUBMITTED", "someone", since.Add(time.Second), message))

	readingOn(srv, since)

	for task, why := range map[string]string{
		"waiting": "the node stopped before its turn started",
		"running": "no stream was left to read how it ended",
	} {
		_, got := a2aAsked(t, srv, http.MethodGet, "/a2a/tasks/"+task, "", tokens[auth.LevelRoot])
		assert.Equal(t, "TASK_STATE_FAILED", at(got, "status.state"), task)
		assert.Contains(t, at(got, "status.message.parts.0.text"), why, task)
	}
	_, got := a2aAsked(t, srv, http.MethodGet, "/a2a/tasks/mine", "", tokens[auth.LevelRoot])
	assert.Equal(t, "TASK_STATE_SUBMITTED", at(got, "status.state"))
}

// The node stopping while a Task's turn runs is not the Task failing: it stays
// working, and the node after it reads the turn on and settles it.
func TestTheNodeStoppingIsNotATasksTurnEnding(t *testing.T) {
	held := filepath.Join(t.TempDir(), "held")
	require.NoError(t, os.WriteFile(held, nil, 0o600))
	srv, tokens, _ := nodeAgentOverA2A(t, held)
	running, stop := context.WithCancel(context.Background())
	srv.ctx = running

	_, sent := a2aAsked(t, srv, http.MethodPost, "/a2a/message:send", saidToTheNode("how long has the box been up?", `{"returnImmediately": true}`), tokens[auth.LevelRoot])
	id := at(sent, "task.id").(string)
	require.Eventually(t, func() bool {
		_, got := a2aAsked(t, srv, http.MethodGet, "/a2a/tasks/"+id, "", tokens[auth.LevelRoot])
		return at(got, "status.state") == "TASK_STATE_WORKING"
	}, 10*time.Second, 20*time.Millisecond)

	stop()
	require.Eventually(t, func() bool { return srv.nodeAgent.in(srv.claudeHarness()).answering.Load() == nil }, 10*time.Second, 20*time.Millisecond)
	_, got := a2aAsked(t, srv, http.MethodGet, "/a2a/tasks/"+id, "", tokens[auth.LevelRoot])
	assert.Equal(t, "TASK_STATE_WORKING", at(got, "status.state"), "the node stopping failed the task")

	// The node after it: the turn it left was stopped with the node, and ended
	// with no result.
	srv.ctx = context.Background()
	readingOn(srv, time.Now())
	_, got = a2aAsked(t, srv, http.MethodGet, "/a2a/tasks/"+id, "", tokens[auth.LevelRoot])
	assert.Equal(t, "TASK_STATE_FAILED", at(got, "status.state"))
	assert.Contains(t, at(got, "status.message.parts.0.text"), "Claude Code ended with no result")
}

// stoppingAtTheStop is a store that stops the node as a turn's Stop is written:
// Claude Code has printed its result, and its stream is about to be gone.
type stoppingAtTheStop struct {
	ats.AttestationStore
	stop func()
}

func (s stoppingAtTheStop) CreateAttestation(as *types.As) error {
	if slices.Contains(as.Predicates, "Stop") {
		s.stop()
	}
	return s.AttestationStore.CreateAttestation(as)
}

// A turn that answered as the node stopped is its Task's answer. Its stream was
// read to its end and removed, so no node after it reads the answer again.
func TestATurnThatAnsweredAsTheNodeStoppedCompletesItsTask(t *testing.T) {
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	running, stop := context.WithCancel(context.Background())
	srv.ctx = running
	system, _ := createTestStore(t)
	srv.held.SetSystem(oneNamespace("system", stoppingAtTheStop{AttestationStore: system, stop: stop}))

	code, sent := a2aAsked(t, srv, http.MethodPost, "/a2a/message:send", saidToTheNode("how long has the box been up?", ""), tokens[auth.LevelRoot])
	require.Equal(t, http.StatusOK, code, sent)
	assert.Equal(t, "TASK_STATE_COMPLETED", at(sent, "task.status.state"))
	assert.Equal(t, "Up 3 days.", at(sent, "task.artifacts.0.parts.0.text"))
}

// ListTasks gives the Node Agent's Tasks the most recently updated first, a
// page at a time, each without its artifacts unless they are asked for, and
// says how many there are and where the next page starts (§3.1.4).
func TestTheNodeAgentsTasksAreListedTheMostRecentlyUpdatedFirst(t *testing.T) {
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	_, first := a2aAsked(t, srv, http.MethodPost, "/a2a/message:send", saidToTheNode("remember 7", ""), tokens[auth.LevelRoot])
	_, second := a2aAsked(t, srv, http.MethodPost, "/a2a/message:send", saidToTheNode("what did I ask you to remember?", ""), tokens[auth.LevelRoot])

	code, page := a2aAsked(t, srv, http.MethodGet, "/a2a/tasks?pageSize=1", "", tokens[auth.LevelRoot])
	require.Equal(t, http.StatusOK, code, page)
	assert.Equal(t, at(second, "task.id"), at(page, "tasks.0.id"))
	assert.Nil(t, at(page, "tasks.1"))
	assert.Equal(t, float64(2), at(page, "totalSize"))
	assert.Equal(t, float64(1), at(page, "pageSize"))
	// "the artifacts field MUST be omitted entirely from each Task object"
	_, artifacts := at(page, "tasks.0").(map[string]any)["artifacts"]
	assert.False(t, artifacts)
	token, _ := at(page, "nextPageToken").(string)
	require.NotEmpty(t, token)

	code, page = a2aAsked(t, srv, http.MethodGet, "/a2a/tasks?pageSize=1&pageToken="+token, "", tokens[auth.LevelRoot])
	require.Equal(t, http.StatusOK, code, page)
	assert.Equal(t, at(first, "task.id"), at(page, "tasks.0.id"))
	// "When there are no more results to retrieve (i.e., this is the final
	// page), the field MUST be set to an empty string"
	next, present := page["nextPageToken"]
	assert.True(t, present, "nextPageToken MUST always be present")
	assert.Equal(t, "", next)

	_, page = a2aAsked(t, srv, http.MethodGet, "/a2a/tasks?includeArtifacts=true&historyLength=0", "", tokens[auth.LevelRoot])
	assert.Equal(t, "Up 3 days.", at(page, "tasks.0.artifacts.0.parts.0.text"))
	assert.Nil(t, at(page, "tasks.0.history"))

	_, page = a2aAsked(t, srv, http.MethodGet, "/a2a/tasks?contextId="+at(first, "task.contextId").(string)+"&status=TASK_STATE_COMPLETED", "", tokens[auth.LevelRoot])
	assert.Equal(t, float64(2), at(page, "totalSize"))
	_, page = a2aAsked(t, srv, http.MethodGet, "/a2a/tasks?status=TASK_STATE_WORKING", "", tokens[auth.LevelRoot])
	assert.Equal(t, []any{}, at(page, "tasks"))
	assert.Equal(t, float64(0), at(page, "totalSize"))
	_, page = a2aAsked(t, srv, http.MethodGet, "/a2a/tasks?statusTimestampAfter="+time.Now().Add(time.Hour).UTC().Format(time.RFC3339), "", tokens[auth.LevelRoot])
	assert.Equal(t, float64(0), at(page, "totalSize"))
}

// A listing the spec does not allow is refused, as its validation error
// example refuses it (§6.5), and so is a page token this node did not give.
func TestAListingOfTasksTheSpecDoesNotAllowIsRefused(t *testing.T) {
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	for _, query := range []string{"pageSize=150", "pageSize=0", "historyLength=-5", "status=TASK_STATE_RUNNING", "pageToken=nothing"} {
		code, answered := a2aAsked(t, srv, http.MethodGet, "/a2a/tasks?"+query, "", tokens[auth.LevelRoot])
		assert.Equal(t, http.StatusBadRequest, code, query+": %v", answered)
	}
}

// "ROOT can do anything": canceling a Task stops its turn, and it ends
// canceled however its turn ends after (§3.1.5).
func TestCancelingAWorkingTaskStopsItsTurn(t *testing.T) {
	held := filepath.Join(t.TempDir(), "held")
	require.NoError(t, os.WriteFile(held, nil, 0o600))
	srv, tokens, _ := nodeAgentOverA2A(t, held)

	_, sent := a2aAsked(t, srv, http.MethodPost, "/a2a/message:send", saidToTheNode("count to a million", `{"returnImmediately": true}`), tokens[auth.LevelRoot])
	id := at(sent, "task.id").(string)
	require.Eventually(t, func() bool {
		_, got := a2aAsked(t, srv, http.MethodGet, "/a2a/tasks/"+id, "", tokens[auth.LevelRoot])
		return at(got, "status.state") == "TASK_STATE_WORKING"
	}, 10*time.Second, 20*time.Millisecond)

	code, canceled := a2aAsked(t, srv, http.MethodPost, "/a2a/tasks/"+id+":cancel", `{}`, tokens[auth.LevelRoot])
	require.Equal(t, http.StatusOK, code, canceled)
	assert.Equal(t, "TASK_STATE_CANCELED", at(canceled, "status.state"))

	// The turn stopped while the stand-in was still held: nothing let it answer.
	require.Eventually(t, func() bool { return srv.nodeAgent.in(srv.claudeHarness()).answering.Load() == nil }, 10*time.Second, 20*time.Millisecond)
	assert.FileExists(t, held)
	_, got := a2aAsked(t, srv, http.MethodGet, "/a2a/tasks/"+id, "", tokens[auth.LevelRoot])
	assert.Equal(t, "TASK_STATE_CANCELED", at(got, "status.state"), "the turn ending wrote over its cancel")
}

// A Task canceled while its turn waits on another never starts its turn.
func TestATaskCanceledBeforeItsTurnStartsNeverStartsIt(t *testing.T) {
	srv, tokens, ran := nodeAgentOverA2A(t, "")
	in := srv.nodeAgent.in(srv.claudeHarness())
	in.turn <- struct{}{}

	_, sent := a2aAsked(t, srv, http.MethodPost, "/a2a/message:send", saidToTheNode("how long has the box been up?", `{"returnImmediately": true}`), tokens[auth.LevelRoot])
	id := at(sent, "task.id").(string)
	assert.Equal(t, "TASK_STATE_SUBMITTED", at(sent, "task.status.state"))
	code, canceled := a2aAsked(t, srv, http.MethodPost, "/a2a/tasks/"+id+":cancel", `{}`, tokens[auth.LevelRoot])
	require.Equal(t, http.StatusOK, code, canceled)
	assert.Equal(t, "TASK_STATE_CANCELED", at(canceled, "status.state"))

	<-in.turn
	_, stops := srv.taskStops.Load(id)
	require.Eventually(t, func() bool { _, stops = srv.taskStops.Load(id); return !stops }, 10*time.Second, 20*time.Millisecond)
	entries, err := os.ReadDir(ran)
	require.NoError(t, err)
	assert.Empty(t, entries, "the turn of a canceled task was run")
	_, got := a2aAsked(t, srv, http.MethodGet, "/a2a/tasks/"+id, "", tokens[auth.LevelRoot])
	assert.Equal(t, "TASK_STATE_CANCELED", at(got, "status.state"))
}

// A Task that ended is not canceled, a second cancel among them, and one no id
// names is not found (§3.1.5).
func TestATaskThatEndedIsNotCanceled(t *testing.T) {
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	_, done := a2aAsked(t, srv, http.MethodPost, "/a2a/message:send", saidToTheNode("remember 7", ""), tokens[auth.LevelRoot])
	require.NoError(t, srv.writeTaskLine(srv.systemTasks(srv.nodeAgent.did), "waiting", "TASK_STATE_SUBMITTED", "someone", time.Now(), map[string]any{"message": `{"messageId":"m","role":"ROLE_USER","parts":[{"text":"up?"}]}`, "context_id": ""}))
	code, _ := a2aAsked(t, srv, http.MethodPost, "/a2a/tasks/waiting:cancel", `{}`, tokens[auth.LevelRoot])
	require.Equal(t, http.StatusOK, code)

	for id, says := range map[string]string{
		at(done, "task.id").(string): "TASK_NOT_CANCELABLE",
		"waiting":                    "TASK_NOT_CANCELABLE",
		"nothing":                    "TASK_NOT_FOUND",
	} {
		code, answered := a2aAsked(t, srv, http.MethodPost, "/a2a/tasks/"+id+":cancel", `{}`, tokens[auth.LevelRoot])
		said, _ := json.Marshal(answered)
		assert.Contains(t, string(said), says, id)
		assert.NotEqual(t, http.StatusOK, code, id)
	}
}

// "Agents MUST validate all input parameters before processing" (§13): a
// negative historyLength is refused wherever it is asked, as ListTasks refuses
// it, and nothing is said to the Node Agent.
func TestANegativeHistoryLengthIsRefused(t *testing.T) {
	srv, tokens, ran := nodeAgentOverA2A(t, "")
	_, done := a2aAsked(t, srv, http.MethodPost, "/a2a/message:send", saidToTheNode("remember 7", ""), tokens[auth.LevelRoot])
	id := at(done, "task.id").(string)

	code, got := a2aAsked(t, srv, http.MethodGet, "/a2a/tasks/"+id+"?historyLength=-5", "", tokens[auth.LevelRoot])
	assert.Equal(t, http.StatusBadRequest, code, got)
	assert.Contains(t, at(got, "error.message"), "historyLength must be a non-negative integer, got -5")

	code, got = a2aAsked(t, srv, http.MethodPost, "/a2a/message:send", saidToTheNode("again", `{"historyLength": -5}`), tokens[auth.LevelRoot])
	assert.Equal(t, http.StatusBadRequest, code, got)
	assert.Contains(t, at(got, "error.message"), "historyLength must be a non-negative integer, got -5")
	entries, err := os.ReadDir(ran)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "a refused message was said to the Node Agent")
}
