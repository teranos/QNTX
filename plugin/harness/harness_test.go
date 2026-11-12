package harness

import (
	"encoding/json"
	"testing"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 「[Fable 5.2, Fable 5.1] A number a tool was called with is the number the harness printed: past」
// 「[Fable 5.2, Fable 5.1] 2^53 a float64 holds another.」
func TestARecordsNumbersCrossAsPrinted(t *testing.T) {
	type record struct {
		Args json.RawMessage `json:"args"`
	}
	printed := record{Args: json.RawMessage(`{"offset":9007199254740993}`)}

	said, err := Said("read on", printed)
	require.NoError(t, err)
	var ran record
	says, err := SaidIn(said, &ran)
	require.NoError(t, err)
	assert.Equal(t, "read on", says)
	assert.JSONEq(t, string(printed.Args), string(ran.Args))
	assert.Contains(t, string(ran.Args), "9007199254740993")

	task := &a2asdk.Task{ID: "t-1", ContextID: "c-1"}
	event, err := Printed(task, printed)
	require.NoError(t, err)
	var streamed record
	carries, err := PrintedIn(event, &streamed)
	require.NoError(t, err)
	require.True(t, carries)
	assert.Contains(t, string(streamed.Args), "9007199254740993")

	completed, err := Answered(task, "done", printed)
	require.NoError(t, err)
	var answered record
	text, err := AnswerIn(completed, &answered)
	require.NoError(t, err)
	assert.Equal(t, "done", text)
	assert.Contains(t, string(answered.Args), "9007199254740993")
}
