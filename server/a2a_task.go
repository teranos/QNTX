package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"time"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/claudecode"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/reach"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
)

// The Tasks of the agents the node hosts. "NODE is ROOT": the agent the node's
// own card describes, reached at no tenant, is the Node Agent (ADR-048). "Each
// agent it hosts is described by an Agent Card and reached by its tenant"
// (ADR-048): a namespace's agent at the namespace, and an element's agent
// (ADR-054) at element:<element>.
//
// "A Task's messages have two roles, user and agent: the user is who sent it,
// the agent is the one doing it." (ADR-048)
//
// A Task is one turn: one run of Claude Code to its result. An agent's one
// session is the context its Tasks are in: "All tasks and messages with the
// same contextId SHOULD be treated as part of the same conversational session"
// (§3.4.1).

// A Task line is a TASK line where its agent's session is written: one per
// state the Task reaches, the newest holding, in the agent's own context.
const taskLineSubject = "TASK"

// The Node Agent's say is what a SendMessage at no tenant asks, over A2A.
const (
	nodeAgentSignumName = "agent"
	nodeAgentSay        = "say"
)

// elementTenantPrefix is how a tenant names an element's agent: element:<element>.
const elementTenantPrefix = "element:"

// taskLines is where an agent's Task lines are: the namespace they are read
// from, the store they are written in, and the agent they are of.
type taskLines struct {
	// did is the agent's, as each of its lines names it.
	did       string
	namespace string
	write     func() (ats.AttestationStore, error)
}

// taskAgent is an agent A2A sends a message to: who it is, the say it is asked
// by, and where its Task lines are.
type taskAgent struct {
	taskLines
	agent *rootAgent
	// called is how an answer names it.
	called string
	say    heldBy
	// asked is what its say takes beside what is said.
	asked map[string]any
	// followed is whether the node after this one reads its turns on from
	// their streams (followLeftTurns): the Node Agent's and an element agent's,
	// which are under the Node Agent's home.
	followed bool
}

// systemTasks is where the Task lines of the agent did are when they are in
// system: the Node Agent's and an element agent's, what the node knows of itself.
func (s *QNTXServer) systemTasks(did string) taskLines {
	return taskLines{did: did, namespace: auth.NamespaceSystem, write: func() (ats.AttestationStore, error) {
		return s.held.WriteWhatTheNodeKnowsOfItself()
	}}
}

// refused is an A2A error of kind base, in the node's own words.
func a2aRefusal(base error, says string) error { return a2asdk.NewError(base, says) }

// taskAgentAt is the agent a request reaches: the Node Agent at no tenant, and
// at a tenant the element's or the namespace's agent it names.
func (s *QNTXServer) taskAgentAt(r *http.Request, tenant string) (taskAgent, error) {
	admitted, gated := auth.AdmissionFrom(r.Context())
	if !gated {
		return taskAgent{}, a2aRefusal(a2asdk.ErrUnauthenticated, "a task is written down with who asked for it, and this asking came through no gate")
	}
	if s.nodeAgent == nil && (tenant == "" || strings.HasPrefix(tenant, elementTenantPrefix)) {
		return taskAgent{}, a2aRefusal(a2asdk.ErrUnsupportedOperation, s.thereIsNoNodeAgent().GetSays())
	}
	var ta taskAgent
	var err error
	switch element, isElement := strings.CutPrefix(tenant, elementTenantPrefix); {
	case tenant == "":
		ta, err = s.nodeTaskAgent()
	case isElement:
		ta, err = s.elementTaskAgent(element)
	default:
		ta, err = s.namespaceTaskAgent(admitted, tenant)
	}
	if err != nil {
		return taskAgent{}, err
	}
	// What the lines say of the say over A2A, before anything is written or
	// read: the gate asks again when the turn is asked.
	if reaching, anyone := s.reachingOver(reach.OverA2A, ta.say); !offeredTo(admitted, gated, reaching, anyone) {
		return taskAgent{}, a2aRefusal(a2asdk.ErrTaskNotFound, ta.called+" is no agent "+spokenBy(r)+" reaches over A2A")
	}
	return ta, nil
}

// nodeTaskAgent is the Node Agent, its Tasks in system.
func (s *QNTXServer) nodeTaskAgent() (taskAgent, error) {
	say, found := s.sayOf(nodeAgentSignumName, nodeAgentSay)
	if !found {
		return taskAgent{}, a2aRefusal(a2asdk.ErrInvalidAgentResponse, "the "+nodeAgentSignumName+" signum has no "+nodeAgentSay+" to send the message to")
	}
	return taskAgent{
		taskLines: s.systemTasks(s.nodeAgent.did), agent: s.nodeAgent, called: "the Node Agent", say: say,
		asked: map[string]any{"harness": agentLineClaude}, followed: true,
	}, nil
}

// elementTaskAgent is the agent of element (ADR-054), its Tasks in system as
// its session is: "this mechanism is special, its just for root".
func (s *QNTXServer) elementTaskAgent(element string) (taskAgent, error) {
	if _, refusal := elementOf(sigil.Sent{"element": element}); refusal != nil {
		return taskAgent{}, a2aRefusal(a2asdk.ErrTaskNotFound, refusal.GetSays())
	}
	agent, refusal := s.holdElementAgent(element)
	if refusal != nil {
		return taskAgent{}, a2aRefusal(a2asdk.ErrInvalidAgentResponse, refusal.GetSays())
	}
	say, found := s.sayOf("elements", "say")
	if !found {
		return taskAgent{}, a2aRefusal(a2asdk.ErrInvalidAgentResponse, "the elements signum has no say to send the message to")
	}
	return taskAgent{
		taskLines: s.systemTasks(agent.did), agent: agent, called: agent.called, say: say,
		asked: map[string]any{"harness": agentLineClaude, "element": element}, followed: true,
	}, nil
}

// namespaceTaskAgent is the agent namespace opted into (ADR-048), its Tasks in
// the namespace where its session is written, through Write as the caller is
// admitted to act there.
func (s *QNTXServer) namespaceTaskAgent(admitted auth.Admission, namespace string) (taskAgent, error) {
	line, set, err := s.agentLineOf(namespace)
	if err != nil {
		return taskAgent{}, a2aRefusal(a2asdk.ErrInvalidAgentResponse, "the agent of "+namespace+" was not read: "+err.Error())
	}
	if !set {
		return taskAgent{}, a2aRefusal(a2asdk.ErrUnsupportedOperation, namespace+" opted into no agent in Claude Code")
	}
	agent, refusal := s.holdNamespaceAgent(namespace, line.spec.Model, line.setBy)
	if refusal != nil {
		return taskAgent{}, a2aRefusal(a2asdk.ErrInvalidAgentResponse, refusal.GetSays())
	}
	say, found := s.sayOf("agents", "say")
	if !found {
		return taskAgent{}, a2aRefusal(a2asdk.ErrInvalidAgentResponse, "the agents signum has no say to send the message to")
	}
	lines := taskLines{did: agent.did, namespace: namespace, write: func() (ats.AttestationStore, error) {
		return s.held.Write(admitted, namespace)
	}}
	return taskAgent{
		taskLines: lines, agent: agent, called: agent.called, say: say,
		asked: map[string]any{"harness": agentLineClaude, "namespace": namespace},
	}, nil
}

// contextOf is the context a Task of ta is in: the agent's one session. One not
// kept yet is kept now, before the Task is answered: "If an agent generates a
// new contextId, it MUST be included in the response" (§3.4.1). Its first turn
// then starts the session under that id.
func (s *QNTXServer) contextOf(ta taskAgent, asked string) (string, error) {
	// Two first messages at once are one session.
	s.taskEnds.Lock()
	defer s.taskEnds.Unlock()
	in := ta.agent.in(s.claudeHarness())
	session, kept, err := ta.agent.sessionIn(in.file)
	if err != nil {
		return "", a2aRefusal(a2asdk.ErrInvalidAgentResponse, err.Error())
	}
	// "If an agent cannot accept a client-provided contextId, it MUST reject
	// the request with an error and MUST NOT generate a new contextId" (§3.4.1).
	if asked != "" && (!kept || asked != session) {
		return "", a2aRefusal(a2asdk.ErrInvalidParams, ta.called+" is one session, and context "+asked+" is not it")
	}
	if !kept {
		if err := ta.agent.keepIn(in.file, session); err != nil {
			return "", a2aRefusal(a2asdk.ErrInvalidAgentResponse, "the session of "+ta.called+" was not kept: "+err.Error())
		}
	}
	return session, nil
}

// sayOf is the sigil a signum serves by name: the say an agent is asked by.
func (s *QNTXServer) sayOf(signumName, sigilName string) (heldBy, bool) {
	for _, signum := range s.checkedSigna() {
		if signum.GetName() != signumName {
			continue
		}
		for _, held := range signum.GetSigils() {
			if held.GetName() == sigilName {
				return heldBy{signum: signumName, sigil: held, answer: signum.Answers[sigilName]}, true
			}
		}
	}
	return heldBy{}, false
}

// taskStartsKey is where a Task's turn finds what to tell once it runs.
type taskStartsKey struct{}

// taskStartsIn is what the turn ctx asks for tells once it runs, when ctx asks
// it for a Task.
func taskStartsIn(ctx contextValuer) (func(session, stream string), bool) {
	starts, forATask := ctx.Value(taskStartsKey{}).(func(session, stream string))
	return starts, forATask
}

// contextValuer is what taskStartsIn reads its tell from: a context.
type contextValuer interface{ Value(key any) any }

// A Task line holds the Task as it stands, whole: the lf.a2a.v1.Task JSON the
// SDK reads and writes, its text masked. A line written before the node served
// A2A through the SDK holds what changed instead, and is read the same.
const taskSnapshot = "task"

// writeTask writes the line of task reaching its state, as by: who sent it,
// for the state it was submitted in, and the agent or who canceled it for the
// states after. The line is in the task and the agent; also is what else it is in.
func (s *QNTXServer) writeTask(lines taskLines, task *a2asdk.Task, by string, also ...string) error {
	store, err := lines.write()
	if err != nil {
		return errors.Wrapf(err, "no store to write task %s in, in %s", task.ID, lines.namespace)
	}
	state := string(task.Status.State)
	line, err := identity.GenerateASUIDWithRetry("AS", taskLineSubject, state, string(task.ID), store.AttestationExists)
	if err != nil {
		return errors.Wrapf(err, "failed to name the %s line of task %s", state, task.ID)
	}
	// The line is when it was written, which orders a Task's lines. The SDK
	// stamps a state it changes to at the millisecond, and the Task it
	// submits with no time at all: a line at the Task's own time sorts behind
	// the line before it when both fall in one millisecond.
	at := time.Now()
	held := maskedTask(task)
	if held.Status.Timestamp == nil {
		held.Status.Timestamp = &at
	}
	snapshot, err := json.Marshal(held)
	if err != nil {
		return errors.Wrapf(err, "task %s did not marshal to be written", task.ID)
	}
	return errors.Wrapf(store.CreateAttestation(&types.As{
		ID: line, Subjects: []string{taskLineSubject}, Predicates: []string{state},
		Contexts: append([]string{taskContext(string(task.ID)), agentContext(lines.did)}, also...),
		Actors:   []string{by}, Timestamp: at, CreatedAt: at, Source: "a2a",
		Attributes: map[string]any{"context_id": task.ContextID, taskSnapshot: string(snapshot)},
	}), "the %s line of task %s was not written", state, task.ID)
}

// maskedTask is task with every text it holds masked, as the turn's own lines
// are: what a person or the agent said can carry a secret.
func maskedTask(task *a2asdk.Task) *a2asdk.Task {
	copied := *task
	maskParts := func(parts a2asdk.ContentParts) a2asdk.ContentParts {
		out := make(a2asdk.ContentParts, 0, len(parts))
		for _, part := range parts {
			p := *part
			if text, isText := p.Content.(a2asdk.Text); isText {
				p.Content = a2asdk.Text(masked(string(text)))
			}
			out = append(out, &p)
		}
		return out
	}
	maskMessage := func(m *a2asdk.Message) *a2asdk.Message {
		if m == nil {
			return nil
		}
		held := *m
		held.Parts = maskParts(m.Parts)
		return &held
	}
	copied.History = nil
	for _, m := range task.History {
		copied.History = append(copied.History, maskMessage(m))
	}
	copied.Artifacts = nil
	for _, artifact := range task.Artifacts {
		held := *artifact
		held.Parts = maskParts(artifact.Parts)
		copied.Artifacts = append(copied.Artifacts, &held)
	}
	copied.Status.Message = maskMessage(task.Status.Message)
	return &copied
}

// taskContext is what a task's lines are in.
func taskContext(id string) string { return taskContextPrefix + id }

const taskContextPrefix = "task:"

// agentContext is what the Task lines of the agent did are in.
func agentContext(did string) string { return agentContextPrefix + did }

const agentContextPrefix = "agent:"

// streamContext is what the line of a Task whose turn prints to stream is in
// beside its task, so the turn is found by its stream after a restart.
func streamContext(stream string) string { return streamContextPrefix + filepath.Base(stream) }

const streamContextPrefix = "stream:"

// agentOfLine is the agent a Task line is of. A line in no agent's context is
// the Node Agent's: written before an agent at a tenant had Tasks of its own.
func (s *QNTXServer) agentOfLine(line *types.As) string {
	for _, context := range line.Contexts {
		if did, isAgent := strings.CutPrefix(context, agentContextPrefix); isAgent {
			return did
		}
	}
	if s.nodeAgent == nil {
		return ""
	}
	return s.nodeAgent.did
}

// heldTask is the task id names, as its lines say it, the stream its turn
// prints to while it works, and how many lines it has: its version. False is
// no task of the agent by that id.
func (s *QNTXServer) heldTask(lines taskLines, id string) (*a2asdk.Task, string, int, bool, error) {
	written, err := s.taskLinesIn(lines, []string{taskContext(id)})
	if err != nil {
		return nil, "", 0, false, errors.Wrapf(err, "failed to read task %s", id)
	}
	if len(written) == 0 {
		return nil, "", 0, false, nil
	}
	task, stream, err := taskFrom(id, written)
	return task, stream, len(written), err == nil, err
}

// taskLinesIn is the agent's Task lines in contexts, or all of them for none.
func (s *QNTXServer) taskLinesIn(lines taskLines, contexts []string) ([]*types.As, error) {
	reading, err := s.held.Read(lines.namespace)
	if err != nil {
		return nil, err
	}
	written, err := reading.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(),
		Subjects: []string{taskLineSubject}, Contexts: contexts, Limit: ats.EveryRow})
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(written, func(line *types.As) bool { return s.agentOfLine(line) != lines.did }), nil
}

// tasks is every Task of the agent, as its lines say each.
func (s *QNTXServer) tasks(lines taskLines) ([]*a2asdk.Task, error) {
	written, err := s.taskLinesIn(lines, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read the tasks of %s", lines.did)
	}
	of := map[string][]*types.As{}
	for _, line := range written {
		id, found := taskIn(line)
		if !found {
			return nil, errors.Newf("%s line %s names no task", taskLineSubject, line.ID)
		}
		of[id] = append(of[id], line)
	}
	tasks := make([]*a2asdk.Task, 0, len(of))
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
// prints to while it works. A line holding the Task whole is the Task from
// then on; a line from before holds what changed, and is laid on the Task.
func taskFrom(id string, lines []*types.As) (*a2asdk.Task, string, error) {
	slices.SortFunc(lines, func(a, b *types.As) int { return a.Timestamp.Compare(b.Timestamp) })
	task, stream := &a2asdk.Task{ID: a2asdk.TaskID(id)}, ""
	for _, line := range lines {
		if len(line.Predicates) != 1 {
			return nil, "", errors.Newf("%s line %s of task %s names %d states, and a line is one", taskLineSubject, line.ID, id, len(line.Predicates))
		}
		state := a2asdk.TaskState(line.Predicates[0])
		for _, context := range line.Contexts {
			if base, isStream := strings.CutPrefix(context, streamContextPrefix); isStream {
				stream = base
			}
		}
		if snapshot, holds := line.Attributes[taskSnapshot].(string); holds {
			whole := &a2asdk.Task{}
			if err := json.Unmarshal([]byte(snapshot), whole); err != nil {
				return nil, "", errors.Wrapf(err, "%s line %s of task %s holds no task", taskLineSubject, line.ID, id)
			}
			task = whole
			continue
		}
		if err := laidOn(task, state, line, id); err != nil {
			return nil, "", err
		}
	}
	if task.Status.State != a2asdk.TaskStateWorking {
		stream = ""
	}
	return task, stream, nil
}

// laidOn lays a line written before the SDK on task: the state it reached,
// and what it said of it.
func laidOn(task *a2asdk.Task, state a2asdk.TaskState, line *types.As, id string) error {
	at := line.Timestamp
	task.Status = a2asdk.TaskStatus{State: state, Timestamp: &at}
	contextID, err := oneText(line.Attributes, "context_id", taskLineSubject, line.ID, id)
	if err != nil {
		return err
	}
	task.ContextID = contextID
	switch state {
	case a2asdk.TaskStateWorking, a2asdk.TaskStateCanceled:
		// What it says is the session it runs in, read above; who canceled it
		// is the line's actor.
	case a2asdk.TaskStateSubmitted:
		said, err := oneText(line.Attributes, "message", taskLineSubject, line.ID, id)
		if err != nil {
			return err
		}
		message := &a2asdk.Message{}
		if err := json.Unmarshal([]byte(said), message); err != nil {
			return errors.Wrapf(err, "%s line %s of task %s holds no message", taskLineSubject, line.ID, id)
		}
		task.History = append(task.History, message)
	case a2asdk.TaskStateCompleted:
		answer, err := oneText(line.Attributes, "answer", taskLineSubject, line.ID, id)
		if err != nil {
			return err
		}
		task.Artifacts = []*a2asdk.Artifact{answerArtifact(answer)}
	case a2asdk.TaskStateFailed:
		why, err := oneText(line.Attributes, "why", taskLineSubject, line.ID, id)
		if err != nil {
			return err
		}
		task.Status.Message = failedWhy(task.ID, task.ContextID, why)
	default:
		return errors.Newf("%s line %s of task %s says %s, which is no state a task here reaches", taskLineSubject, line.ID, id, state)
	}
	return nil
}

// answerArtifact is what the agent gave, as the Task's one artifact: "Results SHOULD
// BE returned using Artifacts associated with a Task" (§3.7).
func answerArtifact(answer string) *a2asdk.Artifact {
	return &a2asdk.Artifact{ID: "answer", Name: "answer", Parts: a2asdk.ContentParts{textPart(answer)}}
}

// failedWhy is why task failed, as "A message associated with the status": the
// agent's, saying why.
func failedWhy(id a2asdk.TaskID, contextID, why string) *a2asdk.Message {
	return &a2asdk.Message{
		ID: string(id) + "-failed", ContextID: contextID, TaskID: id, Role: a2asdk.MessageRoleAgent,
		Parts: a2asdk.ContentParts{textPart(why)},
	}
}

// textPart is text, as text/plain.
func textPart(text string) *a2asdk.Part {
	part := a2asdk.NewTextPart(text)
	part.MediaType = textPlain
	return part
}

// textPlain is the media type the agents answer in.
const textPlain = "text/plain"

// turnModes are the media types a turn takes and gives (§4.4.5): text, JSON,
// and the files Claude Code reads, a PDF and an image among them. Any other is
// refused, as "Agents SHOULD sanitize or validate file content types and
// reject unexpected media types" (§13.4).
var turnModes = []string{
	textPlain, "text/markdown", "text/csv", "application/json",
	"application/pdf", "image/png", "image/jpeg", "image/gif", "image/webp",
}

// endTask writes that task id ended in state, as by, unless it ended already:
// a Task ends once, by its turn, a restart or a cancel, whichever is first.
// answer is what it gave when it completed, why why it did not.
func (s *QNTXServer) endTask(lines taskLines, id string, state a2asdk.TaskState, by, session, answer, why string) error {
	s.taskEnds.Lock()
	defer s.taskEnds.Unlock()
	task, _, _, held, err := s.heldTask(lines, id)
	if err != nil {
		return err
	}
	if !held {
		return errors.Newf("task %s has no lines to end", id)
	}
	if task.Status.State.Terminal() {
		return nil
	}
	now := time.Now()
	task.Status = a2asdk.TaskStatus{State: state, Timestamp: &now}
	if session != "" {
		task.ContextID = session
	}
	switch state {
	case a2asdk.TaskStateCompleted:
		task.Artifacts = []*a2asdk.Artifact{answerArtifact(answer)}
	case a2asdk.TaskStateFailed:
		task.Status.Message = failedWhy(task.ID, task.ContextID, why)
	}
	if err := s.writeTask(lines, task, by); err != nil {
		return err
	}
	// "When a task update occurs, the agent sends an HTTP POST request to the
	// configured webhook URL" (§4.3.3): no execution is left to send it.
	s.notifySettled(lines, task)
	return nil
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
		state, gave, why := a2asdk.TaskStateFailed, "", ""
		switch {
		case unanswered != nil:
			why = masked(unanswered.Error())
		case answer.IsError:
			why = masked(answer.Text)
		default:
			state, gave = a2asdk.TaskStateCompleted, masked(answer.Text)
		}
		did := s.agentOfLine(line)
		if err := s.endTask(s.systemTasks(did), id, state, did, session, gave, why); err != nil {
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
	why := map[a2asdk.TaskState]string{
		a2asdk.TaskStateSubmitted: "the node stopped before its turn started",
		a2asdk.TaskStateWorking:   "the node stopped while its turn ran, and no stream was left to read how it ended",
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
		said, left := why[a2asdk.TaskState(line.Predicates[0])]
		if !left || !line.Timestamp.Before(since) {
			continue
		}
		did := s.agentOfLine(line)
		if err := s.endTask(s.systemTasks(did), id, a2asdk.TaskStateFailed, did, "", "", said); err != nil {
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
