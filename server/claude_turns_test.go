package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/claudecode"
)

func streamedMessage(t *testing.T, line string) claudecode.Message {
	t.Helper()
	var m claudecode.Message
	require.NoError(t, json.Unmarshal([]byte(line), &m))
	return m
}

// An agent's session reads as a transcript from what the agent itself wrote
// down: what it was told, what it reached for and what it answered, each row
// its own DID's (ADR-048).
func TestAnAgentsSessionReadsAsATranscript(t *testing.T) {
	const did = "did:key:zAgent"
	at := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	writes := sessionWriter{did: did, session: "s-1", effort: "low"}

	var rows []*types.As
	told, err := writes.told("what does uname -s print?", "tim", at)
	require.NoError(t, err)
	rows = append(rows, told)
	for i, line := range []string{
		`{"type":"system","subtype":"init","session_id":"s-1","model":"claude-opus-5-5"}`,
		`{"type":"assistant","session_id":"s-1","timestamp":"2026-10-05T09:00:02.000Z","message":{"content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"uname -s"}},{"type":"tool_use","id":"toolu_2","name":"Read","input":{"file_path":"/etc/os-release"}}]}}`,
		`{"type":"user","session_id":"s-1","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"Linux"}]}}`,
		`{"type":"assistant","session_id":"s-1","timestamp":"2026-10-05T09:00:04.000Z","message":{"content":[{"type":"text","text":"It printed Linux."}]}}`,
		`{"type":"result","subtype":"success","session_id":"s-1","is_error":false,"result":"It printed Linux."}`,
	} {
		more, err := writes.rowsOf(streamedMessage(t, line), at.Add(time.Duration(i+1)*time.Second))
		require.NoError(t, err)
		rows = append(rows, more...)
	}

	for _, row := range rows {
		assert.Equal(t, []string{did}, row.Actors, "%s is not the agent's own", row.Predicates[0])
		assert.Equal(t, []string{"session:s-1"}, row.Contexts)
	}

	read := transcriptsOf(rows, 10)
	require.Len(t, read, 1)
	assert.Equal(t, "claude-opus-5-5", read[0].Model)
	// On the box the session read with no effort at all.
	assert.Equal(t, "low", read[0].Effort)
	var said [][2]string
	for _, turn := range read[0].Turns {
		said = append(said, [2]string{turn.Speaker, turn.Text})
	}
	assert.Equal(t, [][2]string{
		{"human", "what does uname -s print?"},
		{"session", "Start startup"},
		{"tool", "uname -s"},
		{"read", "/etc/os-release"},
		{"assistant", "It printed Linux."},
	}, said)
}

// A turn Claude Code reports as failed is said as the error it was.
func TestAnAgentsFailedTurnIsAnErrorTurn(t *testing.T) {
	writes := sessionWriter{did: "did:key:zAgent", session: "s-1", resumed: true}
	at := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)

	started, err := writes.rowsOf(streamedMessage(t, `{"type":"system","subtype":"init","session_id":"s-1","model":"m"}`), at)
	require.NoError(t, err)
	failed, err := writes.rowsOf(streamedMessage(t, `{"type":"result","subtype":"error_during_execution","session_id":"s-1","is_error":true,"result":"API Error: 529 overloaded"}`), at.Add(time.Second))
	require.NoError(t, err)

	read := transcriptsOf(append(started, failed...), 10)
	require.Len(t, read, 1)
	require.Len(t, read[0].Turns, 2)
	assert.Equal(t, "Start resume", read[0].Turns[0].Text)
	assert.Equal(t, "error", read[0].Turns[1].Speaker)
	assert.Equal(t, "error_during_execution: API Error: 529 overloaded", read[0].Turns[1].Text)
}
