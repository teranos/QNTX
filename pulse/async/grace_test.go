package async

import (
	"context"
	"encoding/json"
	"fmt"
	qntxtest "github.com/teranos/QNTX/internal/testing"
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
	pollInterval := 100 * time.Millisecond
	wp := NewWorkerPoolWithContext(ctx, db, cfg, WorkerPoolConfig{
		Workers:       1,
		PauseOnBudget: false,
		PollInterval:  &pollInterval, // Fast polling for tests
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
	payload := map[string]any{
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
				t.Log("Job started running")
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
	}

	// Verify job error is cleared (not a hard failure)
	if finalJob.Error != "" {
		t.Errorf("Expected job error to be cleared, got: %s", finalJob.Error)
	}
}

// TestGRACEOrphanRecovery tests recovery of orphaned jobs on worker start.
// Simulates a crash (jobs left in "running" state) then verifies new worker
// pool marks them as failed so the scheduler can re-create them.
func TestGRACEOrphanRecovery(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	cfg := createTestConfig()

	queue := NewQueue(db)

	// Simulate crash: Create jobs in "running" state (orphaned)
	job1, err := createTestJob("test.grace-handler", "orphaned job 1", 5, 0.01)
	if err != nil {
		t.Fatalf("Failed to create job1: %v", err)
	}
	job1.Status = JobStatusRunning
	if err := queue.store.CreateJob(job1); err != nil {
		t.Fatalf("Failed to store job1: %v", err)
	}

	job2, err := createTestJob("test.grace-handler", "orphaned job 2", 5, 0.01)
	if err != nil {
		t.Fatalf("Failed to create job2: %v", err)
	}
	job2.Status = JobStatusRunning
	if err := queue.store.CreateJob(job2); err != nil {
		t.Fatalf("Failed to store job2: %v", err)
	}

	// Start worker pool (should recover orphaned jobs)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wp := NewWorkerPoolWithContext(ctx, db, cfg, WorkerPoolConfig{Workers: 1}, zap.NewNop().Sugar())
	wp.Start()
	defer wp.Stop()

	// Give recovery time to run
	time.Sleep(200 * time.Millisecond)

	// Verify both jobs were marked failed (orphan recovery)
	recoveredJob1, err := queue.GetJob(job1.ID)
	if err != nil {
		t.Fatalf("Failed to get job1 after recovery: %v", err)
	}
	if recoveredJob1.Status != JobStatusFailed {
		t.Errorf("Expected job1 to be failed, got status '%s'", recoveredJob1.Status)
	}

	recoveredJob2, err := queue.GetJob(job2.ID)
	if err != nil {
		t.Fatalf("Failed to get job2 after recovery: %v", err)
	}
	if recoveredJob2.Status != JobStatusFailed {
		t.Errorf("Expected job2 to be failed, got status '%s'", recoveredJob2.Status)
	}
}

// TestGRACECrashAndRestart simulates a full crash and restart cycle.
// Phase 1: Start worker, enqueue job, hard-cancel context (simulate crash).
// Phase 2: Start new worker pool, verify orphaned job is recovered.
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
	cancel1()

	// Phase 2: Restart worker pool (should recover orphaned job)
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	wp2 := NewWorkerPoolWithContext(ctx2, db, cfg, WorkerPoolConfig{Workers: 1}, zap.NewNop().Sugar())
	wp2.Start()
	defer wp2.Stop()

	// Give recovery time
	time.Sleep(200 * time.Millisecond)

	// Verify job was recovered (marked failed for scheduler to re-create)
	recovered, err := wp2.queue.GetJob(job.ID)
	if err != nil {
		t.Fatalf("Failed to get job after restart: %v", err)
	}

	// Job should be failed (orphan recovery) or queued (re-queued by graceful handler)
	if recovered.Status != JobStatusQueued && recovered.Status != JobStatusFailed {
		t.Errorf("Expected job to be recovered (queued or failed), got status '%s'", recovered.Status)
	}
}

// TestGRACEChildTasksPreserved tests that child tasks survive parent orphan recovery.
// When a parent job is marked failed during recovery, its child tasks must remain queued.
func TestGRACEChildTasksPreserved(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	cfg := createTestConfig()

	queue := NewQueue(db)

	// Create orphaned parent job
	payload := map[string]any{
		"source": "child task preservation test",
		"actor":  "test-actor",
	}
	payloadJSON, _ := json.Marshal(payload)
	parentJob, err := NewJobWithPayload("test.grace-handler", "parent with children", payloadJSON, 5, 0.01, "test-actor")
	if err != nil {
		t.Fatalf("Failed to create parent job: %v", err)
	}
	parentJob.Status = JobStatusRunning // Simulate crash
	if err := queue.store.CreateJob(parentJob); err != nil {
		t.Fatalf("Failed to store parent job: %v", err)
	}

	// Create child tasks (simulate tasks created before crash)
	for i := 1; i <= 3; i++ {
		childPayload := map[string]any{
			"source": fmt.Sprintf("scoring task %d", i),
			"actor":  "test-actor",
		}
		childPayloadJSON, _ := json.Marshal(childPayload)
		childTask, err := NewJobWithPayload("test.child-handler", fmt.Sprintf("scoring task %d", i), childPayloadJSON, 1, 0.001, "test-actor")
		if err != nil {
			t.Fatalf("Failed to create child task %d: %v", i, err)
		}
		childTask.ParentJobID = parentJob.ID
		childTask.Status = JobStatusQueued
		if err := queue.store.CreateJob(childTask); err != nil {
			t.Fatalf("Failed to store child task %d: %v", i, err)
		}
	}

	// Verify child tasks exist
	tasks, err := queue.ListTasksByParent(parentJob.ID)
	if err != nil {
		t.Fatalf("Failed to list child tasks: %v", err)
	}
	if len(tasks) != 3 {
		t.Fatalf("Expected 3 child tasks, got %d", len(tasks))
	}

	// Start worker pool (triggers orphan recovery)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wp := NewWorkerPoolWithContext(ctx, db, cfg, WorkerPoolConfig{Workers: 1}, zap.NewNop().Sugar())
	wp.Start()
	defer wp.Stop()

	time.Sleep(200 * time.Millisecond)

	// Verify parent job was marked failed
	recovered, err := queue.GetJob(parentJob.ID)
	if err != nil {
		t.Fatalf("Failed to get parent job after recovery: %v", err)
	}
	if recovered.Status != JobStatusFailed {
		t.Errorf("Expected parent job to be failed, got status '%s'", recovered.Status)
	}

	// Verify child tasks remain queued (not deleted or failed)
	for _, task := range tasks {
		checkTask, err := queue.GetJob(task.ID)
		if err != nil {
			t.Fatalf("Failed to get child task %s: %v", task.ID, err)
		}
		if checkTask.Status != JobStatusQueued {
			t.Errorf("Expected child task %s to stay queued, got '%s'", task.ID, checkTask.Status)
		}
	}
}

// Helper: GRACETestHandler implements JobHandler for GRACE shutdown testing
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

		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled before %s", taskName)
		default:
		}

		time.Sleep(h.taskDuration)
		h.tasksCompleted++
	}

	return nil
}

func (h *GRACETestHandler) Name() string {
	return "test.grace-handler"
}
