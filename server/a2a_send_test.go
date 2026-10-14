package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/auth"
)

// sendingToGardens is servingGardens with garden's agent there to speak to: a
// stand-in for Claude Code, and the agent signed in to it.
func sendingToGardens(t *testing.T) (*QNTXServer, map[auth.Level]string, gardens) {
	t.Helper()
	namedNode(t)
	srv, tokens := pluginServingServer(t, "fake")
	keyedNode(srv)
	in := gardens{store: map[string]*handed{}, owner: map[string]string{}}
	for _, name := range []string{"garden", "orchard"} {
		store, _ := createTestStore(t)
		in.store[name] = &handed{AttestationStore: store}
	}
	srv.held.SetKnown(in)
	srv.held.SetOpener(in)
	root := auth.Admitted(auth.LevelRoot)
	root.Identity = rootAccount
	require.NoError(t, srv.setAgentLine(root, "garden", agentSpec{Model: "claude-sonnet-5-5", Effort: "medium", Mode: "dontAsk", Allow: []string{}}))

	binary, _ := claudeStandIn(t, claudeAnswered)
	fetched := make(chan struct{})
	close(fetched)
	srv.holdHarness("claude", &harnessHeld{fetched: fetched, path: binary})
	srv.agentsDir = t.TempDir()
	srv.ctx = context.Background()
	// Signed in, as claude login leaves it.
	config := filepath.Join(srv.agentsDir, "garden", "claude-sonnet-5-5", "claude")
	require.NoError(t, os.MkdirAll(config, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(config, ".credentials.json"), []byte("signed-in\n"), 0o600))
	return srv, tokens, in
}

// a2aAsked is one A2A request as a client sends it: the version said, and a
// body for a method that carries one.
func a2aAsked(srv *QNTXServer, method, path, token, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("A2A-Version", "1.0")
	r.Header.Set("Content-Type", "application/a2a+json")
	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, r)
	return w
}

// aTask is a Task as JSON writes it, what a test reads of one.
type aTask struct {
	ID        string `json:"id"`
	ContextID string `json:"contextId"`
	Status    struct {
		State   string   `json:"state"`
		Message aMessage `json:"message"`
	} `json:"status"`
	History []aMessage `json:"history"`
}

type aMessage struct {
	MessageID string `json:"messageId"`
	TaskID    string `json:"taskId"`
	ContextID string `json:"contextId"`
	Role      string `json:"role"`
	Parts     []struct {
		Text string `json:"text"`
	} `json:"parts"`
}

func (m aMessage) text() string {
	var said []string
	for _, part := range m.Parts {
		said = append(said, part.Text)
	}
	return strings.Join(said, "")
}

// sentTask reads SendMessage's answer: the Task the message made.
func sentTask(t *testing.T, w *httptest.ResponseRecorder) aTask {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var sent struct {
		Task aTask `json:"task"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &sent), w.Body.String())
	return sent.Task
}

// readTask reads GetTask's answer.
func readTask(t *testing.T, w *httptest.ResponseRecorder) aTask {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var task aTask
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &task), w.Body.String())
	return task
}

// sendBody is a SendMessageRequest saying text, with the configuration given.
func sendBody(id, text, configuration string) string {
	body := `{"message":{"messageId":"` + id + `","role":"ROLE_USER","parts":[{"text":"` + text + `"}]}`
	if configuration != "" {
		body += `,"configuration":` + configuration
	}
	return body + `}`
}

// "reached over A2A by its tenant" (ADR-048): a message sent to garden's
// agent is said to it, and SendMessage waits for the Task to end (§3.2.2,
// blocking is the default). The Task holds what was said and what the agent
// said back, in the one session it continues, and reads the same afterwards.
func TestAMessageSentToANamespaceAgentIsATaskItAnswered(t *testing.T) {
	srv, tokens, _ := sendingToGardens(t)

	task := sentTask(t, a2aAsked(srv, http.MethodPost, "/a2a/garden/message:send", tokens[auth.LevelRoot], sendBody("m-1", "how long has the box been up?", "")))
	require.NotEmpty(t, task.ID)
	require.Equal(t, "TASK_STATE_COMPLETED", task.Status.State)
	require.Equal(t, "Up 3 days.", task.Status.Message.text())
	require.Equal(t, "ROLE_AGENT", task.Status.Message.Role)
	require.Len(t, task.History, 2)
	require.Equal(t, "how long has the box been up?", task.History[0].text())
	require.Equal(t, "ROLE_USER", task.History[0].Role)
	require.Equal(t, "Up 3 days.", task.History[1].text())
	session, kept, err := srv.a2aSession(anAgentOf(t, srv, "garden"))
	require.NoError(t, err)
	require.True(t, kept, "the first turn keeps the session")
	require.Equal(t, session, task.ContextID, "the context is the one session the agent continues")
	for _, said := range task.History {
		require.Equal(t, task.ID, said.TaskID)
		require.Equal(t, task.ContextID, said.ContextID)
	}

	got := readTask(t, a2aAsked(srv, http.MethodGet, "/a2a/garden/tasks/"+task.ID, tokens[auth.LevelRoot], ""))
	require.Equal(t, task, got)

	w := a2aAsked(srv, http.MethodGet, "/a2a/garden/tasks", tokens[auth.LevelRoot], "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var listed struct {
		Tasks         []aTask `json:"tasks"`
		NextPageToken string  `json:"nextPageToken"`
		TotalSize     int     `json:"totalSize"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listed))
	require.Len(t, listed.Tasks, 1)
	require.Equal(t, task.ID, listed.Tasks[0].ID)
	require.Equal(t, 1, listed.TotalSize)
	require.Contains(t, w.Body.String(), `"nextPageToken":""`, "nextPageToken MUST always be present (§3.1.4)")

	// The next message continues the same session, named or not.
	again := sentTask(t, a2aAsked(srv, http.MethodPost, "/a2a/garden/message:send", tokens[auth.LevelRoot],
		`{"message":{"messageId":"m-2","contextId":"`+session+`","role":"ROLE_USER","parts":[{"text":"and now?"}]}}`))
	require.Equal(t, session, again.ContextID)
	require.NotEqual(t, task.ID, again.ID)
}

// anAgentOf is the agent the node holds for namespace, asked as ROOT.
func anAgentOf(t *testing.T, srv *QNTXServer, namespace string) anAgentHeld {
	t.Helper()
	asked := httptest.NewRequest(http.MethodPost, "/a2a/"+namespace+"/message:send", nil)
	root := auth.Admitted(auth.LevelRoot)
	root.Identity = rootAccount
	asked = asked.WithContext(auth.WithAdmission(asked.Context(), root))
	held, refused := srv.a2aAgentAt(asked, namespace)
	require.Nil(t, refused)
	return held
}

// "If true, the operation returns immediately after creating the task, even
// if processing is still in progress": the Task comes back unfinished, and the
// turn ends without anybody waiting, read with GetTask.
func TestATaskSentToReturnImmediatelyEndsWithoutAnybodyWaiting(t *testing.T) {
	srv, tokens, _ := sendingToGardens(t)

	task := sentTask(t, a2aAsked(srv, http.MethodPost, "/a2a/garden/message:send", tokens[auth.LevelRoot], sendBody("m-1", "how long has the box been up?", `{"returnImmediately":true}`)))
	require.Contains(t, []string{"TASK_STATE_SUBMITTED", "TASK_STATE_WORKING"}, task.Status.State)

	require.Eventually(t, func() bool {
		got := readTask(t, a2aAsked(srv, http.MethodGet, "/a2a/garden/tasks/"+task.ID, tokens[auth.LevelRoot], ""))
		return got.Status.State == "TASK_STATE_COMPLETED" && got.Status.Message.text() == "Up 3 days."
	}, 10*time.Second, 20*time.Millisecond)
}

// A part that is not text is a media type the agent does not take (§3.1.1
// ContentTypeNotSupportedError), and nothing is said to it.
func TestWhatIsNotTextIsNotSaid(t *testing.T) {
	srv, tokens, in := sendingToGardens(t)
	written := len(in.store["garden"].rows)

	for _, part := range []string{`{"url":"https://example.org/a.png","mediaType":"image/png"}`, `{"data":{"a":1}}`, `{"text":"hi","mediaType":"text/html"}`} {
		w := a2aAsked(srv, http.MethodPost, "/a2a/garden/message:send", tokens[auth.LevelRoot],
			`{"message":{"messageId":"m-1","role":"ROLE_USER","parts":[`+part+`]}}`)
		require.Equal(t, http.StatusBadRequest, w.Code, part+": "+w.Body.String())
		require.Contains(t, w.Body.String(), "CONTENT_TYPE_NOT_SUPPORTED", part)
	}
	require.Len(t, in.store["garden"].rows, written, "nothing was said, and nothing written")
}

// "If an agent cannot accept a client-provided contextId, it MUST reject the
// request with an error and MUST NOT generate a new contextId" (§3.4.1): the
// agent continues one session, and a context that is not it is refused.
func TestAContextTheAgentDoesNotContinueIsRefused(t *testing.T) {
	srv, tokens, _ := sendingToGardens(t)

	w := a2aAsked(srv, http.MethodPost, "/a2a/garden/message:send", tokens[auth.LevelRoot],
		`{"message":{"messageId":"m-1","contextId":"another","role":"ROLE_USER","parts":[{"text":"hi"}]}}`)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "has not been spoken to yet")

	sentTask(t, a2aAsked(srv, http.MethodPost, "/a2a/garden/message:send", tokens[auth.LevelRoot], sendBody("m-1", "hi", "")))
	w = a2aAsked(srv, http.MethodPost, "/a2a/garden/message:send", tokens[auth.LevelRoot],
		`{"message":{"messageId":"m-2","contextId":"another","role":"ROLE_USER","parts":[{"text":"hi"}]}}`)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "is not the session")
}

// "Agents MUST return a TaskNotFoundError if the provided taskId does not
// correspond to an existing task" (§3.4.2), and a task nobody made is not
// found to get or to cancel either.
func TestATaskNobodyMadeIsNotFound(t *testing.T) {
	srv, tokens, _ := sendingToGardens(t)

	for _, w := range []*httptest.ResponseRecorder{
		a2aAsked(srv, http.MethodPost, "/a2a/garden/message:send", tokens[auth.LevelRoot],
			`{"message":{"messageId":"m-1","taskId":"nobody-made-it","role":"ROLE_USER","parts":[{"text":"hi"}]}}`),
		a2aAsked(srv, http.MethodGet, "/a2a/garden/tasks/nobody-made-it", tokens[auth.LevelRoot], ""),
		a2aAsked(srv, http.MethodPost, "/a2a/garden/tasks/nobody-made-it:cancel", tokens[auth.LevelRoot], `{}`),
	} {
		require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), "TASK_NOT_FOUND")
	}
}

// "Messages sent to Tasks that are in a terminal state ... cannot accept
// further messages" (§3.1.1), and a task that ended is not canceled (§3.1.5).
func TestATaskThatEndedTakesNothingMore(t *testing.T) {
	srv, tokens, _ := sendingToGardens(t)
	task := sentTask(t, a2aAsked(srv, http.MethodPost, "/a2a/garden/message:send", tokens[auth.LevelRoot], sendBody("m-1", "hi", "")))

	w := a2aAsked(srv, http.MethodPost, "/a2a/garden/message:send", tokens[auth.LevelRoot],
		`{"message":{"messageId":"m-2","taskId":"`+task.ID+`","role":"ROLE_USER","parts":[{"text":"and?"}]}}`)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "UNSUPPORTED_OPERATION")

	w = a2aAsked(srv, http.MethodPost, "/a2a/garden/tasks/"+task.ID+":cancel", tokens[auth.LevelRoot], `{}`)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "TASK_NOT_CANCELABLE")
}

// ListTasks is newest status first, a page at a time, the token where a page
// ended (§3.1.4). A page size outside 1 to 100 is refused.
func TestTasksArePagedNewestFirst(t *testing.T) {
	srv, tokens, _ := sendingToGardens(t)
	first := sentTask(t, a2aAsked(srv, http.MethodPost, "/a2a/garden/message:send", tokens[auth.LevelRoot], sendBody("m-1", "first", "")))
	second := sentTask(t, a2aAsked(srv, http.MethodPost, "/a2a/garden/message:send", tokens[auth.LevelRoot], sendBody("m-2", "second", "")))

	type page struct {
		Tasks         []aTask `json:"tasks"`
		NextPageToken string  `json:"nextPageToken"`
		PageSize      int     `json:"pageSize"`
		TotalSize     int     `json:"totalSize"`
	}
	read := func(path string) page {
		w := a2aAsked(srv, http.MethodGet, path, tokens[auth.LevelRoot], "")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var p page
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &p))
		return p
	}
	one := read("/a2a/garden/tasks?pageSize=1")
	require.Len(t, one.Tasks, 1)
	require.Equal(t, second.ID, one.Tasks[0].ID, "newest first")
	require.Equal(t, 2, one.TotalSize)
	require.NotEmpty(t, one.NextPageToken)
	two := read("/a2a/garden/tasks?pageSize=1&pageToken=" + one.NextPageToken)
	require.Len(t, two.Tasks, 1)
	require.Equal(t, first.ID, two.Tasks[0].ID)
	require.Empty(t, two.NextPageToken)

	none := read("/a2a/garden/tasks?historyLength=0")
	require.Empty(t, none.Tasks[0].History, "0 is a request for no history (§3.2.4)")

	for _, size := range []string{"0", "101"} {
		w := a2aAsked(srv, http.MethodGet, "/a2a/garden/tasks?pageSize="+size, tokens[auth.LevelRoot], "")
		require.Equal(t, http.StatusBadRequest, w.Code, size+": "+w.Body.String())
	}
}

// A Task is a namespace agent's: the node itself takes none, and a namespace
// with no agent serves none (§3.3.2).
func TestATaskIsANamespaceAgents(t *testing.T) {
	srv, tokens, _ := sendingToGardens(t)

	w := a2aAsked(srv, http.MethodPost, "/a2a/message:send", tokens[auth.LevelRoot], sendBody("m-1", "hi", ""))
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "UNSUPPORTED_OPERATION")

	w = a2aAsked(srv, http.MethodPost, "/a2a/orchard/message:send", tokens[auth.LevelRoot], sendBody("m-1", "hi", ""))
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}
