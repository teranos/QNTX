package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
	"github.com/teranos/QNTX/server/statusline"
)

// "AGENTS CAN TALK TO EACH OTHER BY THEIR NAME"
// "AND GROUND DELIVERS IT VIA UG AND STATUSLINE"

// namedOverA2A is a node whose agent Birch named itself, and ROOT's token.
func namedOverA2A(t *testing.T) (*QNTXServer, string) {
	t.Helper()
	held := filepath.Join(t.TempDir(), "held")
	require.NoError(t, os.WriteFile(held, nil, 0o600))
	srv, tokens, _ := nodeAgentOverA2A(t, held)
	srv.news = statusline.NewNewsLog()
	birch := auth.Admitted(auth.LevelRoot, "garden")
	birch.Identity, birch.ClientDID, birch.TokenDID = rootAccount, "did:key:z6Mkbirch", "did:key:z6Mkbirch"
	_, refusal := srv.iName(auth.WithAdmission(context.Background(), birch), sigil.Sent{"name": "Birch"})
	require.Nil(t, refusal, refusal.GetSays())
	return srv, tokens[auth.LevelRoot]
}

// taskStateIn is the state the task answered stands in.
func taskStateIn(answered map[string]any) string {
	task, _ := answered["task"].(map[string]any)
	if task == nil {
		task = answered
	}
	status, _ := task["status"].(map[string]any)
	state, _ := status["state"].(string)
	return state
}

func TestWhatIsSaidToANameIsATaskAndIsOnTheStatusRow(t *testing.T) {
	srv, root := namedOverA2A(t)

	code, answered := a2aAsked(t, srv, http.MethodPost, "/a2a/Birch/message:send", saidToTheNode("the build is red", `{"returnImmediately": true}`), root)
	require.Equal(t, http.StatusOK, code, answered)
	assert.Equal(t, "TASK_STATE_SUBMITTED", taskStateIn(answered))
	task := answered["task"].(map[string]any)
	id := task["id"].(string)
	assert.Equal(t, "did:key:z6Mkbirch", task["contextId"], "the context is the agent it was said to")

	// On the row, for Birch, as ug reads it: the item by the Task's id, and
	// the whole of it naming who it is for.
	var left []statusline.News
	require.Eventually(t, func() bool {
		left = srv.news.Since("did:key:z6Mkbirch", time.Now().UnixMilli())
		return len(left) == 1
	}, 5*time.Second, 10*time.Millisecond)
	assert.Equal(t, id, left[0].ID)
	assert.Equal(t, "the build is red", left[0].Item.Note)
	assert.Equal(t, "Birch", left[0].Detail["to"])
	assert.Equal(t, "did:key:z6Mkbirch", left[0].Detail["did"])

	// A ground takes it from the row: the Task is completed, and off the row.
	left[0].Taken()
	require.Eventually(t, func() bool {
		_, task := a2aAsked(t, srv, http.MethodGet, "/a2a/Birch/tasks/"+id, "", root)
		return taskStateIn(task) == "TASK_STATE_COMPLETED"
	}, 5*time.Second, 10*time.Millisecond)
	assert.Empty(t, srv.news.Since("did:key:z6Mkbirch", time.Now().UnixMilli()))
}

func TestWhatNoGroundTookIsNotReceived(t *testing.T) {
	srv, root := namedOverA2A(t)
	within := toldWithin
	toldWithin = 50 * time.Millisecond
	t.Cleanup(func() { toldWithin = within })

	code, answered := a2aAsked(t, srv, http.MethodPost, "/a2a/Birch/message:send", saidToTheNode("anyone there", `{"returnImmediately": true}`), root)
	require.Equal(t, http.StatusOK, code, answered)
	id := answered["task"].(map[string]any)["id"].(string)
	var ended map[string]any
	require.Eventually(t, func() bool {
		_, ended = a2aAsked(t, srv, http.MethodGet, "/a2a/Birch/tasks/"+id, "", root)
		return taskStateIn(ended) == "TASK_STATE_FAILED"
	}, 5*time.Second, 10*time.Millisecond)
	assert.Contains(t, toJSON(t, ended), "Birch was not told")
}

func TestANameNobodyHoldsIsNoAgent(t *testing.T) {
	srv, root := namedOverA2A(t)
	code, answered := a2aAsked(t, srv, http.MethodPost, "/a2a/Rowan/message:send", saidToTheNode("hello", `{"returnImmediately": true}`), root)
	assert.Equal(t, http.StatusNotFound, code, answered)
	assert.Contains(t, toJSON(t, answered), "no agent is served at tenant Rowan")
}

// toJSON is v as its JSON, to look for words in.
func toJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	return string(raw)
}
