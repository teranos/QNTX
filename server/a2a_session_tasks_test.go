package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
)

// The session is the Task store: a turn said through agents say, and not over
// A2A, is a Task all the same, read from the rows the session wrote.
func TestEveryTurnOfTheSessionIsATask(t *testing.T) {
	srv, tokens, _ := sendingToGardens(t)

	_, refused := answeredAs(srv, rootAccount, auth.LevelRoot, nil, "say", sigil.Sent{"namespace": "garden", "says": "how long has the box been up?"})
	require.Nil(t, refused)

	w := a2aAsked(srv, http.MethodGet, "/a2a/garden/tasks", tokens[auth.LevelRoot], "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var listed struct {
		Tasks []aTask `json:"tasks"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listed))
	require.Len(t, listed.Tasks, 1)
	task := listed.Tasks[0]
	require.Equal(t, "TASK_STATE_COMPLETED", task.Status.State)
	require.Equal(t, "how long has the box been up?", task.History[0].text())
	require.Equal(t, "Up 3 days.", task.Status.Message.text())
}

// What was sent is said, and the row is what is kept of it: the user's
// Message carries the row's id, the Task's, and not the one it was sent with.
func TestWhatWasSentIsKeptAsItsRow(t *testing.T) {
	srv, tokens, _ := sendingToGardens(t)

	task := sentTask(t, a2aAsked(srv, http.MethodPost, "/a2a/garden/message:send", tokens[auth.LevelRoot], sendBody("m-1", "hi", "")))
	require.Equal(t, task.ID, task.History[0].MessageID)
	require.NotEqual(t, "m-1", task.History[0].MessageID)
}
