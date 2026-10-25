package server

import (
	"context"
	"testing"

	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/pulse/schedule"
	errors "github.com/teranos/sacred-error"
)

// TestCheckpointWithoutACheckpointerFails is the axiom. A run that cannot do
// the work must not report success — this returned nil every five minutes, in
// a job status nothing could read as broken.
func TestCheckpointWithoutACheckpointerFails(t *testing.T) {
	store, db := qntxtest.CreateTestStore(t)

	srv, err := NewQNTXServer(db, servingOne(db, store), "test.db", 1)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	srv.walCheckpointer = nil

	h := &checkpointHandler{server: srv, logger: srv.logger.Named("checkpoint")}
	err = h.Execute(context.Background(), &async.Job{
		ID:          "JB-CHECKPOINT-TEST",
		HandlerName: checkpointHandlerName,
	})
	if !errors.Is(err, ErrNoCheckpointer) {
		t.Fatalf("Execute returned %v, want ErrNoCheckpointer", err)
	}
}

// TestCheckpointScheduleExists covers that bringing a server up creates the
// schedule. The backend gate this file was changed for is out of its reach:
// config.Load caches globally, so the run reports which backend it got.
func TestCheckpointScheduleExists(t *testing.T) {
	store, db := qntxtest.CreateTestStore(t)

	srv, err := NewQNTXServer(db, servingOne(db, store), "test.db", 1)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	backend := srv.deps.cfg.Storage.Backend
	if backend == "sqlite" {
		t.Logf("backend is %q, so this run does not exercise the non-sqlite path", backend)
	}

	jobs, err := schedule.NewStore(srv.nodeDB).ListAllScheduledJobs()
	if err != nil {
		t.Fatalf("failed to list scheduled jobs: %v", err)
	}
	live := 0
	for _, j := range jobs {
		if j.HandlerName == checkpointHandlerName && j.State == schedule.StateActive {
			live++
		}
	}
	if live != 1 {
		t.Fatalf("found %d active %s schedules on a %q backend, want 1",
			live, checkpointHandlerName, backend)
	}
}

// The job lives in the store, not in the code: a node that made it at 300 s
// keeps 300 s until boot changes it. One someone set to anything else stays.
func TestCheckpointSchedule_TheOld300BecomesHourly(t *testing.T) {
	store, db := qntxtest.CreateTestStore(t)
	srv, err := NewQNTXServer(db, servingOne(db, store), "test.db", 1)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	schedStore := schedule.NewStore(srv.nodeDB)

	interval := func() (string, int32) {
		t.Helper()
		jobs, err := schedStore.ListAllScheduledJobs()
		if err != nil {
			t.Fatalf("failed to list scheduled jobs: %v", err)
		}
		for _, j := range jobs {
			if j.HandlerName == checkpointHandlerName && j.State == schedule.StateActive {
				return j.Id, j.IntervalSeconds
			}
		}
		t.Fatalf("no active %s schedule", checkpointHandlerName)
		return "", 0
	}

	id, made := interval()
	if made != 3600 {
		t.Fatalf("a new checkpoint schedule runs every %d s, want 3600", made)
	}

	if err := schedStore.UpdateJobInterval(id, 300); err != nil {
		t.Fatalf("failed to put the old interval back: %v", err)
	}
	srv.ensureCheckpointSchedule(schedStore)
	if _, got := interval(); got != 3600 {
		t.Fatalf("the auto-created 300 s schedule is %d s after boot, want 3600", got)
	}

	if err := schedStore.UpdateJobInterval(id, 600); err != nil {
		t.Fatalf("failed to set a chosen interval: %v", err)
	}
	srv.ensureCheckpointSchedule(schedStore)
	if _, got := interval(); got != 600 {
		t.Fatalf("a schedule set to 600 s is %d s after boot, want it left at 600", got)
	}
}

// The schedule asks for the checkpoint each file can make: PASSIVE, and a
// TRUNCATE only past walTruncateAtBytes.
func TestCheckpointAsksForTheLimit(t *testing.T) {
	store, db := qntxtest.CreateTestStore(t)
	srv, err := NewQNTXServer(db, servingOne(db, store), "test.db", 1)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	asked := &askedCheckpointer{}
	srv.walCheckpointer = asked

	h := &checkpointHandler{server: srv, logger: srv.logger.Named("checkpoint")}
	if err := h.Execute(context.Background(), &async.Job{ID: "JB-CHECKPOINT-TEST", HandlerName: checkpointHandlerName}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if asked.truncateAt != walTruncateAtBytes || asked.calls != 1 {
		t.Fatalf("asked %d times with truncateAt %d, want once with %d", asked.calls, asked.truncateAt, walTruncateAtBytes)
	}
}

type askedCheckpointer struct {
	calls      int
	truncateAt int64
}

func (a *askedCheckpointer) WALCheckpoint(truncateAt int64) (int, int, int, error) {
	a.calls++
	a.truncateAt = truncateAt
	return 0, 0, 0, nil
}
