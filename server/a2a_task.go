package server

import (
	"context"
	"encoding/base64"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/claudecode"
	"github.com/teranos/QNTX/internal/sacred"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/a2a"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/reach"
	errors "github.com/teranos/sacred-error"
	"google.golang.org/protobuf/proto"
)

// The Node Agent's Tasks. "NODE is ROOT": the agent the node's own card
// describes, reached at no tenant, is the Node Agent (ADR-048).
//
// "A Task's messages have two roles, user and agent: the user is who sent it,
// the agent is the one doing it." (ADR-048)
//
// A Task is one turn: one run of Claude Code to its result. The Node Agent's
// one session is the context its Tasks are in: "All tasks and messages with
// the same contextId SHOULD be treated as part of the same conversational
// session" (§3.4.1).

// A Task line is a TASK line in system, where the Node Agent's session is
// written: one per state the Task reaches, the newest holding.
const taskLineSubject = "TASK"

// The Node Agent's say is what a SendMessage asks, over A2A.
const (
	nodeAgentSignumName = "agent"
	nodeAgentSay        = "say"
)

// sendToNodeAgent is SendMessage at no tenant (§3.1.1): a new Task of the Node
// Agent, one turn, asked of its say through the gate as any surface asks it.
func (s *QNTXServer) sendToNodeAgent(r *http.Request, request proto.Message) (proto.Message, *a2a.Error) {
	admitted, gated := auth.AdmissionFrom(r.Context())
	if !gated {
		return nil, a2a.Unauthenticated("a message to the Node Agent is written down with who sent it, and this one came through no gate")
	}
	if s.nodeAgent == nil {
		return nil, &a2a.Error{Name: "UnsupportedOperationError", Message: s.thereIsNoNodeAgent().GetSays()}
	}
	say, found := s.nodeAgentsSay()
	if !found {
		return nil, &a2a.Error{Name: "InvalidAgentResponseError", Message: "the " + nodeAgentSignumName + " signum has no " + nodeAgentSay + " to send the message to"}
	}
	// What the lines say of the say over A2A, before anything is written: the
	// gate asks again when the turn is asked.
	if reaching, anyone := s.reachingOver(reach.OverA2A, say); !offeredTo(admitted, gated, reaching, anyone) {
		return nil, a2a.NotFound("the Node Agent is no agent " + spokenBy(r) + " reaches over A2A")
	}

	sent, err := a2a.ReadSent(request)
	if err != nil {
		return nil, a2a.Invalid(err.Error())
	}
	if sent.Role != "ROLE_USER" {
		return nil, a2a.Invalid("a message sent to the Node Agent is the user's, and this one's role is " + sent.Role)
	}
	if sent.Push {
		return nil, a2a.PushNotificationNotSupported()
	}
	says, refused := sent.Said()
	if refused != nil {
		return nil, refused
	}
	// A turn takes nothing more once it is said: a message naming a task is
	// one no task here accepts (§3.1.1, §3.4.2).
	if sent.TaskID != "" {
		task, held, err := s.taskOf(sent.TaskID)
		if err != nil {
			return nil, &a2a.Error{Name: "InvalidAgentResponseError", Message: err.Error()}
		}
		if !held {
			return nil, a2a.TaskNotFound(sent.TaskID)
		}
		return nil, &a2a.Error{Name: "UnsupportedOperationError", Message: "task " + task.ID + " is " + task.State + ": a task of the Node Agent is one turn, which takes nothing more once it is said"}
	}

	in := s.nodeAgent.in(s.claudeHarness())
	session, kept, err := s.nodeAgent.sessionIn(in.file)
	if err != nil {
		return nil, &a2a.Error{Name: "InvalidAgentResponseError", Message: err.Error()}
	}
	// "If an agent cannot accept a client-provided contextId, it MUST reject
	// the request with an error and MUST NOT generate a new contextId" (§3.4.1).
	if sent.ContextID != "" && (!kept || sent.ContextID != session) {
		return nil, a2a.Invalid("the Node Agent is one session, and context " + sent.ContextID + " is not it")
	}
	// A session not yet kept is named by the turn that keeps it.
	contextID := ""
	if kept {
		contextID = session
	}

	id := uuid.NewString()
	message, err := sent.InTask(id, contextID)
	if err != nil {
		return nil, &a2a.Error{Name: "InvalidAgentResponseError", Message: err.Error()}
	}
	// The turn is the node's, not the caller's: a caller that leaves does not
	// stop it, and its Task is answered by its id after (§3.2.2). Canceling
	// the Task does: a turn waiting to start never starts.
	turn, stop := context.WithCancel(context.WithoutCancel(r.Context()))
	s.taskStops.Store(id, stop)
	// Its user is who spoke, as the turn writes them down (said_by).
	if err := s.writeTaskLine(id, a2a.Submitted, spokenBy(r), time.Now(), map[string]any{
		"message": masked(string(message)), "context_id": contextID,
	}); err != nil {
		s.taskStops.Delete(id)
		stop()
		return nil, &a2a.Error{Name: "InvalidAgentResponseError", Message: err.Error()}
	}

	ended := make(chan error, 1)
	sacred.Go("a task of the Node Agent", func() {
		defer func() {
			s.taskStops.Delete(id)
			stop()
		}()
		err := s.performTask(turn, r.WithContext(turn), say, id, contextID, says)
		// Returned immediately, nobody waits on it: this is its one trace.
		if err != nil {
			s.logger.Errorw("a task of the Node Agent ended and how it ended was not written", "task", id, "error", err)
		}
		ended <- err
	})
	if !sent.ReturnImmediately {
		select {
		case err := <-ended:
			if err != nil {
				return nil, &a2a.Error{Name: "InvalidAgentResponseError", Message: err.Error()}
			}
		case <-r.Context().Done():
			return nil, &a2a.Error{Message: "the caller left before task " + id + " ended; it is answered by its id"}
		}
	}
	return s.taskAnswered(id, func(task a2a.Task) (proto.Message, error) { return task.Sending(sent.History) })
}

// performTask is one turn of the Node Agent, asked of its say over A2A, and
// the line of how it ended.
func (s *QNTXServer) performTask(ctx context.Context, caller *http.Request, say heldBy, id, contextID, says string) error {
	// Its turn running is the Task working: "actively being processed by the
	// agent" (TaskState), in the session it runs in and the stream it prints to.
	// A Task canceled before its turn ran is not working: its turn is stopped
	// as it starts.
	ctx = context.WithValue(ctx, taskStartsKey{}, func(session, stream string) {
		s.taskEnds.Lock()
		defer s.taskEnds.Unlock()
		task, _, held, err := s.heldTask(id)
		switch {
		case err != nil:
			s.logger.Errorw("a task of the Node Agent started and what it is was not read", "task", id, "stream", stream, "error", err)
		case held && a2a.Terminal(task.State):
			if err := claudecode.Interrupt(s.nodeAgent.home, filepath.Base(stream)); err != nil {
				s.logger.Errorw("a task of the Node Agent ended before its turn started, and the turn was not stopped", "task", id, "state", task.State, "stream", stream, "error", err)
			}
		default:
			if err := s.writeTaskLine(id, a2a.Working, s.nodeAgent.did, time.Now(),
				map[string]any{"context_id": session, "stream": filepath.Base(stream)}, streamContext(stream)); err != nil {
				s.logger.Errorw("a task of the Node Agent is working and was not written down as working", "task", id, "stream", stream, "error", err)
			}
		}
	})
	asked := say.asking(reach.OverA2A, s.gate, s.reachingOver, caller).Ask(ctx, map[string]any{"harness": agentLineClaude, "says": says})
	// The node stopping is not its turn ending: the next node reads the turn
	// on from its stream and settles the Task (settleLeftTask). A turn that
	// answered has no stream left, so its answer is written here.
	if asked.Answer == nil && s.ctx.Err() != nil {
		return nil
	}
	state, ended := a2a.Failed, map[string]any{"context_id": contextID}
	switch {
	case asked.Rejected != nil:
		ended["why"] = "the gate turned it away: " + asked.Rejected.Body
	case asked.Refusal != nil:
		ended["why"] = refusalSays(asked.Refusal)
	default:
		said, isSaid := asked.Answer.(*protocol.NodeAgentSaid)
		claude := said.GetClaude()
		switch {
		case !isSaid || claude == nil:
			// A Task asks in Claude Code (above), so any other answer is not its.
			ended["why"] = "the Node Agent did not answer in Claude Code, which a Task asks it in"
		case claude.GetIsError():
			ended["why"], ended["context_id"] = masked(claude.GetAnswer()), claude.GetSession()
		default:
			state, ended["answer"], ended["context_id"] = a2a.Completed, masked(claude.GetAnswer()), claude.GetSession()
		}
	}
	return s.endTask(id, state, s.nodeAgent.did, ended)
}

// endTask writes that task id ended in state, unless it ended already: a Task
// ends once, by its turn, a restart or a cancel, whichever is first.
func (s *QNTXServer) endTask(id, state, by string, attributes map[string]any) error {
	s.taskEnds.Lock()
	defer s.taskEnds.Unlock()
	task, _, held, err := s.heldTask(id)
	if err != nil {
		return err
	}
	if held && a2a.Terminal(task.State) {
		return nil
	}
	return s.writeTaskLine(id, state, by, time.Now(), attributes)
}

// getNodeAgentsTask is GetTask at no tenant (§3.1.3): a Task of the Node
// Agent, by its id.
func (s *QNTXServer) getNodeAgentsTask(r *http.Request, request proto.Message) (proto.Message, *a2a.Error) {
	if _, gated := auth.AdmissionFrom(r.Context()); !gated {
		return nil, a2a.Unauthenticated("a task of the Node Agent is given to a caller the node admitted, and this one came through no gate")
	}
	if s.nodeAgent == nil {
		return nil, &a2a.Error{Name: "UnsupportedOperationError", Message: s.thereIsNoNodeAgent().GetSays()}
	}
	id, limit, err := a2a.Asked(request)
	if err != nil {
		return nil, a2a.Invalid(err.Error())
	}
	return s.taskAnswered(id, func(task a2a.Task) (proto.Message, error) {
		built, err := task.Response(limit)
		if err != nil {
			return nil, err
		}
		return built.Interface(), nil
	})
}

// listNodeAgentsTasks is ListTasks at no tenant (§3.1.4): the Node Agent's
// Tasks, the most recently updated first, a page at a time. Who reaches '/a2a/'
// sees each of them.
func (s *QNTXServer) listNodeAgentsTasks(r *http.Request, request proto.Message) (proto.Message, *a2a.Error) {
	if _, gated := auth.AdmissionFrom(r.Context()); !gated {
		return nil, a2a.Unauthenticated("the tasks of the Node Agent are given to a caller the node admitted, and this one came through no gate")
	}
	if s.nodeAgent == nil {
		return nil, &a2a.Error{Name: "UnsupportedOperationError", Message: s.thereIsNoNodeAgent().GetSays()}
	}
	listing, refused := a2a.ReadListing(request)
	if refused != nil {
		return nil, refused
	}
	after, refused := cursorOf(listing.Token)
	if refused != nil {
		return nil, refused
	}
	tasks, err := s.tasks()
	if err != nil {
		return nil, &a2a.Error{Name: "InvalidAgentResponseError", Message: err.Error()}
	}
	tasks = slices.DeleteFunc(tasks, func(task a2a.Task) bool {
		return (listing.ContextID != "" && task.ContextID != listing.ContextID) ||
			(listing.State != "" && task.State != listing.State) ||
			(listing.Since && task.At.Before(listing.After))
	})
	// "Tasks MUST be sorted by last update time in descending order", and by
	// id where two were updated at once, so a page ends at one place.
	slices.SortFunc(tasks, func(a, b a2a.Task) int {
		if at := b.At.Compare(a.At); at != 0 {
			return at
		}
		return strings.Compare(a.ID, b.ID)
	})
	from := 0
	if after != nil {
		from = len(tasks)
		if at := slices.IndexFunc(tasks, after.precedes); at >= 0 {
			from = at
		}
	}
	page := tasks[from:min(from+int(listing.Size), len(tasks))]
	next := ""
	if from+len(page) < len(tasks) {
		next = cursorAt(page[len(page)-1])
	}
	listed, err := listing.Listed(page, next, len(tasks))
	if err != nil {
		return nil, &a2a.Error{Name: "InvalidAgentResponseError", Message: err.Error()}
	}
	return listed, nil
}

// cursor is where a page of tasks ended: the task last on it.
type cursor struct {
	at time.Time
	id string
}

// precedes is whether task comes after the cursor in the order tasks are
// listed in.
func (c cursor) precedes(task a2a.Task) bool {
	return task.At.Before(c.at) || (task.At.Equal(c.at) && task.ID > c.id)
}

// cursorAt is the page token of a page ending at task.
func cursorAt(task a2a.Task) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(task.At.UnixNano(), 10) + ":" + task.ID))
}

// cursorOf is where the page token says the page before ended, nil for the
// first page.
func cursorOf(token string) (*cursor, *a2a.Error) {
	if token == "" {
		return nil, nil
	}
	said, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, a2a.Invalid("pageToken " + token + " is no page token this node gave: " + err.Error())
	}
	nanos, id, cut := strings.Cut(string(said), ":")
	at, err := strconv.ParseInt(nanos, 10, 64)
	if !cut || err != nil {
		return nil, a2a.Invalid("pageToken " + token + " is no page token this node gave")
	}
	return &cursor{at: time.Unix(0, at), id: id}, nil
}

// cancelNodeAgentsTask is CancelTask at no tenant (§3.1.5): the Task ends
// canceled, and its turn stops. "ROOT can do anything": who reaches '/a2a/'
// cancels any of them.
func (s *QNTXServer) cancelNodeAgentsTask(r *http.Request, request proto.Message) (proto.Message, *a2a.Error) {
	if _, gated := auth.AdmissionFrom(r.Context()); !gated {
		return nil, a2a.Unauthenticated("a task of the Node Agent is canceled by a caller the node admitted, and this one came through no gate")
	}
	if s.nodeAgent == nil {
		return nil, &a2a.Error{Name: "UnsupportedOperationError", Message: s.thereIsNoNodeAgent().GetSays()}
	}
	id, err := a2a.Named(request)
	if err != nil {
		return nil, a2a.Invalid(err.Error())
	}
	stream, refused := s.endCanceled(id, spokenBy(r))
	if refused != nil {
		return nil, refused
	}
	// A turn waiting to start never starts, and one running is interrupted.
	if stop, waiting := s.taskStops.Load(id); waiting {
		stop.(context.CancelFunc)()
	}
	if stream != "" {
		if err := claudecode.Interrupt(s.nodeAgent.home, stream); err != nil {
			return nil, &a2a.Error{Name: "InvalidAgentResponseError", Message: "task " + id + " is canceled, and its turn was not stopped: " + err.Error()}
		}
	}
	return s.taskAnswered(id, func(task a2a.Task) (proto.Message, error) {
		built, err := task.Response(a2a.HistoryLimit{})
		if err != nil {
			return nil, err
		}
		return built.Interface(), nil
	})
}

// endCanceled writes that task id ended canceled by by, and gives the stream
// its turn prints to when it is working.
func (s *QNTXServer) endCanceled(id, by string) (string, *a2a.Error) {
	s.taskEnds.Lock()
	defer s.taskEnds.Unlock()
	task, stream, held, err := s.heldTask(id)
	if err != nil {
		return "", &a2a.Error{Name: "InvalidAgentResponseError", Message: err.Error()}
	}
	if !held {
		return "", a2a.TaskNotFound(id)
	}
	if a2a.Terminal(task.State) {
		return "", a2a.TaskNotCancelable(id, task.State)
	}
	if err := s.writeTaskLine(id, a2a.Canceled, by, time.Now(), map[string]any{"context_id": task.ContextID}); err != nil {
		return "", &a2a.Error{Name: "InvalidAgentResponseError", Message: err.Error()}
	}
	return stream, nil
}

// taskAnswered is the task id names as answers gives it, or TaskNotFoundError.
func (s *QNTXServer) taskAnswered(id string, answers func(a2a.Task) (proto.Message, error)) (proto.Message, *a2a.Error) {
	task, held, err := s.taskOf(id)
	if err != nil {
		return nil, &a2a.Error{Name: "InvalidAgentResponseError", Message: err.Error()}
	}
	if !held {
		return nil, a2a.TaskNotFound(id)
	}
	answer, err := answers(task)
	if err != nil {
		return nil, &a2a.Error{Name: "InvalidAgentResponseError", Message: err.Error()}
	}
	return answer, nil
}

// nodeAgentsSay is the Node Agent's say, as the node serves it.
func (s *QNTXServer) nodeAgentsSay() (heldBy, bool) {
	for _, signum := range s.checkedSigna() {
		if signum.GetName() != nodeAgentSignumName {
			continue
		}
		for _, held := range signum.GetSigils() {
			if held.GetName() == nodeAgentSay {
				return heldBy{signum: nodeAgentSignumName, sigil: held, answer: signum.Answers[nodeAgentSay]}, true
			}
		}
	}
	return heldBy{}, false
}

// taskStartsKey is where a Task's turn finds what to tell once it runs.
type taskStartsKey struct{}

// taskStartsIn is what the turn ctx asks for tells once it runs, when ctx asks
// it for a Task.
func taskStartsIn(ctx context.Context) (func(session, stream string), bool) {
	starts, forATask := ctx.Value(taskStartsKey{}).(func(session, stream string))
	return starts, forATask
}

// writeTaskLine writes that task id reached state, as by: who sent it, for the
// state it was submitted in, and the agent for the states after. also is what
// else the line is in.
func (s *QNTXServer) writeTaskLine(id, state, by string, at time.Time, attributes map[string]any, also ...string) error {
	store, err := s.held.WriteWhatTheNodeKnowsOfItself()
	if err != nil {
		return errors.Wrapf(err, "no store to write task %s in", id)
	}
	line, err := identity.GenerateASUIDWithRetry("AS", taskLineSubject, state, id, store.AttestationExists)
	if err != nil {
		return errors.Wrapf(err, "failed to name the %s line of task %s", state, id)
	}
	return errors.Wrapf(store.CreateAttestation(&types.As{
		ID: line, Subjects: []string{taskLineSubject}, Predicates: []string{state}, Contexts: append([]string{taskContext(id)}, also...),
		Actors: []string{by}, Timestamp: at, CreatedAt: at, Source: "a2a", Attributes: attributes,
	}), "the %s line of task %s was not written", state, id)
}

// taskContext is what a task's lines are in.
func taskContext(id string) string { return taskContextPrefix + id }

const taskContextPrefix = "task:"

// streamContext is what the line of a Task whose turn prints to stream is in
// beside its task, so the turn is found by its stream after a restart.
func streamContext(stream string) string { return "stream:" + filepath.Base(stream) }

// taskOf is the task id names, as its lines say it. False is no task by that id.
func (s *QNTXServer) taskOf(id string) (a2a.Task, bool, error) {
	task, _, held, err := s.heldTask(id)
	return task, held, err
}

// heldTask is taskOf, with the stream its turn prints to while it works.
func (s *QNTXServer) heldTask(id string) (a2a.Task, string, bool, error) {
	reading, err := s.held.Read(auth.NamespaceSystem)
	if err != nil {
		return a2a.Task{}, "", false, errors.Wrapf(err, "failed to read task %s", id)
	}
	lines, err := reading.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(),
		Subjects: []string{taskLineSubject}, Contexts: []string{taskContext(id)}, Limit: ats.EveryRow})
	if err != nil {
		return a2a.Task{}, "", false, errors.Wrapf(err, "failed to read task %s", id)
	}
	if len(lines) == 0 {
		return a2a.Task{}, "", false, nil
	}
	task, stream, err := taskFrom(id, lines)
	return task, stream, err == nil, err
}

// tasks is every Task of the Node Agent, as its lines say each.
func (s *QNTXServer) tasks() ([]a2a.Task, error) {
	reading, err := s.held.Read(auth.NamespaceSystem)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the tasks of the Node Agent")
	}
	lines, err := reading.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(),
		Subjects: []string{taskLineSubject}, Limit: ats.EveryRow})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the tasks of the Node Agent")
	}
	of := map[string][]*types.As{}
	for _, line := range lines {
		id, found := taskIn(line)
		if !found {
			return nil, errors.Newf("%s line %s names no task", taskLineSubject, line.ID)
		}
		of[id] = append(of[id], line)
	}
	tasks := make([]a2a.Task, 0, len(of))
	for id, lines := range of {
		task, _, err := taskFrom(id, lines)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

// taskFrom is task id as lines, its lines, say it, and the stream its turn
// prints to while it works.
func taskFrom(id string, lines []*types.As) (a2a.Task, string, error) {
	slices.SortFunc(lines, func(a, b *types.As) int { return a.Timestamp.Compare(b.Timestamp) })
	task, stream := a2a.Task{ID: id}, ""
	var err error
	for _, line := range lines {
		if len(line.Predicates) != 1 {
			return a2a.Task{}, "", errors.Newf("%s line %s of task %s names %d states, and a line is one", taskLineSubject, line.ID, id, len(line.Predicates))
		}
		task.State, task.At = line.Predicates[0], line.Timestamp
		if task.ContextID, err = oneText(line.Attributes, "context_id", taskLineSubject, line.ID, id); err != nil {
			return a2a.Task{}, "", err
		}
		switch task.State {
		case a2a.Working:
			// What it says is the session it runs in, read above, and the
			// stream it prints to.
			if stream, err = oneText(line.Attributes, "stream", taskLineSubject, line.ID, id); err != nil {
				return a2a.Task{}, "", err
			}
		case a2a.Submitted:
			message, err := oneText(line.Attributes, "message", taskLineSubject, line.ID, id)
			if err != nil {
				return a2a.Task{}, "", err
			}
			task.History = append(task.History, []byte(message))
		case a2a.Completed:
			if task.Answer, err = oneText(line.Attributes, "answer", taskLineSubject, line.ID, id); err != nil {
				return a2a.Task{}, "", err
			}
		case a2a.Failed:
			if task.Why, err = oneText(line.Attributes, "why", taskLineSubject, line.ID, id); err != nil {
				return a2a.Task{}, "", err
			}
		case a2a.Canceled:
			// Who canceled it is the line's actor.
		default:
			return a2a.Task{}, "", errors.Newf("%s line %s of task %s says %s, which is no state a task here reaches", taskLineSubject, line.ID, id, task.State)
		}
	}
	if task.State != a2a.Working {
		stream = ""
	}
	return task, stream, nil
}

// settleLeftTask writes how the Task whose turn printed to stream ended, once
// the node read on the turn its last process left: what Claude Code answered,
// or why it did not. A turn no Task asked for settles none.
func (s *QNTXServer) settleLeftTask(agent *rootAgent, session, stream string, answer claudecode.Answer, unanswered error) error {
	reading, err := s.held.Read(auth.NamespaceSystem)
	if err != nil {
		return errors.Wrapf(err, "failed to read which task the turn printing to %s is", stream)
	}
	lines, err := reading.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(),
		Subjects: []string{taskLineSubject}, Contexts: []string{streamContext(stream)}, Limit: ats.EveryRow})
	if err != nil {
		return errors.Wrapf(err, "failed to read which task the turn printing to %s is", stream)
	}
	for _, line := range lines {
		id, found := taskIn(line)
		if !found {
			return errors.Newf("%s line %s names the stream %s and no task", taskLineSubject, line.ID, stream)
		}
		state, ended := a2a.Failed, map[string]any{"context_id": session}
		switch {
		case unanswered != nil:
			ended["why"] = masked(unanswered.Error())
		case answer.IsError:
			ended["why"] = masked(answer.Text)
		default:
			state, ended["answer"] = a2a.Completed, masked(answer.Text)
		}
		if err := s.endTask(id, state, agent.did, ended); err != nil {
			return err
		}
	}
	return nil
}

// settleTasksNoTurnLeft fails each Task from before since that no turn is left
// to end: submitted and never started, its turn waiting on the node that
// stopped; or working with no stream left to read how it ended. Nothing
// stands behind either state once the node that held it is gone.
func (s *QNTXServer) settleTasksNoTurnLeft(agent *rootAgent, since time.Time) error {
	reading, err := s.held.Read(auth.NamespaceSystem)
	if err != nil {
		return errors.Wrap(err, "failed to read the tasks the node's last process left")
	}
	lines, err := reading.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(),
		Subjects: []string{taskLineSubject}, Limit: ats.EveryRow})
	if err != nil {
		return errors.Wrap(err, "failed to read the tasks the node's last process left")
	}
	why := map[string]string{
		a2a.Submitted: "the node stopped before its turn started",
		a2a.Working:   "the node stopped while its turn ran, and no stream was left to read how it ended",
	}
	newest := map[string]*types.As{}
	for _, line := range lines {
		id, found := taskIn(line)
		if !found {
			return errors.Newf("%s line %s names no task", taskLineSubject, line.ID)
		}
		if held, seen := newest[id]; !seen || line.Timestamp.After(held.Timestamp) {
			newest[id] = line
		}
	}
	for id, line := range newest {
		if len(line.Predicates) != 1 {
			return errors.Newf("%s line %s of task %s names %d states, and a line is one", taskLineSubject, line.ID, id, len(line.Predicates))
		}
		said, left := why[line.Predicates[0]]
		if !left || !line.Timestamp.Before(since) {
			continue
		}
		contextID, err := oneText(line.Attributes, "context_id", taskLineSubject, line.ID, id)
		if err != nil {
			return err
		}
		if err := s.endTask(id, a2a.Failed, agent.did, map[string]any{"context_id": contextID, "why": said}); err != nil {
			return err
		}
	}
	return nil
}

// taskIn is the task a TASK line is of.
func taskIn(line *types.As) (string, bool) {
	for _, context := range line.Contexts {
		if id, isTask := strings.CutPrefix(context, taskContextPrefix); isTask {
			return id, true
		}
	}
	return "", false
}
