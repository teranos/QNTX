package server

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/a2a"
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
	allowed := webhookAddress
	webhookAddress = func(net.IP) bool { return true }
	t.Cleanup(func() { webhookAddress = allowed })
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

	completed := stateOf(t, took, task, a2a.Completed)
	assert.Equal(t, "Bearer secure-client-token", completed.authorization)
	assert.Equal(t, "application/a2a+json", completed.contentType)
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
		code, deleted := a2aAsked(t, srv, http.MethodDelete, configs+"/c1", "", root)
		assert.Equal(t, http.StatusOK, code, deleted)
	}
	code, _ = a2aAsked(t, srv, http.MethodGet, configs+"/c1", "", root)
	assert.Equal(t, http.StatusNotFound, code, "a deleted config was got")
	code, listed = a2aAsked(t, srv, http.MethodGet, configs, "", root)
	require.Equal(t, http.StatusOK, code, listed)
	assert.Empty(t, at(listed, "configs"))

	code, refused := a2aAsked(t, srv, http.MethodPost, "/a2a/tasks/nothing/pushNotificationConfigs", `{"url": "`+hook+`"}`, root)
	assert.Equal(t, http.StatusNotFound, code)
	said, _ = json.Marshal(refused)
	assert.Contains(t, string(said), "TASK_NOT_FOUND")
}

// "Reject private IP ranges ... Reject localhost and link-local addresses"
// (§13.2): where the URL is set, and again where it is dialed.
func TestAWebhookTheInternetDoesNotRouteToIsRefused(t *testing.T) {
	for _, url := range []string{"http://127.0.0.1/hook", "http://10.0.0.1/hook", "http://169.254.169.254/latest", "http://localhost:8770/hook", "ftp://example.org/hook"} {
		assert.NotNil(t, webhookRefused(a2a.Push{URL: url}), url)
	}
	assert.Nil(t, webhookRefused(a2a.Push{URL: "https://client.example.com/webhook/a2a-notifications"}))

	hook := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("the node POSTed to a loopback address")
	}))
	t.Cleanup(hook.Close)
	srv, _, _ := nodeAgentOverA2A(t, "")
	// A name the URL check lets by, which resolves to loopback where it is dialed.
	err := srv.post(a2a.Push{URL: strings.Replace(hook.URL, "127.0.0.1", "localhost", 1), TaskID: "t"}, []byte(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no public address")
}
