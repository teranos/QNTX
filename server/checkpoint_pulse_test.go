package server

import (
	"context"
	"testing"

	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/pulse/schedule"
	"github.com/teranos/errors"
)

// TestCheckpointWithoutACheckpointerFails is the axiom. A run that cannot do
// the work must not report success — this returned nil every five minutes, in
// a job status nothing could read as broken.
func TestCheckpointWithoutACheckpointerFails(t *testing.T) {
	store, db := qntxtest.CreateTestStore(t)

	srv, err := NewQNTXServer(db, store, "test.db", 1)
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

	srv, err := NewQNTXServer(db, store, "test.db", 1)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	backend := srv.deps.cfg.Storage.Backend
	if backend == "sqlite" {
		t.Logf("backend is %q, so this run does not exercise the non-sqlite path", backend)
	}

	jobs, err := schedule.NewStore(srv.db).ListAllScheduledJobs()
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
