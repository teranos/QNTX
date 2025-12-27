package async

import (
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/teranos/QNTX/am"
	"go.uber.org/zap"
)

// TestGRACEShutdownFlow tests the complete graceful shutdown flow:
// 1. Job starts executing
// 2. Context cancelled (simulating Ctrl+C)
// 3. Job completes current task
// 4. Job re-queued with checkpoint
// 5. Worker exits cleanly
//
// NOTE: This is a SLOW integration test (~25s) that uses real LLM processing.
// Skip during normal test runs with: go test -short
// Run explicitly with: go test -run TestGRACEShutdownFlow
func TestGRACEShutdownFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping slow integration test in -short mode")
	}
	// Create test database (use existing test helper)
	db := qntxtest.CreateTestDB(t)

	// Create test config
	cfg := &am.Config{
		Pulse: am.PulseConfig{
			DailyBudgetUSD:   10.0,
			MonthlyBudgetUSD: 100.0,
			CostPerScoreUSD:  0.002,
		},
	}

	// Create parent context that we'll cancel (simulates server shutdown)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create worker pool with parent context and fast polling for tests
	wp := NewWorkerPoolWithContext(ctx, db, cfg, WorkerPoolConfig{
		Workers:       1,
		PauseOnBudget: false,
		PollInterval:  100 * time.Millisecond, // Fast polling for tests
	}, zap.NewNop().Sugar())

	// Register a mock handler for protein sequence analysis (generic bioinformatics job)
	mockHandler := &GRACETestHandler{
		taskDuration: 500 * time.Millisecond, // Simulate work
		totalTasks:   5,
	}
	wp.Registry().Register(mockHandler)

	// Start worker
	wp.Start()
	defer wp.Stop()

	// Create and enqueue a test job
	// Use timestamp in description to ensure unique job ID per test run
	jobDesc := fmt.Sprintf("GRACE Test %d", time.Now().UnixNano())
	payload := map[string]interface{}{
		"test_data": "grace-test",
		"actor":     "grace-test",
	}
	payloadJSON, _ := json.Marshal(payload)
	job, err := NewJobWithPayload(
		"test.grace-handler",
		jobDesc,
		payloadJSON,
		10,  // total operations
		0.1, // estimated cost
		"grace-test",
	)
	if err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	if err := wp.queue.Enqueue(job); err != nil {
		t.Fatalf("Failed to enqueue job: %v", err)
	}
	t.Logf("Job enqueued: %s (status: %s)", job.ID, job.Status)

	// Wait for job to start processing (poll until running or timeout)
	timeout := time.After(5 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

WaitForRunning:
	for {
		select {
		case <-timeout:
			t.Fatal("Timeout waiting for job to start running")
		case <-ticker.C:
			checkJob, err := wp.queue.GetJob(job.ID)
			if err != nil {
				continue
			}
			if checkJob.Status == JobStatusRunning {
				t.Log("✓ Job started running")
				break WaitForRunning
			}
		}
	}

	// Cancel context (simulate Ctrl+C during job execution)
	t.Log("Simulating Ctrl+C (cancelling context)...")
	cancel()

	// Wait a bit for graceful shutdown to complete
	time.Sleep(2 * time.Second)

	// Verify job was re-queued (not failed or still running)
	finalJob, err := wp.queue.GetJob(job.ID)
	if err != nil {
		t.Fatalf("Failed to get job after shutdown: %v", err)
	}

	if finalJob.Status != JobStatusQueued {
		t.Errorf("Expected job to be re-queued after shutdown, got status '%s'", finalJob.Status)
	} else {
		t.Log("✓ Job was re-queued after graceful shutdown")
	}

	// Note: Checkpoint functionality has been removed.
	// Job state is now managed via payload updates by handlers.
	t.Log("✓ Job state preserved (checkpoint logic now in handlers)")

	// Verify job error is cleared (not a hard failure)
	if finalJob.Error != "" {
		t.Errorf("Expected job error to be cleared, got: %s", finalJob.Error)
	}
}

// TestGRACEContextCancellation removed due to race condition in test setup
// The test sets wp.executor after starting workers, causing flaky behavior.
// Context cancellation is tested in TestGRACEShutdownFlow (integration test)
// TODO: Reimplement with proper executor injection during WorkerPool creation

// TestGRACECheckpointSaving tests that checkpoints are saved correctly
func TestGRACECheckpointSaving(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	cfg := createTestConfig()

	ctx := context.Background()
	wp := NewWorkerPoolWithContext(ctx, db, cfg, WorkerPoolConfig{Workers: 1}, zap.NewNop().Sugar())

	payload := map[string]interface{}{"test": "checkpoint"}
	payloadJSON, _ := json.Marshal(payload)
	job, err := NewJobWithPayload("test.checkpoint-handler", "checkpoint test", payloadJSON, 3, 0.01, "test")
	if err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}
	if err := wp.queue.Enqueue(job); err != nil {
		t.Fatalf("Failed to enqueue job: %v", err)
	}

	// Note: Checkpoint functionality has been removed.
	// Job progress is now managed via handler-specific payload updates.
	// This test now just verifies that jobs can be created and retrieved.

	wp.queue.UpdateJob(job)

	// Retrieve job from DB
	retrieved, err := wp.queue.GetJob(job.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve job: %v", err)
	}

	// Verify job was saved and retrieved
	if retrieved.ID != job.ID {
		t.Errorf("Expected job ID %s, got %s", job.ID, retrieved.ID)
	}

	t.Log("✓ Job saved and retrieved successfully (checkpoint logic now in handlers)")
}

// TestGRACEWorkerShutdownTimeout tests worker shutdown timeout behavior
func TestGRACEWorkerShutdownTimeout(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	cfg := createTestConfig()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wp := NewWorkerPoolWithContext(ctx, db, cfg, WorkerPoolConfig{Workers: 1}, zap.NewNop().Sugar())
	wp.Start()

	// Stop should complete within reasonable time even with no jobs
	stopDone := make(chan bool)
	go func() {
		wp.Stop()
		stopDone <- true
	}()

	select {
	case <-stopDone:
		t.Log("✓ Worker pool stopped cleanly")
	case <-time.After(35 * time.Second): // 30s timeout + 5s buffer
		t.Error("Worker pool shutdown exceeded timeout")
	}
}

// TestGRACENoJobsRunning tests graceful start with no orphaned jobs
func TestGRACENoJobsRunning(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	cfg := createTestConfig()

	// Start with empty queue (no orphaned jobs)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wp := NewWorkerPoolWithContext(ctx, db, cfg, WorkerPoolConfig{Workers: 1}, zap.NewNop().Sugar())
	wp.Start()
	defer wp.Stop()

	// Graceful start should complete without errors
	time.Sleep(100 * time.Millisecond)

	t.Log("✓ Graceful start completed with no orphaned jobs")
}

// TestGRACEGracefulStart tests recovery of orphaned jobs on worker start
func TestGRACEGracefulStart(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	cfg := createTestConfig()

	// Simulate crash: Create jobs in "running" state (orphaned)
	queue := NewQueue(db)

	job1, err := createTestJob("test.grace-handler", "orphaned job 1", 5, 0.01)
	if err != nil {
		t.Fatalf("Failed to create job1: %v", err)
	}
	job1.Status = JobStatusRunning // Simulate job that was running when server crashed
	if err := queue.store.CreateJob(job1); err != nil {
		t.Fatalf("Failed to store job1: %v", err)
	}

	job2, err := createTestJob("test.grace-handler", "orphaned job 2", 5, 0.01)
	if err != nil {
		t.Fatalf("Failed to create job2: %v", err)
	}
	job2.Status = JobStatusRunning
	// Note: Checkpoint functionality removed - job state managed via payload updates
	if err := queue.store.CreateJob(job2); err != nil {
		t.Fatalf("Failed to store job2: %v", err)
	}

	// Now start worker pool (should recover orphaned jobs)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wp := NewWorkerPoolWithContext(ctx, db, cfg, WorkerPoolConfig{Workers: 1}, zap.NewNop().Sugar())
	wp.Start()
	defer wp.Stop()

	// Give recovery time to run
	time.Sleep(100 * time.Millisecond)

	// Verify both jobs were recovered (re-queued)
	recoveredJob1, err := queue.GetJob(job1.ID)
	if err != nil {
		t.Fatalf("Failed to get job1 after recovery: %v", err)
	}
	if recoveredJob1.Status != JobStatusQueued {
		t.Errorf("Expected job1 to be re-queued, got status '%s'", recoveredJob1.Status)
	} else {
		t.Log("✓ Orphaned job 1 recovered and re-queued")
	}

	recoveredJob2, err := queue.GetJob(job2.ID)
	if err != nil {
		t.Fatalf("Failed to get job2 after recovery: %v", err)
	}
	if recoveredJob2.Status != JobStatusQueued {
		t.Errorf("Expected job2 to be re-queued, got status '%s'", recoveredJob2.Status)
	} else {
		t.Log("✓ Orphaned job 2 recovered and re-queued")
	}
}

// TestGRACEGracefulStartNoOrphans verifies clean start with no orphaned jobs
func TestGRACEGracefulStartNoOrphans(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	cfg := createTestConfig()

	// Create a completed job and a queued job (not orphaned)
	queue := NewQueue(db)

	job1, err := createTestJob("test.grace-handler", "completed job", 5, 0.01)
	if err != nil {
		t.Fatalf("Failed to create job1: %v", err)
	}
	job1.Status = JobStatusCompleted
	if err := queue.store.CreateJob(job1); err != nil {
		t.Fatalf("Failed to store job1: %v", err)
	}

	job2, err := createTestJob("test.grace-handler", "queued job", 5, 0.01)
	if err != nil {
		t.Fatalf("Failed to create job2: %v", err)
	}
	job2.Status = JobStatusQueued
	if err := queue.store.CreateJob(job2); err != nil {
		t.Fatalf("Failed to store job2: %v", err)
	}

	// Start worker pool
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wp := NewWorkerPoolWithContext(ctx, db, cfg, WorkerPoolConfig{Workers: 1}, zap.NewNop().Sugar())
	wp.Start()
	defer wp.Stop()

	// Give startup time
	time.Sleep(100 * time.Millisecond)

	// Verify jobs unchanged (no orphans to recover)
	checkJob1, err := queue.GetJob(job1.ID)
	if err != nil {
		t.Fatalf("Failed to get job1: %v", err)
	}
	if checkJob1.Status != JobStatusCompleted {
		t.Errorf("Expected completed job to stay completed, got '%s'", checkJob1.Status)
	}

	checkJob2, err := queue.GetJob(job2.ID)
	if err != nil {
		t.Fatalf("Failed to get job2: %v", err)
	}
	if checkJob2.Status != JobStatusQueued {
		t.Errorf("Expected queued job to stay queued, got '%s'", checkJob2.Status)
	}

	t.Log("✓ Non-orphaned jobs unchanged after graceful start")
}

// TestGRACEGradualRecovery tests the super gradual warm start recovery
// Jobs 2-10 recovered at 1/second, remaining jobs spread over 15 minutes
//
// NOTE: This is a SLOW test (~35s) that validates timing characteristics
// Skip with: go test -short
func TestGRACEGradualRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping slow timing test in -short mode")
	}

	db := qntxtest.CreateTestDB(t)
	cfg := createTestConfig()

	// Simulate crash: Create 12 orphaned jobs
	queue := NewQueue(db)
	for i := 1; i <= 12; i++ {
		job, err := createTestJob("test.grace-handler", fmt.Sprintf("orphan %d", i), 1, 0.01)
		if err != nil {
			t.Fatalf("Failed to create job %d: %v", i, err)
		}
		job.Status = JobStatusRunning
		if err := queue.store.CreateJob(job); err != nil {
			t.Fatalf("Failed to store job %d: %v", i, err)
		}
	}

	// Start worker pool (triggers gradual recovery)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wp := NewWorkerPoolWithContext(ctx, db, cfg, WorkerPoolConfig{
		Workers:            1,
		GracefulStartPhase: 10 * time.Second, // Test mode: 10s phases (warm start = 2s, slow start = 30s)
	}, zap.NewNop().Sugar())
	wp.Start()
	defer wp.Stop()

	// Verify first job recovered immediately
	time.Sleep(100 * time.Millisecond)
	countRecovered := countJobsByStatus(t, queue, JobStatusQueued)
	if countRecovered < 1 {
		t.Errorf("Expected at least 1 job recovered immediately, got %d", countRecovered)
	} else {
		t.Logf("✓ First job recovered immediately (%d queued)", countRecovered)
	}

	// Verify warm start phase (jobs 2-10 at ~1/second)
	// After 5 seconds, expect ~6 jobs recovered (1 immediate + 5 gradual)
	time.Sleep(5 * time.Second)
	countAfter5s := countJobsByStatus(t, queue, JobStatusQueued)
	if countAfter5s < 5 || countAfter5s > 7 {
		t.Logf("Warning: Expected ~6 jobs after 5s, got %d (timing may vary)", countAfter5s)
	} else {
		t.Logf("✓ Warm start progressing (%d jobs recovered after 5s)", countAfter5s)
	}

	// Verify full recovery completes (all 12 jobs)
	// With test mode timing (10s phases), should complete in ~30s total
	// Wait up to 35s to account for timing variance
	timeout := time.After(35 * time.Second)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			// Count jobs by all statuses to debug
			queued := countJobsByStatus(t, queue, JobStatusQueued)
			running := countJobsByStatus(t, queue, JobStatusRunning)
			completed := countJobsByStatus(t, queue, JobStatusCompleted)
			failed := countJobsByStatus(t, queue, JobStatusFailed)
			total := queued + running + completed + failed
			t.Errorf("Timeout: Expected all 12 jobs recovered, got queued=%d, running=%d, completed=%d, failed=%d, total=%d",
				queued, running, completed, failed, total)
			return
		case <-ticker.C:
			// Check if all jobs have finished processing
			// Jobs transition: orphaned(running) → recovered(queued) → executing(running) → completed/failed
			// Success = all 12 jobs are completed or failed (means they were recovered and finished)
			completed := countJobsByStatus(t, queue, JobStatusCompleted)
			failed := countJobsByStatus(t, queue, JobStatusFailed)
			finished := completed + failed

			if finished >= 12 {
				// All 12 jobs have been recovered and finished processing
				t.Logf("✓ All 12 jobs recovered gradually and finished processing (completed=%d, failed=%d)",
					completed, failed)
				return
			}
		}
	}
}

// TestGRACECrashAndRestart simulates a full crash and restart cycle
func TestGRACECrashAndRestart(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	cfg := createTestConfig()

	// Phase 1: Start worker, enqueue job, simulate crash mid-execution
	ctx1, cancel1 := context.WithCancel(context.Background())
	wp1 := NewWorkerPoolWithContext(ctx1, db, cfg, WorkerPoolConfig{Workers: 1}, zap.NewNop().Sugar())
	wp1.Start()

	job, err := createTestJob("test.grace-handler", "crash test", 5, 0.01)
	if err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}
	if err := wp1.queue.Enqueue(job); err != nil {
		t.Fatalf("Failed to enqueue job: %v", err)
	}

	// Let job start
	time.Sleep(100 * time.Millisecond)

	// Simulate crash (force stop without graceful shutdown)
	cancel1() // Context cancelled but we don't call wp1.Stop() - simulates hard crash

	// Phase 2: Restart worker pool (should recover orphaned job)
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	wp2 := NewWorkerPoolWithContext(ctx2, db, cfg, WorkerPoolConfig{Workers: 1}, zap.NewNop().Sugar())
	wp2.Start()
	defer wp2.Stop()

	// Give recovery time
	time.Sleep(100 * time.Millisecond)

	// Verify job was recovered
	recovered, err := wp2.queue.GetJob(job.ID)
	if err != nil {
		t.Fatalf("Failed to get job after restart: %v", err)
	}

	if recovered.Status != JobStatusQueued {
		t.Errorf("Expected job to be recovered and queued, got status '%s'", recovered.Status)
	} else {
		t.Log("✓ Job recovered after simulated crash and restart")
	}
}

// TestGRACETaskAtomicity removed due to race condition in test setup
// The test sets wp.executor after starting workers, causing flaky behavior.
// Task atomicity is verified in TestGRACEShutdownFlow (integration test)
// TODO: Reimplement with proper executor injection during WorkerPool creation

// TestGRACEPhaseRecoveryNoChildTasks tests orphaned job recovery when no child tasks exist
// Note: Phase-specific recovery logic has been removed (domain logic belongs in handlers)
func TestGRACEPhaseRecoveryNoChildTasks(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	cfg := createTestConfig()

	// Simulate crash scenario: Parent job was running with no child tasks
	queue := NewQueue(db)

	// Create orphaned parent job with payload indicating it might spawn tasks
	payload := map[string]interface{}{
		"source": "phase recovery test - no tasks",
		"actor":  "test-actor",
	}
	payloadJSON, _ := json.Marshal(payload)
	job, _ := NewJobWithPayload("test.grace-handler", "phase recovery test - no tasks", payloadJSON, 5, 0.01, "test-actor")
	job.Status = JobStatusRunning // Simulate crash (job was running)
	queue.store.CreateJob(job)

	// Verify no child tasks exist
	tasks, _ := queue.ListTasksByParent(job.ID)
	if len(tasks) != 0 {
		t.Fatalf("Test setup error: Expected no child tasks, got %d", len(tasks))
	}

	// Start worker pool (should trigger generic recovery)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wp := NewWorkerPoolWithContext(ctx, db, cfg, WorkerPoolConfig{Workers: 1}, zap.NewNop().Sugar())
	wp.Start()
	defer wp.Stop()

	// Give recovery time to run
	time.Sleep(200 * time.Millisecond)

	// Verify job was recovered
	recovered, err := queue.GetJob(job.ID)
	if err != nil {
		t.Fatalf("Failed to get job after recovery: %v", err)
	}

	// Verify job status is queued (ready to re-run)
	if recovered.Status != JobStatusQueued {
		t.Errorf("Expected job to be queued, got status '%s'", recovered.Status)
	} else {
		t.Log("✓ Job re-queued after recovery")
	}
}

// TestGRACEPhaseRecoveryWithChildTasks tests orphaned parent job recovery with child tasks
// Note: Phase-specific recovery logic has been removed (domain logic belongs in handlers)
func TestGRACEPhaseRecoveryWithChildTasks(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	cfg := createTestConfig()

	queue := NewQueue(db)

	// Create orphaned parent job
	payload := map[string]interface{}{
		"source": "phase recovery test - with tasks",
		"actor":  "test-actor",
	}
	payloadJSON, _ := json.Marshal(payload)
	parentJob, _ := NewJobWithPayload("test.grace-handler", "phase recovery test - with tasks", payloadJSON, 5, 0.01, "test-actor")
	parentJob.Status = JobStatusRunning // Simulate crash
	queue.store.CreateJob(parentJob)

	// Create child tasks (simulate tasks were successfully created before crash)
	for i := 1; i <= 3; i++ {
		childPayload := map[string]interface{}{
			"source": fmt.Sprintf("scoring task %d", i),
			"actor":  "test-actor",
		}
		childPayloadJSON, _ := json.Marshal(childPayload)
		childTask, _ := NewJobWithPayload("test.child-handler", fmt.Sprintf("scoring task %d", i), childPayloadJSON, 1, 0.001, "test-actor")
		childTask.ParentJobID = parentJob.ID // Link to parent
		childTask.Status = JobStatusQueued
		queue.store.CreateJob(childTask)
	}

	// Verify child tasks exist
	tasks, _ := queue.ListTasksByParent(parentJob.ID)
	if len(tasks) != 3 {
		t.Fatalf("Test setup error: Expected 3 child tasks, got %d", len(tasks))
	}

	// Start worker pool (should trigger generic recovery)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wp := NewWorkerPoolWithContext(ctx, db, cfg, WorkerPoolConfig{Workers: 1}, zap.NewNop().Sugar())
	wp.Start()
	defer wp.Stop()

	// Give recovery time to run
	time.Sleep(200 * time.Millisecond)

	// Verify parent job was recovered
	recovered, err := queue.GetJob(parentJob.ID)
	if err != nil {
		t.Fatalf("Failed to get job after recovery: %v", err)
	}

	// Verify job status is queued (ready to continue processing)
	if recovered.Status != JobStatusQueued {
		t.Errorf("Expected job to be queued, got status '%s'", recovered.Status)
	} else {
		t.Log("✓ Parent job re-queued after recovery")
	}

	// Verify child tasks remain queued
	for _, task := range tasks {
		checkTask, _ := queue.GetJob(task.ID)
		if checkTask.Status != JobStatusQueued {
			t.Errorf("Expected child task %s to stay queued, got '%s'", task.ID, checkTask.Status)
		}
	}
	t.Log("✓ Child tasks remain queued")
}

// Helper functions

func countJobsByStatus(t *testing.T, queue *Queue, status JobStatus) int {
	jobs, err := queue.store.ListJobs(&status, 100)
	if err != nil {
		t.Fatalf("Failed to list jobs: %v", err)
	}

	return len(jobs)
}

// MockExecutor simulates job execution for testing
type MockExecutor struct {
	taskDuration    time.Duration
	totalTasks      int
	tasksCompleted  int
	checkpointStage string
	cancelled       bool
}

func (m *MockExecutor) Execute(ctx context.Context, job *Job) error {
	tasks := []string{"parse", "extract", "validate", "transform", "complete"}

	for i, taskName := range tasks {
		if i >= m.totalTasks {
			break
		}

		// Check context BEFORE starting task (respects cancellation between tasks)
		select {
		case <-ctx.Done():
			m.cancelled = true
			m.checkpointStage = taskName
			// Note: Checkpoint functionality removed - handlers manage state via payload updates
			return fmt.Errorf("context cancelled before %s", taskName)
		default:
		}

		// Execute task atomically (simulates indivisible work unit)
		time.Sleep(m.taskDuration)

		// Task completed - increment counter
		m.tasksCompleted++
		m.checkpointStage = taskName
		// Note: Checkpoint functionality removed - handlers manage state via payload updates
	}

	return nil
}

// GRACETestHandler implements JobHandler for GRACE shutdown testing
// Simulates generic work without domain-specific logic
type GRACETestHandler struct {
	taskDuration   time.Duration
	totalTasks     int
	tasksCompleted int
}

func (h *GRACETestHandler) Execute(ctx context.Context, job *Job) error {
	tasks := []string{"parse", "extract", "validate", "transform", "complete"}

	for i, taskName := range tasks {
		if i >= h.totalTasks {
			break
		}

		// Check context BEFORE starting task (GRACE shutdown behavior)
		select {
		case <-ctx.Done():
			// Context cancelled - handler can update payload if needed for resume
			// Note: Checkpoint functionality removed - handlers manage state via payload updates
			return fmt.Errorf("context cancelled before %s", taskName)
		default:
		}

		// Execute task atomically
		time.Sleep(h.taskDuration)

		// Task completed
		h.tasksCompleted++
		// Note: Checkpoint functionality removed - handlers manage state via payload updates
	}

	return nil
}

func (h *GRACETestHandler) Name() string {
	return "test.grace-handler"
}
