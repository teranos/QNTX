package harness

import (
	"context"
	"encoding/json"
	"sync"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
)

// 「[Fable 5.2, Fable 5.1] TurnsKept is how many Tasks whose turn ended a harness plugin still answers」
// 「[Fable 5.2, Fable 5.1] for.」
const TurnsKept = 16

// 「[Fable 5.2, Fable 5.1] turns is the Tasks a harness plugin holds: every one whose turn runs,」
// 「[Fable 5.2, Fable 5.1] and the newest kept of those that ended. a2a-go's own in-memory store keeps」
// 「[Fable 5.2, Fable 5.1] every Task for as long as the process lives.」
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

// 「[Fable 5.2, Fable 5.1] Turns is the Task store of a harness plugin. Core writes every turn down」
// 「[Fable 5.2, Fable 5.1] (ADR-059) and asks for none again, so the plugin keeps a Task only while its」
// 「[Fable 5.2, Fable 5.1] turn runs, and kept after.」
func Turns(kept int) taskstore.Store { return newTurns(kept) }

func newTurns(kept int) *turns {
	return &turns{kept: kept, held: map[a2asdk.TaskID]*turn{}}
}

// 「[Fable 5.2, Fable 5.1] Running is each turn a harness plugin runs, by its Task, and how it is」
// 「[Fable 5.2, Fable 5.1] stopped. a2a-go runs a turn apart from the call that asked for it, so a」
// 「[Fable 5.2, Fable 5.1] caller that left does not end the turn's context: Cancel does, which core」
// 「[Fable 5.2, Fable 5.1] asks for when it gives a turn up.」
type Running struct {
	stops sync.Map
}

// 「[Fable 5.2, Fable 5.1] Began has the Task's turn stopped by stop, until the ended it answers with」
// 「[Fable 5.2, Fable 5.1] is called.」
func (r *Running) Began(id a2asdk.TaskID, stop context.CancelFunc) (ended func()) {
	r.stops.Store(id, stop)
	return func() { r.stops.Delete(id) }
}

// 「[Fable 5.2, Fable 5.1] Stop stops the Task's turn, when one runs that may be stopped.」
func (r *Running) Stop(id a2asdk.TaskID) {
	if stop, runs := r.stops.Load(id); runs {
		stop.(context.CancelFunc)()
	}
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

// 「[Fable 5.2, Fable 5.1] settle counts a Task whose turn ended among those kept, and lets go of the」
// 「[Fable 5.2, Fable 5.1] oldest past kept.」
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

// 「[Fable 5.2, Fable 5.1] List answers with no Task: core asks a harness plugin for none, and what a」
// 「[Fable 5.2, Fable 5.1] turn said is the node's to answer for, where it is written down.」
func (s *turns) List(context.Context, *a2asdk.ListTasksRequest) (*a2asdk.ListTasksResponse, error) {
	return &a2asdk.ListTasksResponse{Tasks: []*a2asdk.Task{}}, nil
}

// 「[Fable 5.2, Fable 5.1] Caller is the caller of one turn's stream, as the plugin's server knows」
// 「[Fable 5.2, Fable 5.1] them: told when they left, they stop the run they asked for. A turn core」
// 「[Fable 5.2, Fable 5.1] gives up before it was told which Task it is has no Task to cancel.」
type Caller struct {
	mu   sync.Mutex
	left bool
	stop context.CancelFunc
}

type callerKey struct{}

// 「[Fable 5.2, Fable 5.1] WithCaller is ctx carrying a Caller, for the server of a turn's stream.」
func WithCaller(ctx context.Context) (context.Context, *Caller) {
	caller := &Caller{}
	return context.WithValue(ctx, callerKey{}, caller), caller
}

// 「[Fable 5.2, Fable 5.1] StoppedWhenTheCallerLeaves has stop called when the caller of ctx's stream」
// 「[Fable 5.2, Fable 5.1] leaves: at once, when they already have. A ctx no server gave a Caller is」
// 「[Fable 5.2, Fable 5.1] stopped by Cancel alone.」
func StoppedWhenTheCallerLeaves(ctx context.Context, stop context.CancelFunc) {
	caller, served := ctx.Value(callerKey{}).(*Caller)
	if !served {
		return
	}
	caller.mu.Lock()
	left := caller.left
	caller.stop = stop
	caller.mu.Unlock()
	if left {
		stop()
	}
}

// 「[Fable 5.2, Fable 5.1] Left says the caller left before the turn ended.」
func (c *Caller) Left() {
	c.mu.Lock()
	c.left = true
	stop := c.stop
	c.mu.Unlock()
	if stop != nil {
		stop()
	}
}
