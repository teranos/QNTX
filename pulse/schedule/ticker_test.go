package schedule

import (
	"context"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teranos/QNTX/internal/logger"
	"github.com/teranos/QNTX/pulse/async"
)

// mockBroadcaster is a no-op implementation of ExecutionBroadcaster for tests
type mockBroadcaster struct{}

func (m *mockBroadcaster) BroadcastPulseExecutionStarted(scheduledJobID, executionID, atsCode string) {
}
func (m *mockBroadcaster) BroadcastPulseExecutionFailed(scheduledJobID, executionID, atsCode, errorMsg string, errorDetails []string, durationMs int) {
}

func TestEnqueueAsyncJob_WithPrecomputedHandler(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewStore(db)
	queue := async.NewQueue(db)

	// WorkerPool is optional (nil) in tests - only needed for system metrics display
	ticker := NewTicker(store, queue, nil, &mockBroadcaster{}, DefaultTickerConfig(), logger.Logger)

	// Create scheduled job with pre-computed handler/payload (new approach)
	scheduledJob := &Job{
		Id:              "SPJ_precomputed_test",
		AtsCode:         "ix jd https://example.com/job/precomputed",
		HandlerName:     "role.jd-ingestion",
		Payload:         []byte(`{"jd_url":"https://example.com/job/precomputed","actor":"pulse:SPJ_precomputed_test"}`),
		SourceUrl:       "https://example.com/job/precomputed",
		IntervalSeconds: 3600,
		State:           StateActive,
	}

	// Enqueue using the domain-agnostic method
	jobID, err := ticker.enqueueAsyncJob(scheduledJob)
	require.NoError(t, err)
	assert.NotEmpty(t, jobID)

	// Verify async job was created correctly
	asyncJob, err := queue.GetJob(jobID)
	require.NoError(t, err)
	assert.Equal(t, "role.jd-ingestion", asyncJob.HandlerName)
	assert.Equal(t, "https://example.com/job/precomputed", asyncJob.Source)
}

func TestEnqueueAsyncJob_RequiresHandlerName(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewStore(db)
	queue := async.NewQueue(db)

	// WorkerPool is optional (nil) in tests - only needed for system metrics display
	ticker := NewTicker(store, queue, nil, &mockBroadcaster{}, DefaultTickerConfig(), logger.Logger)

	// Create scheduled job WITHOUT pre-computed handler (should fail)
	scheduledJob := &Job{
		Id:              "SPJ_missing_handler",
		AtsCode:         "ix jd https://example.com/job/missing",
		HandlerName:     "", // Empty - should cause error
		IntervalSeconds: 3600,
		State:           StateActive,
	}

	// Enqueue should fail because HandlerName is required
	_, err := ticker.enqueueAsyncJob(scheduledJob)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing handler_name")
}

func TestEnqueueAsyncJob_Deduplication(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewStore(db)
	queue := async.NewQueue(db)

	// WorkerPool is optional (nil) in tests - only needed for system metrics display
	ticker := NewTicker(store, queue, nil, &mockBroadcaster{}, DefaultTickerConfig(), logger.Logger)

	sourceURL := "https://example.com/job/dedup"

	scheduledJob := &Job{
		Id:              "SPJ_dedup1",
		AtsCode:         "ix jd " + sourceURL,
		HandlerName:     "role.jd-ingestion",
		Payload:         []byte(`{"jd_url":"` + sourceURL + `"}`),
		SourceUrl:       sourceURL,
		IntervalSeconds: 3600,
		State:           StateActive,
	}

	// Create first job
	jobID1, err := ticker.enqueueAsyncJob(scheduledJob)
	require.NoError(t, err)
	assert.NotEmpty(t, jobID1)

	// Try to create duplicate job - should return existing job ID
	scheduledJob.Id = "SPJ_dedup2"
	jobID2, err := ticker.enqueueAsyncJob(scheduledJob)
	require.NoError(t, err)
	assert.Equal(t, jobID1, jobID2, "Deduplication should return existing job ID")

	// Complete the first job
	job1, err := queue.GetJob(jobID1)
	require.NoError(t, err)
	job1.Status = async.JobStatusCompleted
	err = queue.UpdateJob(job1)
	require.NoError(t, err)

	// Now a new job should be created
	scheduledJob.Id = "SPJ_dedup3"
	jobID3, err := ticker.enqueueAsyncJob(scheduledJob)
	require.NoError(t, err)
	assert.NotEqual(t, jobID1, jobID3, "New job should be created after previous completed")
}

func TestCheckJobs_Integration(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewStore(db)
	queue := async.NewQueue(db)

	// Create scheduled job with pre-computed handler (new approach)
	now := time.Now()
	scheduledJob := &Job{
		Id:              "SPJ_integration_test",
		AtsCode:         "ix jd https://example.com/job/integration",
		HandlerName:     "role.jd-ingestion",
		Payload:         []byte(`{"jd_url":"https://example.com/job/integration","actor":"pulse:SPJ_integration_test"}`),
		SourceUrl:       "https://example.com/job/integration",
		IntervalSeconds: 3600,
		NextRunAt:       now.Add(-1 * time.Minute).Format(time.RFC3339), // Due 1 minute ago
		State:           StateActive,
		CreatedAt:       now.Format(time.RFC3339),
		UpdatedAt:       now.Format(time.RFC3339),
	}
	err := store.CreateJob(scheduledJob)
	require.NoError(t, err)

	// WorkerPool is optional (nil) in tests - only needed for system metrics display
	ticker := NewTicker(store, queue, nil, &mockBroadcaster{}, DefaultTickerConfig(), logger.Logger)

	// Check for scheduled jobs
	err = ticker.checkScheduledJobs(now)
	require.NoError(t, err)

	// Verify async job was created
	jobs, err := queue.ListJobs(nil, 100)
	require.NoError(t, err)
	assert.Greater(t, len(jobs), 0, "At least one async job should be created")

	// Verify scheduled job was updated
	updated, err := store.GetJob("SPJ_integration_test")
	require.NoError(t, err)
	nextRun, err := time.Parse(time.RFC3339, updated.NextRunAt)
	require.NoError(t, err, "next_run_at %q is not RFC3339", updated.NextRunAt)
	assert.True(t, nextRun.After(now), "NextRunAt should be updated to future time")
	assert.NotEmpty(t, updated.LastExecutionId, "LastExecutionID should be set")
}

func TestCheckJobs_FailsWithoutHandlerName(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewStore(db)
	queue := async.NewQueue(db)

	// Create scheduled job WITHOUT pre-computed handler (should fail at execution)
	now := time.Now()
	scheduledJob := &Job{
		Id:              "SPJ_missing_handler_integration",
		AtsCode:         "ix jd https://example.com/job/missing-handler",
		HandlerName:     "", // Empty - should cause execution to fail
		IntervalSeconds: 3600,
		NextRunAt:       now.Add(-1 * time.Minute).Format(time.RFC3339), // Due 1 minute ago
		State:           StateActive,
		CreatedAt:       now.Format(time.RFC3339),
		UpdatedAt:       now.Format(time.RFC3339),
	}
	err := store.CreateJob(scheduledJob)
	require.NoError(t, err)

	// WorkerPool is optional (nil) in tests - only needed for system metrics display
	ticker := NewTicker(store, queue, nil, &mockBroadcaster{}, DefaultTickerConfig(), logger.Logger)

	// Check for scheduled jobs - the job should be found but execution should fail
	err = ticker.checkScheduledJobs(now)
	// checkScheduledJobs doesn't return individual job errors, it logs them and continues
	require.NoError(t, err)

	// Verify NO async job was created (because handler was missing)
	jobs, err := queue.ListJobs(nil, 100)
	require.NoError(t, err)
	assert.Equal(t, 0, len(jobs), "No async jobs should be created when handler_name is missing")
}

// TestCheckJobs_ShortJobID_DoesNotPanic drives a scheduled job whose ID is
// shorter than 8 chars through the failure branch of executeScheduledJob.
// Reproduces the qntx.example.com panic class (`slice bounds out of range [:8] with
// length 0`): the failure branch logs `scheduled.ID[:8]` and `execution.ID[:8]`
// without length-checking. On the current code any ID shorter than 8 chars —
// including empty ones from `GenerateExecutionID()` when built without
// `qntxwasm` — panics before the log line lands.
//
// The fix is a package-local `shortID` helper used at every ID-slice site
// in `ticker.go`. This test proves the failure path is bounds-safe.
func TestCheckJobs_ShortJobID_DoesNotPanic(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewStore(db)
	queue := async.NewQueue(db)
	ticker := NewTicker(store, queue, nil, &mockBroadcaster{}, DefaultTickerConfig(), logger.Logger)

	// 3-char ID + missing HandlerName triggers the failure branch, which
	// logs `scheduled.ID[:8]` — panics today on the 3-char string.
	now := time.Now()
	scheduledJob := &Job{
		Id:              "SPJ",
		AtsCode:         "ix jd https://example.com/short-id",
		HandlerName:     "",
		IntervalSeconds: 3600,
		NextRunAt:       now.Add(-1 * time.Minute).Format(time.RFC3339),
		State:           StateActive,
		CreatedAt:       now.Format(time.RFC3339),
		UpdatedAt:       now.Format(time.RFC3339),
	}
	require.NoError(t, store.CreateJob(scheduledJob))

	require.NotPanics(t, func() {
		_ = ticker.checkScheduledJobs(now)
	})
}

func TestTickerStartStop(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewStore(db)
	queue := async.NewQueue(db)

	// WorkerPool is optional (nil) in tests - only needed for system metrics display
	ticker := NewTicker(store, queue, nil, &mockBroadcaster{}, DefaultTickerConfig(), logger.Logger)

	// Start ticker
	ticker.Start()

	// Wait for at least one tick (default interval is 1 second)
	time.Sleep(1100 * time.Millisecond)

	// Check stats
	stats := ticker.GetStats()
	assert.NotNil(t, stats["last_tick_at"])
	assert.Greater(t, stats["ticks_since_start"].(int64), int64(0))

	// Stop ticker
	ticker.Stop()

	// Verify stopped
	ticksBefore := stats["ticks_since_start"].(int64)
	time.Sleep(1100 * time.Millisecond)
	statsAfter := ticker.GetStats()
	assert.Equal(t, ticksBefore, statsAfter["ticks_since_start"].(int64), "Ticks should not increment after stop")
}

func TestTickerWithContext_Cancellation(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewStore(db)
	queue := async.NewQueue(db)

	ctx, cancel := context.WithCancel(context.Background())

	// WorkerPool is optional (nil) in tests - only needed for system metrics display
	ticker := NewTickerWithContext(ctx, store, queue, nil, &mockBroadcaster{}, DefaultTickerConfig(), logger.Logger)

	ticker.Start()
	time.Sleep(100 * time.Millisecond)

	// Cancel context
	cancel()

	// Ticker should stop
	ticker.wg.Wait()

	// Should not panic
	stats := ticker.GetStats()
	assert.NotNil(t, stats)
}
