package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/internal/acp"
)

// "「[Sonnet 5.5] Just like how I would like any harness to be pluggable」"
// 「[Opus 5.5] A turn over ACP is written down as the hook events Claude Code's are written as.」
func TestAnACPTurnReadsAsAClaudeCodeTurn(t *testing.T) {
	writes := sessionWriter{did: "did:key:zAgent", session: "s-1", effort: "low"}
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

	rows, err := writes.rowsOfACP(acp.Update{Kind: acp.Started}, "a-harness", at)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, []string{"SessionStart"}, rows[0].Predicates)
	assert.Equal(t, "a-harness", rows[0].Attributes["source"])

	rows, err = writes.rowsOfACP(acp.Update{Kind: acp.ToolCall, ToolCallID: "t1", ToolKind: "execute", Title: "uptime", RawInput: `{"CommandLine":"uptime -p"}`}, "a-harness", at)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, []string{"PreToolUse"}, rows[0].Predicates)
	assert.Equal(t, "Bash", rows[0].Attributes["tool_name"])
	assert.Equal(t, "uptime -p", rows[0].Attributes["command"])

	rows, err = writes.rowsOfACP(acp.Update{Kind: acp.ToolCall, ToolCallID: "t2", ToolKind: "read", RawInput: `{"AbsolutePath":"/etc/hosts"}`}, "a-harness", at)
	require.NoError(t, err)
	assert.Equal(t, "Read", rows[0].Attributes["tool_name"])
	assert.Equal(t, "/etc/hosts", rows[0].Attributes["file_path"])

	rows, err = writes.acpAnswered("done", at)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, []string{"Stop"}, rows[0].Predicates)
	assert.Equal(t, "done", rows[0].Attributes["last_assistant_message"])
	assert.Equal(t, map[string]any{"level": "low"}, rows[0].Attributes["effort"])
}

// 「[Opus 5.5] A kind Claude Code has no tool for keeps the agent's own name for it.」
func TestAToolOfNoClaudeCodeKindKeepsItsName(t *testing.T) {
	assert.Equal(t, "web_search", toolOfACP(acp.Update{ToolKind: "search", Name: "web_search", Title: "searching"}))
	assert.Equal(t, "searching", toolOfACP(acp.Update{ToolKind: "search", Title: "searching"}))
}
