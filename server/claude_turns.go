package server

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/access"
	"github.com/teranos/QNTX/internal/claudecode"
	"github.com/teranos/errors"
)

// sessionWriter writes an agent's session down as the agent (ADR-048): the
// hook events a transcript is read from, each row its own DID's.
type sessionWriter struct {
	did     string
	session string
	// resumed is whether the session existed before this run.
	resumed bool
	// effort is the effort the node ran this turn at, said on how it ended as
	// a transcript reads it.
	effort string
}

// agentSubject is what an agent's rows are about: where it works.
const agentSubject = "qntx/root-agent"

// secretShapes is how the secrets an agent comes to hold start: GitHub's
// tokens, the node's own, and a Claude plan token. A run of body at least
// least long after the start is taken for the secret.
var secretShapes = []struct {
	starts string
	body   func(byte) bool
	least  int
}{
	{"ghs_", tokenByte, 20}, {"ghp_", tokenByte, 20}, {"gho_", tokenByte, 20},
	{"ghu_", tokenByte, 20}, {"ghr_", tokenByte, 20}, {"github_pat_", tokenByte, 20},
	{"sk-ant-", tokenByte, 20},
	{access.TokenPrefix, hexByte, 2 * access.TokenSeedBytes},
}

func tokenByte(c byte) bool {
	return hexByte(c) || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' || c == '-' || c == '.'
}

func hexByte(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// masked is text with every secret in it named by how it starts and not
// carried. A session is written down to be read, and a row is kept: what an
// agent was handed and put in a command does not belong in either.
func masked(text string) string {
	for _, shape := range secretShapes {
		from := 0
		for {
			at := strings.Index(text[from:], shape.starts)
			if at < 0 {
				break
			}
			body := from + at + len(shape.starts)
			end := body
			for end < len(text) && shape.body(text[end]) {
				end++
			}
			if end-body < shape.least {
				from = body
				continue
			}
			text = text[:body] + "…(masked)" + text[end:]
			from = body
		}
	}
	return text
}

func (w sessionWriter) row(predicate string, at time.Time, attrs map[string]any) (*types.As, error) {
	id, err := identity.GenerateASUID("AS", agentSubject, predicate, w.session)
	if err != nil {
		return nil, errors.Wrapf(err, "no id for the %s of session %s", predicate, w.session)
	}
	for name, value := range attrs {
		if said, isText := value.(string); isText {
			attrs[name] = masked(said)
		}
	}
	return &types.As{
		ID:         id,
		Subjects:   []string{agentSubject},
		Predicates: []string{predicate},
		Contexts:   []string{"session:" + w.session},
		Actors:     []string{w.did},
		Timestamp:  at,
		CreatedAt:  at,
		Source:     "agent",
		Attributes: attrs,
	}, nil
}

// told is what was said to the agent, and by whom.
func (w sessionWriter) told(says, by string, at time.Time) (*types.As, error) {
	return w.row("UserPromptSubmit", at, map[string]any{"prompt": says, "said_by": by})
}

// rowsOf is what one message of the stream says happened: the run starting,
// each tool reached for, and how the turn ended.
func (w sessionWriter) rowsOf(m claudecode.Message, at time.Time) ([]*types.As, error) {
	if stamped, err := time.Parse(time.RFC3339, m.Timestamp); err == nil {
		at = stamped
	}
	var rows []*types.As
	add := func(predicate string, attrs map[string]any) error {
		// One message can carry several tools, and they happened in the order
		// it carries them: each row is a moment after the one before.
		row, err := w.row(predicate, at.Add(time.Duration(len(rows))*time.Millisecond), attrs)
		if err != nil {
			return err
		}
		rows = append(rows, row)
		return nil
	}
	switch {
	case m.Type == "system" && m.Subtype == "init":
		source := "startup"
		if w.resumed {
			source = "resume"
		}
		if err := add("SessionStart", map[string]any{"source": source, "model": m.Model}); err != nil {
			return nil, err
		}
	case m.Type == "assistant":
		for _, block := range m.Blocks() {
			if block.Type != "tool_use" {
				continue
			}
			// A tool call carries what a transcript reads of one, and no more.
			var input struct {
				FilePath string `json:"file_path"`
				Command  string `json:"command"`
			}
			if len(block.Input) > 0 {
				if err := json.Unmarshal(block.Input, &input); err != nil {
					return nil, errors.Wrapf(err, "the input of %s did not read", block.Name)
				}
			}
			attrs := map[string]any{"tool_name": block.Name, "tool_use_id": block.ID}
			if input.FilePath != "" {
				attrs["file_path"] = input.FilePath
			}
			if input.Command != "" {
				attrs["command"] = input.Command
			}
			if err := add("PreToolUse", attrs); err != nil {
				return nil, err
			}
		}
	case m.Type == "result" && m.IsError:
		if err := add("StopFailure", map[string]any{"error": m.Subtype, "last_assistant_message": m.Result}); err != nil {
			return nil, err
		}
	case m.Type == "result":
		attrs := map[string]any{"last_assistant_message": m.Result}
		if w.effort != "" {
			attrs["effort"] = map[string]any{"level": w.effort}
		}
		if err := add("Stop", attrs); err != nil {
			return nil, err
		}
	}
	return rows, nil
}
