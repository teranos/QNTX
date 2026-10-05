package server

import (
	"encoding/json"
	"time"

	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
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

func (w sessionWriter) row(predicate string, at time.Time, attrs map[string]any) (*types.As, error) {
	id, err := identity.GenerateASUID("AS", agentSubject, predicate, w.session)
	if err != nil {
		return nil, errors.Wrapf(err, "no id for the %s of session %s", predicate, w.session)
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
