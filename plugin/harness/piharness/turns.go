package piharness

import (
	"context"
	"encoding/json"
	"sync"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
)

// turnsKept is how many Tasks whose turn ended the plugin still answers for.
const turnsKept = 16

// turns is the Tasks a Pi harness plugin holds: every one whose turn runs,
// and the newest kept of those that ended. a2a-go's own in-memory store keeps
// every Task for as long as the process lives.
type turns struct {
	mu    sync.Mutex
	kept  int
	held  map[a2asdk.TaskID]*turn
	ended []a2asdk.TaskID
}

type turn struct {
	task    []byte
	version taskstore.TaskVersion
	ended   bool
}

var _ taskstore.Store = (*turns)(nil)

func newTurns(kept int) *turns {
	return &turns{kept: kept, held: map[a2asdk.TaskID]*turn{}}
}

func (s *turns) Create(_ context.Context, task *a2asdk.Task) (taskstore.TaskVersion, error) {
	written, err := json.Marshal(task)
	if err != nil {
		return taskstore.TaskVersionMissing, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.held[task.ID] != nil {
		return taskstore.TaskVersionMissing, taskstore.ErrTaskAlreadyExists
	}
	s.held[task.ID] = &turn{task: written, version: 1}
	s.settle(task)
	return 1, nil
}

func (s *turns) Update(_ context.Context, update *taskstore.UpdateRequest) (taskstore.TaskVersion, error) {
	written, err := json.Marshal(update.Task)
	if err != nil {
		return taskstore.TaskVersionMissing, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	held := s.held[update.Task.ID]
	if held == nil {
		return taskstore.TaskVersionMissing, a2asdk.ErrTaskNotFound
	}
	if update.PrevVersion != taskstore.TaskVersionMissing && held.version != update.PrevVersion {
		return taskstore.TaskVersionMissing, taskstore.ErrConcurrentModification
	}
	held.task, held.version = written, held.version+1
	s.settle(update.Task)
	return held.version, nil
}

// settle counts a Task whose turn ended among those kept, and lets go of the
// oldest past turnsKept.
func (s *turns) settle(task *a2asdk.Task) {
	held := s.held[task.ID]
	if held.ended || !task.Status.State.Terminal() {
		return
	}
	held.ended = true
	s.ended = append(s.ended, task.ID)
	for len(s.ended) > s.kept {
		delete(s.held, s.ended[0])
		s.ended = s.ended[1:]
	}
}

func (s *turns) Get(_ context.Context, id a2asdk.TaskID) (*taskstore.StoredTask, error) {
	s.mu.Lock()
	held := s.held[id]
	var written []byte
	var version taskstore.TaskVersion
	if held != nil {
		written, version = held.task, held.version
	}
	s.mu.Unlock()
	if held == nil {
		return nil, a2asdk.ErrTaskNotFound
	}
	var task a2asdk.Task
	if err := json.Unmarshal(written, &task); err != nil {
		return nil, err
	}
	return &taskstore.StoredTask{Task: &task, Version: version}, nil
}

// List answers with no Task: core asks a harness plugin for none, and what a
// turn said is the node's to answer for, where it is written down.
func (s *turns) List(context.Context, *a2asdk.ListTasksRequest) (*a2asdk.ListTasksResponse, error) {
	return &a2asdk.ListTasksResponse{Tasks: []*a2asdk.Task{}}, nil
}
