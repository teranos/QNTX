package server

import (
	"encoding/json"
	"time"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/acp"
)

// 「[Opus 5.5] An ACP harness's turn written down as the hook events Claude Code's are written as, whichever agent」
// 「[Opus 5.5] runs it: the session starting, each tool it reached for, and the turn's end once it answered.」
// "「[Sonnet 5.5] Just like how I would like any harness to be pluggable」"

// 「[Opus 5.5] rowsOfACP is the rows of one update of a turn in the harness source names.」
func (w sessionWriter) rowsOfACP(u acp.Update, source string, at time.Time) ([]*types.As, error) {
	switch u.Kind {
	case acp.Started:
		row, err := w.row("SessionStart", at, map[string]any{"source": source})
		return []*types.As{row}, err
	case acp.ToolCall:
		attrs := map[string]any{"tool_use_id": u.ToolCallID, "tool_name": toolOfACP(u)}
		input := map[string]any{}
		_ = json.Unmarshal([]byte(u.RawInput), &input)
		if u.ToolKind == "execute" {
			attrs["command"] = firstText(input, "command", "CommandLine", "cmd")
			if attrs["command"] == "" {
				attrs["command"] = u.Title
			}
		}
		if path := firstText(input, "file_path", "path", "filePath", "TargetFile", "AbsolutePath"); path != "" {
			attrs["file_path"] = path
		}
		row, err := w.row("PreToolUse", at, attrs)
		return []*types.As{row}, err
	}
	return nil, nil
}

// 「[Opus 5.5] toolOfACP names a tool call as Claude Code names its tools, by the kind ACP gives it, so a transcript」
// 「[Opus 5.5] reads it as it reads theirs; a kind with no such tool keeps the agent's own name for it.」
func toolOfACP(u acp.Update) string {
	switch u.ToolKind {
	case "execute":
		return "Bash"
	case "read":
		return "Read"
	case "edit":
		return "Edit"
	}
	if u.Name != "" {
		return u.Name
	}
	return u.Title
}

// 「[Opus 5.5] firstText is the first of keys whose value is a string.」
func firstText(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// 「[Opus 5.5] acpAnswered is the row of a turn's end: what the agent answered.」
func (w sessionWriter) acpAnswered(text string, at time.Time) ([]*types.As, error) {
	attrs := map[string]any{"last_assistant_message": text}
	if w.effort != "" {
		attrs["effort"] = map[string]any{"level": w.effort}
	}
	row, err := w.row("Stop", at, attrs)
	return []*types.As{row}, err
}
