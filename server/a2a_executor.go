package server

import (
	"context"
	"iter"
	"mime"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/teranos/QNTX/internal/claudecode"
	"github.com/teranos/QNTX/internal/sacred"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/reach"
	"github.com/teranos/QNTX/server/sigil"
)

// A Task is one turn: one run of Claude Code to its result, asked of the
// agent's say over A2A. The turn is the node's, not the caller's: a caller that
// leaves does not stop it, and its Task is answered by its id after (§3.2.2).
// Canceling the Task does: a turn waiting to start never starts, and one
// running is interrupted.
type turnExecutor struct{ s *QNTXServer }

var (
	_ a2asrv.AgentExecutor         = turnExecutor{}
	_ a2asrv.AgentExecutionCleaner = turnExecutor{}
)

// saidInClaude is an agent's answer as Claude Code gave it: the Node Agent's
// and an element agent's, and a namespace agent's.
type saidInClaude interface{ GetClaude() *protocol.ClaudeSaid }

func (e turnExecutor) Execute(ctx context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2asdk.Event, error] {
	return func(yield func(a2asdk.Event, error) bool) {
		call, err := a2aCallFrom(ctx)
		if err != nil {
			yield(nil, err)
			return
		}
		ta, id := call.agent, ec.TaskID
		says, err := saidIn(ec.Message)
		if err != nil {
			yield(nil, err)
			return
		}
		// The message sent is the Task's history, naming the task and the
		// context it is in (§3.4.1, §3.4.2).
		sent := *ec.Message
		sent.TaskID, sent.ContextID = id, ec.ContextID
		submitted := a2asdk.NewSubmittedTask(ec, &sent)
		if ec.StoredTask == nil && !yield(submitted, nil) {
			return
		}

		// Its turn running is the Task working: "actively being processed by
		// the agent" (TaskState), in the stream it prints to. A Task canceled
		// before its turn ran is not working: its turn is stopped as it starts.
		// The turn starting and its Task ending are told under one lock, so
		// exactly one of them stops a turn that started as its Task ended.
		var (
			starting sync.Mutex
			stopped  bool
			running  string
		)
		started := make(chan string, 1)
		turn := context.WithValue(ctx, taskStartsKey{}, func(session, stream string) {
			starting.Lock()
			defer starting.Unlock()
			if stopped || ctx.Err() != nil {
				stopped = true
				if err := claudecode.Interrupt(ta.agent.home, filepath.Base(stream)); err != nil {
					e.s.logger.Errorw("a task ended before its turn started, and the turn was not stopped", "agent", ta.did, "task", id, "stream", stream, "error", err)
				}
				return
			}
			running = stream
			e.s.taskStreams.Store(string(id), filepath.Base(stream))
			started <- stream
		})
		// stop ends the turn as its Task ends: the one that started, or none
		// to start after.
		stop := func() {
			starting.Lock()
			stopped = true
			stream := running
			starting.Unlock()
			e.interrupt(ta, id, stream)
		}
		asked := map[string]any{"says": says}
		for name, value := range ta.asked {
			asked[name] = value
		}
		answered := make(chan sigil.Asked, 1)
		sacred.Go("a task of an agent the node hosts", func() {
			answered <- ta.say.asking(reach.OverA2A, e.s.gate, e.s.reachingOver, call.request.WithContext(turn)).Ask(turn, asked)
		})

		for {
			select {
			case <-started:
				if !yield(a2asdk.NewStatusUpdateEvent(ec, a2asdk.TaskStateWorking, nil), nil) {
					stop()
					return
				}
			case <-ctx.Done():
				stop()
				return
			case answer := <-answered:
				e.ended(ec, ta, submitted, answer, yield)
				return
			}
		}
	}
}

// ended is how the turn ended, as the events of the Task's end.
func (e turnExecutor) ended(ec *a2asrv.ExecutorContext, ta taskAgent, submitted *a2asdk.Task, answered sigil.Asked, yield func(a2asdk.Event, error) bool) {
	failed := func(why string) {
		yield(a2asdk.NewStatusUpdateEvent(ec, a2asdk.TaskStateFailed, failedWhy(ec.TaskID, ec.ContextID, why)), nil)
	}
	// The node stopping is not a followed turn ending: the next node reads the
	// turn on from its stream and settles the Task (settleLeftTask). A turn
	// that answered has no stream left, so its answer is written here. A turn
	// no node reads on ends with the node that ran it.
	if answered.Answer == nil && e.s.ctx.Err() != nil {
		if ta.followed {
			yield(nil, a2aRefusal(a2asdk.ErrInternalError, "the node stopped while the turn of task "+string(ec.TaskID)+" ran, and the node after it settles the task"))
			return
		}
		failed("the node stopped while its turn ran")
		return
	}
	switch {
	case answered.Rejected != nil:
		failed("the gate turned it away: " + answered.Rejected.Body)
		return
	case answered.Refusal != nil:
		failed(refusalSays(answered.Refusal))
		return
	}
	said, isSaid := answered.Answer.(saidInClaude)
	var claude *protocol.ClaudeSaid
	if isSaid {
		claude = said.GetClaude()
	}
	switch {
	case claude == nil:
		// A Task asks in Claude Code, so any other answer is not its.
		failed(ta.called + " did not answer in Claude Code, which a Task asks it in")
	case claude.GetIsError():
		failed(masked(claude.GetAnswer()))
	default:
		// "Results SHOULD BE returned using Artifacts associated with a Task"
		// (§3.7): the Task completed is one line, its answer on it.
		completed := *submitted
		now := time.Now()
		completed.Status = a2asdk.TaskStatus{State: a2asdk.TaskStateCompleted, Timestamp: &now}
		completed.Artifacts = []*a2asdk.Artifact{answerArtifact(masked(claude.GetAnswer()))}
		yield(&completed, nil)
	}
}

// interrupt stops the turn of task id printing to stream, when it started.
func (e turnExecutor) interrupt(ta taskAgent, id a2asdk.TaskID, stream string) {
	if stream == "" {
		return
	}
	if err := claudecode.Interrupt(ta.agent.home, filepath.Base(stream)); err != nil {
		e.s.logger.Errorw("a task was canceled, and its turn was not stopped", "agent", ta.did, "task", id, "stream", stream, "error", err)
	}
}

// Cancel ends the Task canceled by who asked; a turn this process runs stops
// as its execution's context ends, and one a process before it left running is
// interrupted here by its stream.
func (e turnExecutor) Cancel(ctx context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2asdk.Event, error] {
	return func(yield func(a2asdk.Event, error) bool) {
		call, err := a2aCallFrom(ctx)
		if err != nil {
			yield(nil, err)
			return
		}
		id := string(ec.TaskID)
		e.s.taskCancels.Store(id, spokenBy(call.request))
		if _, runs := e.s.taskStreams.Load(id); !runs {
			_, stream, _, _, err := e.s.heldTask(call.agent.taskLines, id)
			if err != nil {
				yield(nil, err)
				return
			}
			if stream != "" {
				if err := claudecode.Interrupt(call.agent.agent.home, stream); err != nil {
					yield(nil, a2aRefusal(a2asdk.ErrInternalError, "task "+id+" was not canceled: its turn was not stopped: "+err.Error()))
					return
				}
			}
		}
		yield(a2asdk.NewStatusUpdateEvent(ec, a2asdk.TaskStateCanceled, nil), nil)
	}
}

func (e turnExecutor) Cleanup(_ context.Context, ec *a2asrv.ExecutorContext, _ a2asdk.SendMessageResult, _ error) {
	if ec.Message == nil {
		e.s.taskCancels.Delete(string(ec.TaskID))
		return
	}
	e.s.taskStreams.Delete(string(ec.TaskID))
}

// saidIn is what a message's text parts say, one after another. A part that is
// not text, or text in another media type than text/plain, is refused: the
// agent takes text and nothing else (ContentTypeNotSupportedError).
func saidIn(message *a2asdk.Message) (string, error) {
	if message == nil {
		return "", a2aRefusal(a2asdk.ErrInvalidParams, "a SendMessageRequest carries a message")
	}
	texts := make([]string, 0, len(message.Parts))
	for i, part := range message.Parts {
		text, isText := part.Content.(a2asdk.Text)
		if !isText {
			return "", a2aRefusal(a2asdk.ErrUnsupportedContentType, "part "+strconv.Itoa(i)+" carries no text, and the agent takes "+textPlain+" alone")
		}
		if part.MediaType != "" && !isTextPlain(part.MediaType) {
			return "", a2aRefusal(a2asdk.ErrUnsupportedContentType, "part "+strconv.Itoa(i)+" is "+part.MediaType+", and the agent takes "+textPlain+" alone")
		}
		texts = append(texts, string(text))
	}
	return strings.Join(texts, "\n"), nil
}

// isTextPlain is whether a media type is text/plain, whatever its parameters
// and case.
func isTextPlain(mediaType string) bool {
	parsed, _, err := mime.ParseMediaType(mediaType)
	return err == nil && parsed == textPlain
}
