package server

import (
	"testing"
	"time"

	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/pulse/schedule"
	qntxtest "github.com/teranos/QNTX/internal/testing"
)

// TestHandlePulseExecutionUpdate_Failure verifies that when an async job fails,
// the pulse_execution record is updated and BroadcastPulseExecutionFailed is called.
//
// This test ensures IX glyphs receive error feedback (Issue #356).
func TestHandlePulseExecutionUpdate_Failure(t *testing.T) {
	// Create test database with migrations
	db := qntxtest.CreateTestDB(t)

	// Create stores
	scheduleStore := schedule.NewStore(db)
	executionStore := schedule.NewExecutionStore(db)

	// Create a scheduled job (simulating forceTriggerJob)
	now := time.Now()
	scheduledJob := &schedule.Job{
		ID:              "PSJFORCETRIGGER1",
		ATSCode:         "ix https://example.com/repo.git",
		HandlerName:     "ixgest.git",
		State:           schedule.StateInactive,
		IntervalSeconds: 0,
		CreatedFromDoc:  "__force_trigger__",
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := scheduleStore.CreateJob(scheduledJob); err != nil {
		t.Fatalf("Failed to create scheduled job: %v", err)
	}

	// Create pulse_execution record (simulating what forceTriggerJob does)
	execution := &schedule.Execution{
		ID:             "PEX_TEST_123",
		ScheduledJobID: scheduledJob.ID,
		Status:         schedule.ExecutionStatusRunning,
		StartedAt:      now.Format(time.RFC3339),
		CreatedAt:      now.Format(time.RFC3339),
		UpdatedAt:      now.Format(time.RFC3339),
	}
	if err := executionStore.CreateExecution(execution); err != nil {
		t.Fatalf("Failed to create execution: %v", err)
	}

	// Create failed async job
	startTime := now
	completedTime := now.Add(500 * time.Millisecond)
	asyncJobID := "JB_ASYNC_FAILED"
	failedJob := &async.Job{
		ID:          asyncJobID,
		HandlerName: "ixgest.git",
		Source:      "https://example.com/repo.git",
		Status:      async.JobStatusFailed,
		Error:       "no handler registered for handler name: ixgest.git",
		StartedAt:   &startTime,
		CompletedAt: &completedTime,
		CreatedAt:   now,
		UpdatedAt:   completedTime,
	}

	// Link execution to async job (simulating the update when job starts)
	execution.AsyncJobID = &asyncJobID
	if err := executionStore.UpdateExecution(execution); err != nil {
		t.Fatalf("Failed to link async job to execution: %v", err)
	}

	// Create mock server (broadcasts will be no-ops in test, but that's ok)
	mockServer := &QNTXServer{
		db: db,
	}

	// Execute the function under test
	// Note: BroadcastPulseExecutionFailed will be called but won't actually broadcast
	// (no WebSocket clients in test). We verify the database updates instead.
	mockServer.handlePulseExecutionUpdate(failedJob, executionStore, scheduleStore)

	// Verify pulse_execution was updated to 'failed'
	updatedExecution, err := executionStore.GetExecution(execution.ID)
	if err != nil {
		t.Fatalf("Failed to get updated execution: %v", err)
	}

	if updatedExecution.Status != schedule.ExecutionStatusFailed {
		t.Errorf("Expected execution status 'failed', got '%s'", updatedExecution.Status)
	}

	if updatedExecution.ErrorMessage == nil {
		t.Error("Expected error message to be set")
	} else if *updatedExecution.ErrorMessage != failedJob.Error {
		t.Errorf("Expected error message '%s', got '%s'", failedJob.Error, *updatedExecution.ErrorMessage)
	}

	if updatedExecution.DurationMs == nil {
		t.Error("Expected duration_ms to be set")
	} else if *updatedExecution.DurationMs != 500 {
		t.Errorf("Expected duration 500ms, got %dms", *updatedExecution.DurationMs)
	}

	// Note: We don't verify the broadcast call itself in this unit test
	// (would require mock WebSocket infrastructure). The key behavior is
	// that pulse_execution is updated correctly in the database.
	// End-to-end broadcast verification should be done in integration tests.
}
