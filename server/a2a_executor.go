package server

import (
	"context"
	"encoding/json"
	"iter"
	"mime"
	"os"
	"path/filepath"
	"slices"
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
		// What is sent and given back as files lies in the turn's work, under
		// the Task, and goes once the Task ends; a turn the next node reads
		// on (settleLeftTask) keeps it.
		files := filepath.Join(ta.agent.home, "work", "a2a", string(id))
		in, out := filepath.Join(files, "in"), filepath.Join(files, "out")
		defer func() {
			if e.s.ctx.Err() == nil {
				_ = os.RemoveAll(files)
			}
		}()
		if err := os.MkdirAll(out, 0o700); err != nil {
			yield(nil, a2aRefusal(a2asdk.ErrInternalError, "where the turn writes its files was not made: "+err.Error()))
			return
		}
		says, err := saidIn(ec.Message, in, out)
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

		// A turn Claude Code still prints in is working, and is told so once
		// in each quarter of turnQuietMost it printed in: the turn is quiet
		// when Claude Code is, not when the Task has no news. What it says as
		// it goes is the Task's "said" artifact, chunk by chunk (§3.7), so a
		// stream shows the turn as it happens.
		printed := make(chan []string)
		told := time.NewTicker(turnQuietMost / 4)
		defer told.Stop()
		moved := false
		var said []*a2asdk.Part
		for {
			select {
			case stream := <-started:
				if !yield(a2asdk.NewStatusUpdateEvent(ec, a2asdk.TaskStateWorking, nil), nil) {
					stop()
					return
				}
				sacred.Go("the stream of a task's turn, read as it moves", func() {
					if _, _, err := claudecode.Tail(turn, stream, 0, func(line []byte, _ int64) {
						select {
						case printed <- textsIn(line):
						case <-turn.Done():
						}
					}); err != nil {
						e.s.logger.Warnw("a task's turn was not read as it moves", "agent", ta.did, "task", id, "stream", stream, "error", err)
					}
				})
			case texts := <-printed:
				moved = true
				for _, text := range texts {
					chunk := a2asdk.NewArtifactUpdateEvent(ec, saidArtifactID, textPart(masked(text)))
					chunk.Append = len(said) > 0
					chunk.Artifact.Name = saidArtifactID
					if !yield(chunk, nil) {
						stop()
						return
					}
					said = append(said, chunk.Artifact.Parts...)
				}
			case <-told.C:
				if !moved {
					continue
				}
				moved = false
				if !yield(a2asdk.NewStatusUpdateEvent(ec, a2asdk.TaskStateWorking, nil), nil) {
					stop()
					return
				}
			case <-ctx.Done():
				stop()
				return
			case answer := <-answered:
				e.ended(ec, ta, submitted, answer, said, e.filesOut(out, call.accepts), yield)
				return
			}
		}
	}
}

// saidArtifactID is the artifact of what a turn said as it moved.
const saidArtifactID = "said"

// textsIn is the text a line of Claude Code's stream says: an assistant
// message's text blocks, none for any other line.
func textsIn(line []byte) []string {
	var m claudecode.Message
	if json.Unmarshal(line, &m) != nil || m.Type != "assistant" {
		return nil
	}
	var texts []string
	for _, block := range m.Blocks() {
		if block.Type == "text" && block.Text != "" {
			texts = append(texts, block.Text)
		}
	}
	return texts
}

// ended is how the turn ended, as the events of the Task's end: its answer,
// and what it said as it moved when it said anything.
func (e turnExecutor) ended(ec *a2asrv.ExecutorContext, ta taskAgent, submitted *a2asdk.Task, answered sigil.Asked, moving []*a2asdk.Part, files []*a2asdk.Artifact, yield func(a2asdk.Event, error) bool) {
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
		if len(moving) > 0 {
			completed.Artifacts = append(completed.Artifacts, &a2asdk.Artifact{ID: saidArtifactID, Name: saidArtifactID, Parts: moving})
		}
		completed.Artifacts = append(completed.Artifacts, files...)
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

// saidIn is what a message says, part after part, as the turn reads it:
// text as it is; JSON data as its JSON (§4.1.6); a URL as the URL, which the
// node does not fetch, as "File references within A2A messages MUST be
// validated to prevent server-side request forgery" (§14.1.1); and a file's
// bytes written into in, named where it lies. out, when named, is where a file
// the turn writes is given back with its answer (§3.7, §6.7).
func saidIn(message *a2asdk.Message, in, out string) (string, error) {
	if message == nil {
		return "", a2aRefusal(a2asdk.ErrInvalidParams, "a SendMessageRequest carries a message")
	}
	says := make([]string, 0, len(message.Parts)+1)
	var files []string
	for i, part := range message.Parts {
		switch content := part.Content.(type) {
		case a2asdk.Text:
			says = append(says, string(content))
		case a2asdk.Data:
			data, err := json.Marshal(content.Value)
			if err != nil {
				return "", a2aRefusal(a2asdk.ErrInvalidParams, "part "+strconv.Itoa(i)+" is data that is no JSON: "+err.Error())
			}
			says = append(says, string(data))
		case a2asdk.URL:
			says = append(says, "A file is at "+string(content)+partNamed(part)+".")
		case a2asdk.Raw:
			name, err := fileIn(in, i, part, content)
			if err != nil {
				return "", err
			}
			files = append(files, filepath.Join(in, name)+partNamed(part))
		default:
			return "", a2aRefusal(a2asdk.ErrUnsupportedContentType, "part "+strconv.Itoa(i)+" carries nothing a turn reads")
		}
	}
	if len(files) > 0 {
		says = append(says, "Files sent with this: "+strings.Join(files, ", ")+".")
	}
	if out != "" {
		says = append(says, "A file you write into "+out+" is given back with your answer.")
	}
	return strings.Join(says, "\n"), nil
}

// partNamed is what a part says of the file it carries: its name and media type.
func partNamed(part *a2asdk.Part) string {
	var named []string
	if part.Filename != "" {
		named = append(named, part.Filename)
	}
	if part.MediaType != "" {
		named = append(named, part.MediaType)
	}
	if len(named) == 0 {
		return ""
	}
	return " (" + strings.Join(named, ", ") + ")"
}

// fileIn writes part i's bytes into in, through in so its name leaves it
// never, as the name it was sent with when that is a plain name, and part-i
// otherwise.
func fileIn(in string, i int, part *a2asdk.Part, raw a2asdk.Raw) (string, error) {
	if err := os.MkdirAll(in, 0o700); err != nil {
		return "", a2aRefusal(a2asdk.ErrInternalError, "the files of the message were not written: "+err.Error())
	}
	root, err := os.OpenRoot(in)
	if err != nil {
		return "", a2aRefusal(a2asdk.ErrInternalError, "the files of the message were not written: "+err.Error())
	}
	defer root.Close()
	name := filepath.Base(part.Filename)
	if part.Filename == "" || name != part.Filename || strings.HasPrefix(name, ".") {
		name = "part-" + strconv.Itoa(i)
		if extensions, _ := mime.ExtensionsByType(part.MediaType); len(extensions) > 0 {
			name += extensions[0]
		}
	}
	if _, err := root.Stat(name); err == nil {
		name = strconv.Itoa(i) + "-" + name
	}
	if err := root.WriteFile(name, raw, 0o600); err != nil {
		return "", a2aRefusal(a2asdk.ErrInternalError, "part "+strconv.Itoa(i)+" was not written: "+err.Error())
	}
	return name, nil
}

// filesOut is what a turn wrote into out, each an artifact of the Task (§3.7)
// as its bytes, name and media type (§6.7), the most answerFiles carries into a
// room, and only of a media type the agent gives and the client takes.
func (e turnExecutor) filesOut(out string, accepts []string) []*a2asdk.Artifact {
	var artifacts []*a2asdk.Artifact
	for _, file := range e.s.answerFiles(out) {
		kind, _, err := mime.ParseMediaType(file.GetType())
		if err != nil || !slices.Contains(turnModes, kind) || (len(accepts) > 0 && !slices.Contains(accepts, kind)) {
			e.s.logger.Warnw("a file a turn wrote is no media type its Task gives", "file", file.GetName(), "type", file.GetType())
			continue
		}
		part := a2asdk.NewRawPart(file.GetData())
		part.Filename, part.MediaType = file.GetName(), kind
		artifacts = append(artifacts, &a2asdk.Artifact{ID: a2asdk.ArtifactID("file-" + file.GetName()), Name: file.GetName(), Parts: a2asdk.ContentParts{part}})
	}
	return artifacts
}

// isTextPlain is whether a media type is text/plain, whatever its parameters
// and case.
func isTextPlain(mediaType string) bool {
	parsed, _, err := mime.ParseMediaType(mediaType)
	return err == nil && parsed == textPlain
}
