package server

import (
	"context"
	"encoding/base64"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	errors "github.com/teranos/sacred-error"
)

// The SDK keeps the Tasks of an agent the node hosts on its TASK lines: each
// line holds the Task as it stands, and how many lines a Task has is its
// version.

// a2aCallKey holds the agent an A2A request reaches and the request itself,
// for what the SDK calls with the request's context.
type a2aCallKey struct{}

type a2aCall struct {
	agent   taskAgent
	tenant  string
	request *http.Request
}

func withA2ACall(ctx context.Context, call a2aCall) context.Context {
	return context.WithValue(ctx, a2aCallKey{}, call)
}

func a2aCallFrom(ctx context.Context) (a2aCall, error) {
	call, held := ctx.Value(a2aCallKey{}).(a2aCall)
	if !held {
		return a2aCall{}, a2aRefusal(a2asdk.ErrInternalError, "an A2A call reached the node's tasks with no agent it reaches")
	}
	return call, nil
}

// tasksOnLines is the SDK's taskstore.Store over the TASK lines of the agent
// each call reaches.
type tasksOnLines struct{ s *QNTXServer }

var _ taskstore.Store = tasksOnLines{}

func (t tasksOnLines) Create(ctx context.Context, task *a2asdk.Task) (taskstore.TaskVersion, error) {
	call, err := a2aCallFrom(ctx)
	if err != nil {
		return taskstore.TaskVersionMissing, err
	}
	t.s.taskEnds.Lock()
	defer t.s.taskEnds.Unlock()
	_, _, _, held, err := t.s.heldTask(call.agent.taskLines, string(task.ID))
	if err != nil {
		return taskstore.TaskVersionMissing, err
	}
	if held {
		return taskstore.TaskVersionMissing, taskstore.ErrTaskAlreadyExists
	}
	// Its user is who spoke, as the turn writes them down (said_by).
	if err := t.s.writeTask(call.agent.taskLines, task, spokenBy(call.request)); err != nil {
		return taskstore.TaskVersionMissing, err
	}
	return 1, nil
}

func (t tasksOnLines) Update(ctx context.Context, update *taskstore.UpdateRequest) (taskstore.TaskVersion, error) {
	call, err := a2aCallFrom(ctx)
	if err != nil {
		return taskstore.TaskVersionMissing, err
	}
	ta, task := call.agent, update.Task
	id := string(task.ID)
	t.s.taskEnds.Lock()
	defer t.s.taskEnds.Unlock()
	_, _, version, held, err := t.s.heldTask(ta.taskLines, id)
	if err != nil {
		return taskstore.TaskVersionMissing, err
	}
	if !held {
		return taskstore.TaskVersionMissing, a2asdk.ErrTaskNotFound
	}
	if update.PrevVersion != taskstore.TaskVersionMissing && taskstore.TaskVersion(version) != update.PrevVersion {
		return taskstore.TaskVersionMissing, taskstore.ErrConcurrentModification
	}
	// The node stopping is not a followed turn ending: the next node reads the
	// turn on from its stream and settles the Task (settleLeftTask). A failure
	// the agent did not say is the SDK's for the stopping, and is not written.
	if ta.followed && t.s.ctx.Err() != nil && task.Status.State == a2asdk.TaskStateFailed &&
		(task.Status.Message == nil || task.Status.Message.ID != id+"-failed") {
		return taskstore.TaskVersionMissing, errors.Newf("the node is stopping, and the node after it settles task %s from its turn's stream", id)
	}
	// A chunk of what a turn says is streamed, not written: the Task's next
	// line holds it, and its last line holds all of it.
	if _, chunk := update.Event.(*a2asdk.TaskArtifactUpdateEvent); chunk {
		return taskstore.TaskVersion(version), nil
	}
	by, also := ta.did, []string(nil)
	switch event := update.Event.(type) {
	case *a2asdk.Message:
		by = spokenBy(call.request)
	case *a2asdk.TaskStatusUpdateEvent:
		switch event.Status.State {
		case a2asdk.TaskStateCanceled:
			if canceler, canceled := t.s.taskCancels.Load(id); canceled {
				by = canceler.(string)
			}
		case a2asdk.TaskStateWorking:
			if stream, runs := t.s.taskStreams.Load(id); runs {
				also = append(also, streamContext(stream.(string)))
			}
		}
	}
	if err := t.s.writeTask(ta.taskLines, task, by, also...); err != nil {
		return taskstore.TaskVersionMissing, err
	}
	return taskstore.TaskVersion(version + 1), nil
}

func (t tasksOnLines) Get(ctx context.Context, id a2asdk.TaskID) (*taskstore.StoredTask, error) {
	call, err := a2aCallFrom(ctx)
	if err != nil {
		return nil, err
	}
	task, _, version, held, err := t.s.heldTask(call.agent.taskLines, string(id))
	if err != nil {
		return nil, err
	}
	if !held {
		return nil, a2aRefusal(a2asdk.ErrTaskNotFound, "task "+string(id)+" is no task of "+call.agent.called)
	}
	return &taskstore.StoredTask{Task: task, Version: taskstore.TaskVersion(version)}, nil
}

// listedMost is the most Tasks a page holds, and listedSize how many when the
// client names none: "Must be between 1 and 100. If not set, the default value
// is 50."
const (
	listedMost = 100
	listedSize = 50
)

func (t tasksOnLines) List(ctx context.Context, req *a2asdk.ListTasksRequest) (*a2asdk.ListTasksResponse, error) {
	call, err := a2aCallFrom(ctx)
	if err != nil {
		return nil, err
	}
	size := req.PageSize
	if size <= 0 {
		size = listedSize
	}
	size = min(size, listedMost)
	after, err := cursorOf(req.PageToken)
	if err != nil {
		return nil, err
	}
	tasks, err := t.s.tasks(call.agent.taskLines)
	if err != nil {
		return nil, err
	}
	tasks = slices.DeleteFunc(tasks, func(task *a2asdk.Task) bool {
		return (req.ContextID != "" && task.ContextID != req.ContextID) ||
			(req.Status != a2asdk.TaskStateUnspecified && task.Status.State != req.Status) ||
			(req.StatusTimestampAfter != nil && updated(task).Before(*req.StatusTimestampAfter))
	})
	// "Tasks MUST be sorted by last update time in descending order", and by
	// id where two were updated at once, so a page ends at one place.
	slices.SortFunc(tasks, func(a, b *a2asdk.Task) int {
		if at := updated(b).Compare(updated(a)); at != 0 {
			return at
		}
		return strings.Compare(string(a.ID), string(b.ID))
	})
	from := 0
	if after != nil {
		from = len(tasks)
		if at := slices.IndexFunc(tasks, after.precedes); at >= 0 {
			from = at
		}
	}
	page := tasks[from:min(from+size, len(tasks))]
	next := ""
	if from+len(page) < len(tasks) {
		next = cursorAt(page[len(page)-1])
	}
	for _, task := range page {
		task.History = historyOf(task.History, req.HistoryLength)
		if !req.IncludeArtifacts {
			task.Artifacts = nil
		}
	}
	return &a2asdk.ListTasksResponse{Tasks: page, TotalSize: len(tasks), PageSize: size, NextPageToken: next}, nil
}

// historyOf is as much of history as length gives: all of it for none, "the
// client does not impose any limit", and none for zero, "a request to not
// include any messages" (a2a.proto).
func historyOf(history []*a2asdk.Message, length *int) []*a2asdk.Message {
	if length == nil {
		return history
	}
	return history[max(len(history)-max(*length, 0), 0):]
}

// updated is when task last changed.
func updated(task *a2asdk.Task) time.Time {
	if task.Status.Timestamp == nil {
		return time.Time{}
	}
	return *task.Status.Timestamp
}

// cursor is where a page of Tasks ended: the last Task it held.
type cursor struct {
	at time.Time
	id string
}

// precedes is whether task comes after the cursor in the order Tasks are listed in.
func (c cursor) precedes(task *a2asdk.Task) bool {
	at := updated(task)
	return at.Before(c.at) || (at.Equal(c.at) && string(task.ID) > c.id)
}

func cursorAt(task *a2asdk.Task) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(updated(task).UnixNano(), 10) + ":" + string(task.ID)))
}

// cursorOf is where the page token says the page before ended, nil for the
// first page.
func cursorOf(token string) (*cursor, error) {
	if token == "" {
		return nil, nil
	}
	said, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, a2aRefusal(a2asdk.ErrInvalidParams, "pageToken "+token+" is no page token this node gave: "+err.Error())
	}
	nanos, id, cut := strings.Cut(string(said), ":")
	at, err := strconv.ParseInt(nanos, 10, 64)
	if !cut || err != nil {
		return nil, a2aRefusal(a2asdk.ErrInvalidParams, "pageToken "+token+" is no page token this node gave")
	}
	return &cursor{at: time.Unix(0, at), id: id}, nil
}
