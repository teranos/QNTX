package server

import (
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/server/a2a"
	"github.com/teranos/QNTX/server/auth"
	errors "github.com/teranos/sacred-error"
	"google.golang.org/protobuf/proto"
)

// A namespace agent's Tasks, each its own rows in the namespace: "Who may see
// its Tasks is REACH on its namespace" (ADR-048, §13.1), so they are written
// where the agent stands, as the agent, and read by whoever acts there.
//
// A Task is a subject, its id; each row is one state it entered, a predicate
// TaskState names, and the row that entered it carries the Message said then.
// Its status is its newest row.

// taskContext is the context every row of a Task is written in.
const taskContext = "a2a:task"

// a2aTask answers the Task operations at a namespace agent's tenant.
func (s *QNTXServer) a2aTask(r *http.Request, op string, namespace string, request proto.Message) (proto.Message, *a2a.Error) {
	answer, err := s.a2aTaskAnswer(r, op, namespace, request)
	if err != nil {
		return nil, a2a.Refused(err)
	}
	return answer, nil
}

func (s *QNTXServer) a2aTaskAnswer(r *http.Request, op string, namespace string, request proto.Message) (proto.Message, error) {
	switch op {
	case "SendMessage":
		return s.a2aSend(r, namespace, request)
	case "GetTask":
		asked, err := a2a.ReadGet(request)
		if err != nil {
			return nil, err
		}
		task, err := s.taskIn(namespace, asked.ID)
		if err != nil {
			return nil, err
		}
		return answered(task.Message(asked.History))
	case "ListTasks":
		asked, err := a2a.ReadList(request)
		if err != nil {
			return nil, err
		}
		tasks, err := s.tasksIn(namespace, ats.AttestationFilter{})
		if err != nil {
			return nil, err
		}
		return a2a.Listed(tasks, asked)
	case "CancelTask":
		id, err := a2a.ReadCancel(request)
		if err != nil {
			return nil, err
		}
		task, err := s.taskIn(namespace, id)
		if err != nil {
			return nil, err
		}
		if task.State.Terminal() {
			return nil, a2a.TaskNotCancelable("task " + id + " is " + string(task.State) + ", and a task that ended is not canceled")
		}
		// "a caller that leaves does not stop what it asked for halfway"
		// (agent_turn.go), and nothing else stops a turn either.
		return nil, a2a.TaskNotCancelable("task " + id + " is a turn the agent is in, and a turn is not stopped halfway")
	}
	return nil, a2a.UnsupportedBecause(op + " is not one this node does at a tenant yet")
}

// answered is a Task as an operation answers it, or why it is not one.
func answered(task proto.Message, err error) (proto.Message, error) {
	if err != nil {
		return nil, &a2a.Error{Name: "InvalidAgentResponseError", Message: "the task did not read as an A2A Task: " + err.Error()}
	}
	return task, nil
}

// a2aSend is SendMessage at a namespace agent's tenant: a Task made, written
// down before anything is said, and the turn it is.
func (s *QNTXServer) a2aSend(r *http.Request, namespace string, request proto.Message) (proto.Message, error) {
	send, err := a2a.ReadSend(request)
	if err != nil {
		return nil, err
	}
	held, err := s.a2aAgentAt(r, namespace)
	if err != nil {
		return nil, err
	}
	if err := s.a2aContext(held, send); err != nil {
		return nil, err
	}
	// "When a client includes a taskId in a Message, it MUST reference an
	// existing task" (§3.4.2). A Task here is one turn, and takes no second.
	if send.NamesTask {
		task, err := s.taskIn(namespace, send.TaskID)
		if err != nil {
			return nil, err
		}
		return nil, a2a.UnsupportedBecause("task " + task.ID + " is " + string(task.State) + ": a task here is one turn of the agent, and takes no further message")
	}
	admitted, gated := auth.AdmissionFrom(r.Context())
	if !gated {
		return nil, a2a.Unauthenticated("a task is written in " + namespace + " as its sender acts there, and this sender came through no gate")
	}
	store, err := s.held.Write(admitted, namespace)
	if err != nil {
		return nil, errors.Wrapf(err, "no %s to write the task in", namespace)
	}
	task := a2a.Task{ID: uuid.NewString(), State: a2a.Submitted, At: time.Now(), History: []map[string]any{send.Message}}
	if err := held.writeTask(store, task.ID, a2a.Submitted, task.At, map[string]any{"message": send.Message, "sent_by": spokenBy(r)}); err != nil {
		return nil, errors.Wrapf(err, "the task was not written down, so nothing was said to %s", held.agent.called)
	}

	begun, ended := s.a2aSay(r, held, send.Text)
	recorded := make(chan error, 1)
	go func() { recorded <- held.recordTurn(store, task.ID, begun, ended) }()
	if send.ReturnImmediately {
		go s.unrecorded(held, task.ID, recorded)
		return answered(task.Sent(send.History))
	}
	if err := <-recorded; err != nil {
		return nil, &a2a.Error{Name: "InvalidAgentResponseError", Message: "the turn of task " + task.ID + " was not written down whole: " + err.Error()}
	}
	read, err := s.taskIn(namespace, task.ID)
	if err != nil {
		return nil, err
	}
	return answered(read.Sent(send.History))
}

// unrecorded says a task whose turn ended without its end written down, which
// nobody waits on: it stays in the state it was last written in.
func (s *QNTXServer) unrecorded(held anAgentHeld, task string, recorded <-chan error) {
	if err := <-recorded; err != nil {
		s.logger.Errorw("a task's turn was not written down whole", "task", task, "agent", held.agent.did, "namespace", held.agent.namespace, "error", err)
	}
}

// recordTurn writes the turn of a task down as it goes: working once the
// agent heard it, in the session it continues, and how it ended.
func (held anAgentHeld) recordTurn(store ats.AttestationStore, task string, begun <-chan turnBegun, ended <-chan turnEnded) error {
	for {
		select {
		case heard := <-begun:
			if heard.err != nil {
				// The turn's own row was not written: the session says why
				// (agent_turn.go), and the task stays submitted until it ends.
				continue
			}
			if err := held.writeTask(store, task, a2a.Working, heard.told.Timestamp, map[string]any{"context_id": sessionOf(heard.told.Contexts)}); err != nil {
				return err
			}
		case end := <-ended:
			at := time.Now()
			if end.err != nil {
				return held.writeTask(store, task, a2a.Failed, at, map[string]any{"message": a2a.AgentSaid(uuid.NewString(), end.err.Error())})
			}
			state := a2a.Completed
			if end.said.GetIsError() {
				state = a2a.Failed
			}
			return held.writeTask(store, task, state, at, map[string]any{
				"message": a2a.AgentSaid(uuid.NewString(), end.said.GetAnswer()), "context_id": end.said.GetSession(),
			})
		}
	}
}

// writeTask writes one row of a task: the state it entered, as the agent.
func (held anAgentHeld) writeTask(store ats.AttestationStore, task string, state a2a.State, at time.Time, attrs map[string]any) error {
	id, err := identity.GenerateASUID("AS", task, string(state), taskContext)
	if err != nil {
		return errors.Wrapf(err, "no id for task %s entering %s", task, state)
	}
	row := &types.As{
		ID: id, Subjects: []string{task}, Predicates: []string{string(state)}, Contexts: []string{taskContext},
		Actors: []string{held.agent.did}, Timestamp: at, CreatedAt: at, Source: "a2a", Attributes: attrs,
	}
	if err := held.agent.signer.Sign(row); err != nil {
		return errors.Wrapf(err, "task %s entering %s was not signed", task, state)
	}
	return errors.Wrapf(store.CreateAttestation(row), "task %s entering %s was not written", task, state)
}

// taskIn is one task of the namespace, or TaskNotFoundError.
func (s *QNTXServer) taskIn(namespace, id string) (a2a.Task, error) {
	tasks, err := s.tasksIn(namespace, ats.AttestationFilter{Subjects: []string{id}})
	if err != nil {
		return a2a.Task{}, err
	}
	for _, task := range tasks {
		return task, nil
	}
	return a2a.Task{}, a2a.TaskNotFound(id)
}

// tasksIn is the tasks of the namespace the filter's subjects name, or every
// one when it names none.
func (s *QNTXServer) tasksIn(namespace string, named ats.AttestationFilter) ([]a2a.Task, error) {
	reading, err := s.held.Read(namespace)
	if err != nil {
		return nil, errors.Wrapf(err, "no %s to read its tasks from", namespace)
	}
	filter := ats.AttestationFilter{Subjects: named.Subjects, Contexts: []string{taskContext}, TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Limit: ats.EveryRow}
	rows, err := reading.GetAttestations(filter)
	if err != nil {
		return nil, errors.Wrapf(err, "the tasks of %s did not read", namespace)
	}
	slices.SortStableFunc(rows, func(a, b *types.As) int { return a.Timestamp.Compare(b.Timestamp) })
	byID := map[string]*a2a.Task{}
	var order []string
	for _, row := range rows {
		if len(row.Subjects) != 1 || len(row.Predicates) != 1 {
			return nil, errors.Newf("row %s of a task in %s names %d subjects and %d predicates, and a task's row names one of each", row.ID, namespace, len(row.Subjects), len(row.Predicates))
		}
		state := a2a.State(row.Predicates[0])
		if !slices.Contains([]a2a.State{a2a.Submitted, a2a.Working, a2a.Completed, a2a.Failed}, state) {
			return nil, errors.Newf("row %s of task %s in %s enters %s, which no task here enters", row.ID, row.Subjects[0], namespace, state)
		}
		task, seen := byID[row.Subjects[0]]
		if !seen {
			task = &a2a.Task{ID: row.Subjects[0], History: []map[string]any{}}
			byID[task.ID] = task
			order = append(order, task.ID)
		}
		task.State, task.At = state, row.Timestamp
		if context, said := row.Attributes["context_id"].(string); said {
			task.ContextID = context
		}
		if message, said := row.Attributes["message"].(map[string]any); said {
			task.History = append(task.History, message)
		}
	}
	tasks := make([]a2a.Task, 0, len(order))
	for _, id := range order {
		tasks = append(tasks, *byID[id])
	}
	return tasks, nil
}
