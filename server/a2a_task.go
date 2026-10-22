package server

import (
	"context"
	"net/http"
	"path/filepath"
	"slices"
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
		return nil, &a2a.Error{Name: "PushNotificationNotSupportedError", Message: "the node sends no push notifications (capabilities.pushNotifications is false)"}
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
	// Its user is who spoke, as the turn writes them down (said_by).
	if err := s.writeTaskLine(id, a2a.Submitted, spokenBy(r), time.Now(), map[string]any{
		"message": masked(string(message)), "context_id": contextID,
	}); err != nil {
		return nil, &a2a.Error{Name: "InvalidAgentResponseError", Message: err.Error()}
	}

	// The turn is the node's, not the caller's: a caller that leaves does not
	// stop it, and its Task is answered by its id after (§3.2.2).
	turn := context.WithoutCancel(r.Context())
	ended := make(chan error, 1)
	sacred.Go("a task of the Node Agent", func() {
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
	ctx = context.WithValue(ctx, taskStartsKey{}, func(session, stream string) {
		if err := s.writeTaskLine(id, a2a.Working, s.nodeAgent.did, time.Now(),
			map[string]any{"context_id": session, "stream": filepath.Base(stream)}, streamContext(stream)); err != nil {
			s.logger.Errorw("a task of the Node Agent is working and was not written down as working", "task", id, "stream", stream, "error", err)
		}
	})
	asked := say.asking(reach.OverA2A, s.gate, s.reachingOver, caller).Ask(ctx, map[string]any{"harness": agentLineClaude, "says": says})
	// The node stopping is not its turn ending: the next node reads the turn
	// on from its stream and settles the Task (settleLeftTask).
	if s.ctx.Err() != nil {
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
			ended["why"] = "the Node Agent answered as neither Claude Code nor Pi"
		case claude.GetIsError():
			ended["why"], ended["context_id"] = masked(claude.GetAnswer()), claude.GetSession()
		default:
			state, ended["answer"], ended["context_id"] = a2a.Completed, masked(claude.GetAnswer()), claude.GetSession()
		}
	}
	return s.writeTaskLine(id, state, s.nodeAgent.did, time.Now(), ended)
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
	reading, err := s.held.Read(auth.NamespaceSystem)
	if err != nil {
		return a2a.Task{}, false, errors.Wrapf(err, "failed to read task %s", id)
	}
	lines, err := reading.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(),
		Subjects: []string{taskLineSubject}, Contexts: []string{taskContext(id)}, Limit: ats.EveryRow})
	if err != nil {
		return a2a.Task{}, false, errors.Wrapf(err, "failed to read task %s", id)
	}
	if len(lines) == 0 {
		return a2a.Task{}, false, nil
	}
	slices.SortFunc(lines, func(a, b *types.As) int { return a.Timestamp.Compare(b.Timestamp) })
	task := a2a.Task{ID: id}
	for _, line := range lines {
		if len(line.Predicates) != 1 {
			return a2a.Task{}, false, errors.Newf("%s line %s of task %s names %d states, and a line is one", taskLineSubject, line.ID, id, len(line.Predicates))
		}
		task.State, task.At = line.Predicates[0], line.Timestamp
		if task.ContextID, err = oneText(line.Attributes, "context_id", taskLineSubject, line.ID, id); err != nil {
			return a2a.Task{}, false, err
		}
		switch task.State {
		case a2a.Working:
			// What it says is the session it runs in, read above.
		case a2a.Submitted:
			message, err := oneText(line.Attributes, "message", taskLineSubject, line.ID, id)
			if err != nil {
				return a2a.Task{}, false, err
			}
			task.History = append(task.History, []byte(message))
		case a2a.Completed:
			if task.Answer, err = oneText(line.Attributes, "answer", taskLineSubject, line.ID, id); err != nil {
				return a2a.Task{}, false, err
			}
		case a2a.Failed:
			if task.Why, err = oneText(line.Attributes, "why", taskLineSubject, line.ID, id); err != nil {
				return a2a.Task{}, false, err
			}
		default:
			return a2a.Task{}, false, errors.Newf("%s line %s of task %s says %s, which is no state a task here reaches", taskLineSubject, line.ID, id, task.State)
		}
	}
	return task, true, nil
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
		if err := s.writeTaskLine(id, state, agent.did, time.Now(), ended); err != nil {
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
		if err := s.writeTaskLine(id, a2a.Failed, agent.did, time.Now(), map[string]any{"context_id": contextID, "why": said}); err != nil {
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
