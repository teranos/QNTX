package server

import (
	"net/http"
	"slices"
	"strings"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/server/a2a"
	errors "github.com/teranos/sacred-error"
	"google.golang.org/protobuf/proto"
)

// A namespace agent's Tasks are its session: "Who may see its Tasks is REACH
// on its namespace" (ADR-048, §13.1), and its session is written there
// already, as the agent. Nothing more is written for a Task.
//
// A Task is one turn: what was said to the agent, its UserPromptSubmit, whose
// id is the Task's, and how the turn ended, the Stop or StopFailure after it
// in the session. A turn not ended is working. Every turn is a Task, however
// it was said.

// turnPredicates are the rows of a session a Task is read from.
var turnPredicates = []string{"UserPromptSubmit", "Stop", "StopFailure"}

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
		tasks, err := s.tasksIn(namespace)
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

// a2aSend is SendMessage at a namespace agent's tenant: what was sent is said
// to the agent, and the turn it begins is the Task.
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

	begun, ended := s.a2aSay(r, held, send.Text)
	// A turn refused before anything was said ends without beginning.
	var told *types.As
	select {
	case heard := <-begun:
		if heard.err != nil {
			return nil, errors.Wrapf(heard.err, "what was said to %s was not written down, so it is no Task, and the agent answers it all the same", held.agent.called)
		}
		told = heard.told
	case end := <-ended:
		return nil, end.err
	}
	if send.ReturnImmediately {
		task, err := taskBegun(told)
		if err != nil {
			return nil, err
		}
		return answered(task.Sent(send.History))
	}
	if end := <-ended; end.err != nil {
		return nil, end.err
	}
	read, err := s.taskIn(namespace, told.ID)
	if err != nil {
		return nil, err
	}
	return answered(read.Sent(send.History))
}

// taskBegun is the Task a turn begins as, working: what was said to the agent is
// the user's Message, whose id is the row's, since the row is what is kept of
// what was sent.
func taskBegun(told *types.As) (a2a.Task, error) {
	session, in := inSession(told)
	if !in {
		return a2a.Task{}, errors.Newf("row %s is in no session", told.ID)
	}
	prompt, err := textOfRow(told, "prompt")
	if err != nil {
		return a2a.Task{}, err
	}
	return a2a.Task{ID: told.ID, ContextID: session, State: a2a.Working, At: told.Timestamp, History: []map[string]any{a2a.UserSaid(told.ID, prompt)}}, nil
}

// inSession is the session a row is written in, and false when it names none.
func inSession(row *types.As) (string, bool) {
	for _, context := range row.Contexts {
		if session, in := strings.CutPrefix(context, "session:"); in {
			return session, true
		}
	}
	return "", false
}

// textOfRow is the text a session's row carries under key.
func textOfRow(row *types.As, key string) (string, error) {
	text, isText := row.Attributes[key].(string)
	if !isText {
		return "", errors.Newf("row %s, %s, carries no text as %s", row.ID, row.Predicates[0], key)
	}
	return text, nil
}

// taskIn is one task of the namespace, or TaskNotFoundError.
func (s *QNTXServer) taskIn(namespace, id string) (a2a.Task, error) {
	tasks, err := s.tasksIn(namespace)
	if err != nil {
		return a2a.Task{}, err
	}
	at := slices.IndexFunc(tasks, func(task a2a.Task) bool { return task.ID == id })
	if at < 0 {
		return a2a.Task{}, a2a.TaskNotFound(id)
	}
	return tasks[at], nil
}

// tasksIn is every turn of the agent's sessions in the namespace, read as a
// transcript reads them: each session's rows in the order they happened.
func (s *QNTXServer) tasksIn(namespace string) ([]a2a.Task, error) {
	reading, err := s.held.Read(namespace)
	if err != nil {
		return nil, errors.Wrapf(err, "no %s to read its tasks from", namespace)
	}
	rows, err := reading.GetAttestations(ats.AttestationFilter{
		Subjects: []string{agentSubject}, Predicates: turnPredicates,
		TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Limit: ats.EveryRow,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "the sessions of %s did not read", namespace)
	}
	slices.SortStableFunc(rows, func(a, b *types.As) int { return a.Timestamp.Compare(b.Timestamp) })
	var tasks []a2a.Task
	// open is the turn each session is in, by where it is in tasks.
	open := map[string]int{}
	for _, row := range rows {
		if len(row.Predicates) != 1 {
			return nil, errors.Newf("row %s of a session in %s names %d predicates, and a session's row names one", row.ID, namespace, len(row.Predicates))
		}
		session, in := inSession(row)
		if !in {
			return nil, errors.Newf("row %s of %s is in no session", row.ID, namespace)
		}
		switch row.Predicates[0] {
		case "UserPromptSubmit":
			task, err := taskBegun(row)
			if err != nil {
				return nil, err
			}
			open[session] = len(tasks)
			tasks = append(tasks, task)
		case "Stop", "StopFailure":
			at, turning := open[session]
			if !turning {
				// An end with no turn said before it in its session ends
				// nothing a Task was made of.
				continue
			}
			delete(open, session)
			text, err := textOfRow(row, "last_assistant_message")
			if err != nil {
				return nil, err
			}
			state := a2a.Completed
			if row.Predicates[0] == "StopFailure" {
				// Said as a transcript says it: which error, then what was
				// written of it, whole.
				why, err := textOfRow(row, "error")
				if err != nil {
					return nil, err
				}
				state, text = a2a.Failed, why+": "+text
			}
			task := &tasks[at]
			task.State, task.At = state, row.Timestamp
			task.History = append(task.History, a2a.AgentSaid(row.ID, text))
		}
	}
	return tasks, nil
}
