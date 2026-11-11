package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv/push"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/auth"
)

// "Push notifications is something I want though, especially for approvals":
// a Task's updates are POSTed to the webhook its config names (§4.3.3).

// posted is one notification as a webhook took it.
type posted struct {
	authorization, contentType string
	body                       map[string]any
}

// aWebhook is a webhook on loopback, which the node is let to POST to for one
// test, and what it was POSTed.
func aWebhook(t *testing.T) (string, <-chan posted) {
	t.Helper()
	sender := pushSender
	pushSender = push.NewHTTPPushSender(&push.HTTPSenderConfig{AllowPrivateNetworks: true})
	t.Cleanup(func() { pushSender = sender })
	took := make(chan posted, 16)
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		took <- posted{authorization: r.Header.Get("Authorization"), contentType: r.Header.Get("Content-Type"), body: body}
	}))
	t.Cleanup(hook.Close)
	return hook.URL + "/hook", took
}

// stateOf waits for the webhook to be told the task reached state.
func stateOf(t *testing.T, took <-chan posted, task, state string) posted {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case one := <-took:
			if at(one.body, "task.id") == task && at(one.body, "task.status.state") == state {
				return one
			}
		case <-timeout:
			t.Fatalf("the webhook was not told task %s is %s", task, state)
		}
	}
}

// A config sent with the message is the Task's, and the webhook is told of each
// state the Task reaches, with the credentials the config carries.
func TestATaskSentWithAPushConfigIsPostedToItsWebhook(t *testing.T) {
	hook, took := aWebhook(t)
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	keyedNode(srv)

	code, sent := a2aAsked(t, srv, http.MethodPost, "/a2a/message:send", saidToTheNode("how long has the box been up?",
		`{"taskPushNotificationConfig": {"url": "`+hook+`", "authentication": {"scheme": "Bearer", "credentials": "secure-client-token"}}}`), tokens[auth.LevelRoot])
	require.Equal(t, http.StatusOK, code, sent)
	task := at(sent, "task.id").(string)

	completed := stateOf(t, took, task, string(a2asdk.TaskStateCompleted))
	assert.Equal(t, "Bearer secure-client-token", completed.authorization)
	assert.NotEmpty(t, at(completed.body, "task.artifacts"), "the completed task was POSTed without its answer")
}

// A config is created for a Task, listed, got and deleted; its credentials are
// never answered back, and deleting it twice is deleting it once (§3.1.10).
func TestAPushConfigIsCreatedListedGotAndDeleted(t *testing.T) {
	hook, _ := aWebhook(t)
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	keyedNode(srv)
	root := tokens[auth.LevelRoot]

	code, sent := a2aAsked(t, srv, http.MethodPost, "/a2a/message:send", saidToTheNode("hi", ""), root)
	require.Equal(t, http.StatusOK, code, sent)
	task := at(sent, "task.id").(string)
	configs := "/a2a/tasks/" + task + "/pushNotificationConfigs"

	code, created := a2aAsked(t, srv, http.MethodPost, configs,
		`{"id": "c1", "url": "`+hook+`", "token": "t", "authentication": {"scheme": "Bearer", "credentials": "kept-here"}}`, root)
	require.Equal(t, http.StatusOK, code, created)
	assert.Equal(t, "c1", at(created, "id"))
	assert.Equal(t, task, at(created, "taskId"))
	assert.Equal(t, "t", at(created, "token"))
	assert.Equal(t, "Bearer", at(created, "authentication.scheme"))

	code, listed := a2aAsked(t, srv, http.MethodGet, configs, "", root)
	require.Equal(t, http.StatusOK, code, listed)
	assert.Len(t, at(listed, "configs"), 1)
	said, _ := json.Marshal(listed)
	assert.NotContains(t, string(said), "kept-here", "a config's credentials were answered back")

	code, got := a2aAsked(t, srv, http.MethodGet, configs+"/c1", "", root)
	require.Equal(t, http.StatusOK, code, got)
	assert.Equal(t, hook, at(got, "url"))

	for range 2 {
		r := asBearer(http.MethodDelete, configs+"/c1", root)
		r.Header.Set("A2A-Version", "1.0")
		w := httptest.NewRecorder()
		srv.served.ServeHTTP(w, r)
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}
	code, _ = a2aAsked(t, srv, http.MethodGet, configs+"/c1", "", root)
	assert.Equal(t, http.StatusNotFound, code, "a deleted config was got")
	code, listed = a2aAsked(t, srv, http.MethodGet, configs, "", root)
	require.Equal(t, http.StatusOK, code, listed)
	assert.Empty(t, at(listed, "configs"))

	// A task no id names has no configs to create, list, get or delete.
	for _, asked := range []struct{ method, path, body string }{
		{http.MethodPost, "/a2a/tasks/nothing/pushNotificationConfigs", `{"url": "` + hook + `"}`},
		{http.MethodGet, "/a2a/tasks/nothing/pushNotificationConfigs", ""},
		{http.MethodGet, "/a2a/tasks/nothing/pushNotificationConfigs/c1", ""},
		{http.MethodDelete, "/a2a/tasks/nothing/pushNotificationConfigs/c1", ""},
	} {
		code, refused := a2aAsked(t, srv, asked.method, asked.path, asked.body, root)
		assert.Equal(t, http.StatusNotFound, code, asked.method+" "+asked.path)
		said, _ = json.Marshal(refused)
		assert.Contains(t, string(said), "TASK_NOT_FOUND", asked.method+" "+asked.path)
	}
}

// "Reject private IP ranges ... Reject localhost and link-local addresses"
// (§13.2): a2a-go's sender dials no loopback address.
func TestAWebhookTheInternetDoesNotRouteToIsRefused(t *testing.T) {
	hook := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("the node POSTed to a loopback address")
	}))
	t.Cleanup(hook.Close)
	for _, url := range []string{hook.URL, strings.Replace(hook.URL, "127.0.0.1", "localhost", 1)} {
		_ = pushSender.SendPush(context.Background(), &a2asdk.PushConfig{URL: url, TaskID: "t"}, &a2asdk.Task{ID: "t"})
	}
}

// A config deleted while an earlier webhook is still being told of a settled
// task is told nothing more: "No further notifications will be sent" (§3.1.10).
func TestAConfigDeletedWhileAnotherIsToldIsToldNothing(t *testing.T) {
	sender := pushSender
	pushSender = push.NewHTTPPushSender(&push.HTTPSenderConfig{AllowPrivateNetworks: true})
	t.Cleanup(func() { pushSender = sender })
	release, slowTook, fastTook := make(chan struct{}), make(chan struct{}, 4), make(chan struct{}, 4)
	slow := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { slowTook <- struct{}{}; <-release }))
	fast := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { fastTook <- struct{}{} }))
	t.Cleanup(slow.Close)
	t.Cleanup(fast.Close)
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	root := tokens[auth.LevelRoot]

	code, sent := a2aAsked(t, srv, http.MethodPost, "/a2a/message:send", saidToTheNode("hi", ""), root)
	require.Equal(t, http.StatusOK, code, sent)
	task := at(sent, "task.id").(string)
	configs := "/a2a/tasks/" + task + "/pushNotificationConfigs"
	for _, config := range []string{`{"id": "a-slow", "url": "` + slow.URL + `"}`, `{"id": "b-fast", "url": "` + fast.URL + `"}`} {
		code, created := a2aAsked(t, srv, http.MethodPost, configs, config, root)
		require.Equal(t, http.StatusOK, code, created)
	}
	ta, err := srv.nodeTaskAgent()
	require.NoError(t, err)
	held, _, _, found, err := srv.heldTask(ta.taskLines, task)
	require.NoError(t, err)
	require.True(t, found)

	srv.notifySettled(ta.taskLines, held)
	<-slowTook
	r := asBearer(http.MethodDelete, configs+"/b-fast", root)
	r.Header.Set("A2A-Version", "1.0")
	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	close(release)

	select {
	case <-fastTook:
		t.Fatal("a config deleted before its turn was still told")
	case <-time.After(300 * time.Millisecond):
	}
}

// A config replaced under its id while an earlier webhook is still being told
// is told at the URL it now names, not the one it named.
func TestAConfigReplacedWhileAnotherIsToldIsToldWhereItNowSays(t *testing.T) {
	sender := pushSender
	pushSender = push.NewHTTPPushSender(&push.HTTPSenderConfig{AllowPrivateNetworks: true})
	t.Cleanup(func() { pushSender = sender })
	release, slowTook, formerTook, nowTook := make(chan struct{}), make(chan struct{}, 4), make(chan struct{}, 4), make(chan struct{}, 4)
	slow := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { slowTook <- struct{}{}; <-release }))
	former := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { formerTook <- struct{}{} }))
	now := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { nowTook <- struct{}{} }))
	for _, hook := range []*httptest.Server{slow, former, now} {
		t.Cleanup(hook.Close)
	}
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	root := tokens[auth.LevelRoot]

	code, sent := a2aAsked(t, srv, http.MethodPost, "/a2a/message:send", saidToTheNode("hi", ""), root)
	require.Equal(t, http.StatusOK, code, sent)
	task := at(sent, "task.id").(string)
	configs := "/a2a/tasks/" + task + "/pushNotificationConfigs"
	for _, config := range []string{`{"id": "a-slow", "url": "` + slow.URL + `"}`, `{"id": "b", "url": "` + former.URL + `"}`} {
		code, created := a2aAsked(t, srv, http.MethodPost, configs, config, root)
		require.Equal(t, http.StatusOK, code, created)
	}
	ta, err := srv.nodeTaskAgent()
	require.NoError(t, err)
	held, _, _, found, err := srv.heldTask(ta.taskLines, task)
	require.NoError(t, err)
	require.True(t, found)

	srv.notifySettled(ta.taskLines, held)
	<-slowTook
	code, replaced := a2aAsked(t, srv, http.MethodPost, configs, `{"id": "b", "url": "`+now.URL+`"}`, root)
	require.Equal(t, http.StatusOK, code, replaced)
	close(release)

	select {
	case <-nowTook:
	case <-formerTook:
		t.Fatal("a replaced config was told at the URL it no longer names")
	case <-time.After(5 * time.Second):
		t.Fatal("the replaced config was not told")
	}
}

// A webhook that refuses a settled task is said in a2a-go's log with the task
// and the config it was refused for.
func TestARefusedSettledPushNamesItsTaskAndConfig(t *testing.T) {
	sender := pushSender
	pushSender = push.NewHTTPPushSender(&push.HTTPSenderConfig{AllowPrivateNetworks: true})
	t.Cleanup(func() { pushSender = sender })
	var said bytes.Buffer
	var mu sync.Mutex
	before := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(writerFunc(func(p []byte) (int, error) { mu.Lock(); defer mu.Unlock(); return said.Write(p) }), nil)))
	t.Cleanup(func() { slog.SetDefault(before) })
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	t.Cleanup(refusing.Close)
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	root := tokens[auth.LevelRoot]

	code, sent := a2aAsked(t, srv, http.MethodPost, "/a2a/message:send", saidToTheNode("hi", ""), root)
	require.Equal(t, http.StatusOK, code, sent)
	task := at(sent, "task.id").(string)
	code, created := a2aAsked(t, srv, http.MethodPost, "/a2a/tasks/"+task+"/pushNotificationConfigs", `{"id": "refuses", "url": "`+refusing.URL+`"}`, root)
	require.Equal(t, http.StatusOK, code, created)
	ta, err := srv.nodeTaskAgent()
	require.NoError(t, err)
	held, _, _, found, err := srv.heldTask(ta.taskLines, task)
	require.NoError(t, err)
	require.True(t, found)

	srv.notifySettled(ta.taskLines, held)
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		line := said.String()
		return strings.Contains(line, "push sending failed") && strings.Contains(line, "task="+task) && strings.Contains(line, "config=refuses")
	}, 5*time.Second, 20*time.Millisecond, "the refused push was not said with its task and config")
}

// writerFunc is a function as an io.Writer.
type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }
