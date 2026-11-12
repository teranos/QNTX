// Package claudeharness is Claude Code as a harness plugin runs it (ADR-059).
//
// "Harness is a plugin that speaks grpc A2A"
// "I see multiple harnesses existing side by side"
//
// Core says a turn to the plugin over A2A; the plugin runs it in the Claude
// Code it holds, prints each message back as it arrives, and ends the Task
// with Claude Code's result.
package claudeharness

import (
	"context"
	"iter"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/teranos/QNTX/internal/claudecode"
	"github.com/teranos/QNTX/plugin/harness"
	errors "github.com/teranos/sacred-error"
)

// Run is how one turn is run in Claude Code, as core sends it: every field of
// claudecode.Said that crosses the wire. Binary is the plugin's own to name,
// Says the message's text, and Started is answered as a record (Started).
type Run struct {
	Home    string
	Session string
	Resumes bool
	Model   string
	Effort  string
	// Token is the Claude plan token, handed to the process and offered to no
	// model (ADR-051): it is in how the turn is run, and the Task keeps none of it.
	Token  string
	System string
	MCP    []claudecode.MCPServer
	Mode   string
	Allow  []string
	Env    []string
	Apart  bool
}

// Record is one record Claude Code prints while a turn runs.
type Record = claudecode.Message

// Answer is what Claude Code says of a run that ended.
type Answer = claudecode.Answer

// The record the plugin prints once the turn's process runs, in place of
// claudecode.Said.Started: Type and Subtype name it, and Result is the stream
// the turn prints to. It is the harness's word, not Claude Code's.
const (
	StartedType    = "qntx"
	StartedSubtype = "started"
)

// Started is the record the plugin prints for stream.
func Started(stream string) Record {
	return Record{Type: StartedType, Subtype: StartedSubtype, Result: stream}
}

// IsStarted reports whether record is the one Started prints, and the stream it names.
func IsStarted(record Record) (string, bool) {
	if record.Type != StartedType || record.Subtype != StartedSubtype {
		return "", false
	}
	return record.Result, true
}

// said is run as Claude Code is told it.
func (r Run) said(binary, says string, started func(string)) claudecode.Said {
	return claudecode.Said{
		Binary: binary, Home: r.Home, Session: r.Session, Resumes: r.Resumes, Says: says,
		Model: r.Model, Effort: r.Effort, Token: r.Token, System: r.System, MCP: r.MCP,
		Mode: r.Mode, Allow: r.Allow, Env: r.Env, Apart: r.Apart, Started: started,
	}
}

// Executor runs each turn in the Claude Code that Binary names.
type Executor struct {
	// Binary is where the plugin's Claude Code is, or why it has none.
	Binary func(ctx context.Context) (string, error)

	running harness.Running
}

var _ a2asrv.AgentExecutor = (*Executor)(nil)

// Handler is what a Claude Code harness plugin hands back as its Harness.
func Handler(binary func(ctx context.Context) (string, error)) a2asrv.RequestHandler {
	return a2asrv.NewHandler(&Executor{Binary: binary}, a2asrv.WithTaskStore(harness.Turns(harness.TurnsKept)))
}

func (e *Executor) Execute(ctx context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2asdk.Event, error] {
	return func(yield func(a2asdk.Event, error) bool) {
		var run Run
		says, err := harness.SaidIn(ec.Message, &run)
		if err != nil {
			yield(nil, errors.WithSecondaryError(a2asdk.ErrInvalidParams, err))
			return
		}
		binary, err := e.Binary(ctx)
		if err != nil {
			yield(nil, errors.Wrap(err, "this plugin has no Claude Code to run"))
			return
		}
		// How the turn is run carries what is handed to the process and
		// offered to no model (ADR-051): the Task keeps none of it.
		// a2a-go runs a turn apart from the call that asked for it, so a
		// caller that left does not end ctx: the run is stopped by Cancel,
		// which core asks for when it gives a turn up, and by the plugin's
		// server when the caller of the stream leaves. Both are in place
		// before the caller is told which Task this is. Unless the turn runs
		// apart: then it outlives the caller, as it outlives the node (ADR-048).
		running, stop := context.WithCancel(ctx)
		defer stop()
		if !run.Apart {
			defer e.running.Began(ec.TaskID, stop)()
			harness.StoppedWhenTheCallerLeaves(ctx, stop)
		}
		submitted := a2asdk.NewSubmittedTask(ec, a2asdk.NewMessage(a2asdk.MessageRoleUser, a2asdk.NewTextPart(says)))
		if ec.StoredTask == nil && !yield(submitted, nil) {
			return
		}
		if running.Err() != nil {
			return
		}
		listening := true
		print := func(record Record) {
			if !listening {
				return
			}
			printed, err := harness.Printed(ec, record)
			if err == nil && !yield(printed, nil) {
				listening = false
				if !run.Apart {
					stop()
				}
			}
		}
		answer, err := run.said(binary, says, func(stream string) { print(Started(stream)) }).Run(running, print)
		if !listening {
			return
		}
		if err != nil {
			yield(a2asdk.NewStatusUpdateEvent(ec, a2asdk.TaskStateFailed,
				a2asdk.NewMessage(a2asdk.MessageRoleAgent, a2asdk.NewTextPart(err.Error()))), nil)
			return
		}
		completed, err := harness.Answered(submitted, answer.Text, answer)
		if err != nil {
			yield(nil, err)
			return
		}
		yield(completed, nil)
	}
}

// Cancel stops the Task's run, one that runs apart aside, and ends the Task
// canceled.
func (e *Executor) Cancel(_ context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2asdk.Event, error] {
	return func(yield func(a2asdk.Event, error) bool) {
		e.running.Stop(ec.TaskID)
		yield(a2asdk.NewStatusUpdateEvent(ec, a2asdk.TaskStateCanceled, nil), nil)
	}
}
