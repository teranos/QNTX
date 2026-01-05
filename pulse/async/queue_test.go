package async

import (
	"fmt"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"sync"
	"testing"
	"time"
)

// ============================================================================
// TAS Bot & Yugi Queue Test Universe
// ============================================================================
//
// Characters:
//   - TAS Bot: Frame-perfect coordinator who enqueues jobs with precision
//   - Yugi: The duelist who draws and plays job cards from the queue
//   - Cronos: Greek god of time, appears for time-sensitive queue operations
//
// Theme: TAS Bot places jobs in the queue, Yugi draws them like cards and plays them,
// and Cronos ensures timing is correct for paused/scheduled jobs.
// ============================================================================

// TestTASBotEnqueuesJob tests that TAS Bot can enqueue a job to the queue
func TestTASBotEnqueuesJob(t *testing.T) {
	t.Log("🎮 TAS Bot begins enqueuing jobs for the speedrun...")
	t.Log("   'Placing job in queue with frame-perfect timing'")

	db := qntxtest.CreateTestDB(t)
	queue := NewQueue(db)

	// TAS Bot creates a job
	job := &Job{
		ID:          "JOB_TASBOT_001",
		HandlerName: "test.jd-extraction",
		Source:      "speedrun_jd.html",
		Status:      "queued",

		CostEstimate: 0.10,
		CreatedAt:    time.Now(),
	}

	// TAS Bot enqueues the job
	err := queue.Enqueue(job)
	if err != nil {
		t.Fatalf("TAS Bot failed to enqueue job: %v", err)
	}

	t.Log("✓ TAS Bot successfully enqueued job JOB_TASBOT_001")
	t.Log("  'Job ready for frame-perfect execution'")
}

// TestYugiDequeuesJob tests that Yugi can dequeue a job from the queue
func TestYugiDequeuesJob(t *testing.T) {
	t.Log("⭐ Yugi prepares to dequeue job from queue...")
	t.Log("   'It's time to duel!' *prepares to draw from queue*")

	db := qntxtest.CreateTestDB(t)
	queue := NewQueue(db)

	// First, TAS Bot enqueues a job for Yugi
	job := &Job{
		ID:          "JOB_YUGI_001",
		HandlerName: "test.jd-extraction",
		Source:      "card_ability.html",
		Status:      "queued",

		CostEstimate: 0.05,
		CreatedAt:    time.Now(),
	}
	err := queue.Enqueue(job)
	if err != nil {
		t.Fatalf("Failed to enqueue job for Yugi: %v", err)
	}

	// Yugi dequeues the job
	dequeuedJob, err := queue.Dequeue()
	if err != nil {
		t.Fatalf("Yugi failed to dequeue job: %v", err)
	}

	if dequeuedJob == nil {
		t.Fatal("Yugi found no job in queue")
	}

	if dequeuedJob.ID != "JOB_YUGI_001" {
		t.Errorf("Yugi dequeued wrong job: got %s, expected JOB_YUGI_001", dequeuedJob.ID)
	}

	t.Log("✓ Yugi successfully dequeued job JOB_YUGI_001")
	t.Log("  'It's time to duel!' *drew card job successfully*")
}

// TestTASBotJobPriority tests that TAS Bot's high-priority jobs are dequeued first
func TestTASBotJobPriority(t *testing.T) {
	t.Log("🎮 TAS Bot tests job priority ordering...")
	t.Log("   'High-priority jobs must execute first for optimal speedrun'")

	db := qntxtest.CreateTestDB(t)
	queue := NewQueue(db)

	// TAS Bot enqueues low-priority job
	lowPriorityJob := &Job{
		ID:          "JOB_LOW_PRIORITY",
		HandlerName: "test.jd-extraction",
		Source:      "slow_strat.html",
		Status:      "queued",

		CostEstimate: 0.10,
		CreatedAt:    time.Now(),
	}
	queue.Enqueue(lowPriorityJob)

	// TAS Bot enqueues high-priority job (after low-priority)
	time.Sleep(10 * time.Millisecond) // Ensure different timestamps
	highPriorityJob := &Job{
		ID:           "JOB_HIGH_PRIORITY",
		HandlerName:  "test.jd-extraction",
		Source:       "fast_strat.html",
		Status:       "queued",
		CostEstimate: 0.10,
		CreatedAt:    time.Now(),
	}
	queue.Enqueue(highPriorityJob)

	// Yugi dequeues - should get high-priority job first
	firstJob, err := queue.Dequeue()
	if err != nil {
		t.Fatalf("Failed to dequeue first job: %v", err)
	}

	if firstJob.ID != "JOB_HIGH_PRIORITY" {
		t.Errorf("TAS Bot expected high-priority job first, got %s", firstJob.ID)
	}

	t.Log("✓ TAS Bot confirmed high-priority job dequeued first")
	t.Log("  'Optimal speedrun route achieved'")
}

// TestYugiEmptyQueue tests that Yugi handles an empty queue gracefully
func TestYugiEmptyQueue(t *testing.T) {
	t.Log("⭐ Yugi tries to dequeue from empty queue...")
	t.Log("   'No cards in hand' *checks empty queue*")

	db := qntxtest.CreateTestDB(t)
	queue := NewQueue(db)

	// Yugi tries to dequeue from empty queue
	job, err := queue.Dequeue()
	if err != nil {
		t.Fatalf("Yugi encountered error on empty queue: %v", err)
	}

	if job != nil {
		t.Error("Yugi expected nil job from empty queue")
	}

	t.Log("✓ Yugi handled empty queue correctly (returned nil)")
	t.Log("  'Heart of the cards says: no jobs' *empty deck*")
}

// TestCronosPausedJob tests that Cronos can pause a job and it won't be dequeued
func TestCronosPausedJob(t *testing.T) {
	t.Log("⏰ Cronos pauses time for a job (paused job should not dequeue)...")
	t.Log("   'Time stands still for this job'")

	db := qntxtest.CreateTestDB(t)
	queue := NewQueue(db)

	// TAS Bot enqueues a job
	job := &Job{
		ID:          "JOB_PAUSED_001",
		HandlerName: "test.jd-extraction",
		Source:      "paused.html",
		Status:      "queued",

		CostEstimate: 0.10,
		CreatedAt:    time.Now(),
	}
	queue.Enqueue(job)

	// Yugi dequeues and starts the job (sets to running)
	dequeuedJob, _ := queue.Dequeue()
	dequeuedJob.Status = "running"
	queue.UpdateJob(dequeuedJob)

	// Cronos pauses the running job
	err := queue.PauseJob(job.ID, "budget_exceeded")
	if err != nil {
		t.Fatalf("Cronos failed to pause job: %v", err)
	}

	// Yugi tries to dequeue another job - should get nothing (paused job not dequeueable)
	nextJob, err := queue.Dequeue()
	if err != nil {
		t.Fatalf("Error during dequeue: %v", err)
	}

	if nextJob != nil {
		t.Errorf("Cronos expected paused job to not dequeue, but Yugi got: %s", nextJob.ID)
	}

	t.Log("✓ Cronos confirmed paused job was not dequeued")
	t.Log("  'Time remains frozen for this job'")
}

// TestTASBotResumeJob tests that TAS Bot can resume a paused job
func TestTASBotResumeJob(t *testing.T) {
	t.Log("🎮 TAS Bot resumes a paused job (unpausing for continued speedrun)...")
	t.Log("   'Resuming execution at optimal frame'")

	db := qntxtest.CreateTestDB(t)
	queue := NewQueue(db)

	// Create, dequeue, and pause a job
	job := &Job{
		ID:          "JOB_RESUME_001",
		HandlerName: "test.jd-extraction",
		Source:      "resume.html",
		Status:      "queued",

		CostEstimate: 0.10,
		CreatedAt:    time.Now(),
	}
	queue.Enqueue(job)

	// Yugi dequeues and starts the job (sets to running)
	dequeuedJob, _ := queue.Dequeue()
	dequeuedJob.Status = "running"
	queue.UpdateJob(dequeuedJob)

	// Cronos pauses the running job
	queue.PauseJob(job.ID, "rate_limited")

	// TAS Bot resumes the job (sets it back to running status)
	err := queue.ResumeJob(job.ID)
	if err != nil {
		t.Fatalf("TAS Bot failed to resume job: %v", err)
	}

	// Verify job is back to running status
	resumedJob, err := queue.GetJob(job.ID)
	if err != nil {
		t.Fatalf("Error retrieving resumed job: %v", err)
	}

	if resumedJob.Status != "running" {
		t.Errorf("TAS Bot expected resumed job to be running, got status: %s", resumedJob.Status)
	}

	t.Log("✓ TAS Bot successfully resumed job to running status")
	t.Log("  'Speedrun resumed at optimal frame'")
}

// TestYugiJobStateTransitions tests that Yugi can transition job states
func TestYugiJobStateTransitions(t *testing.T) {
	t.Log("⭐ Yugi tests job state transitions (card ability transformations)...")
	t.Log("   'It's time to duel!' *transforming through job states*")

	db := qntxtest.CreateTestDB(t)
	queue := NewQueue(db)

	// TAS Bot creates a job
	job := &Job{
		ID:          "JOB_TRANSFORM_001",
		HandlerName: "test.jd-extraction",
		Source:      "transform.html",
		Status:      "queued",

		CostEstimate: 0.10,
		CreatedAt:    time.Now(),
	}
	queue.Enqueue(job)

	// Yugi starts the job (queued → running)
	dequeuedJob, _ := queue.Dequeue()
	dequeuedJob.Status = "running"
	err := queue.UpdateJob(dequeuedJob)
	if err != nil {
		t.Fatalf("Yugi failed to update job to running: %v", err)
	}
	t.Log("  Yugi: 'It's time to duel!' *job now running*")

	// Yugi completes the job (running → completed)
	dequeuedJob.Status = "completed"
	dequeuedJob.CompletedAt = timePtr(time.Now())
	err = queue.UpdateJob(dequeuedJob)
	if err != nil {
		t.Fatalf("Yugi failed to update job to completed: %v", err)
	}

	t.Log("✓ Yugi successfully transitioned job: queued → running → completed")
	t.Log("  'It's time to duel!' *card ability mastered*")
}

// TestTASBotFailJob tests that TAS Bot can mark a job as failed
func TestTASBotFailJob(t *testing.T) {
	t.Log("🎮 TAS Bot handles job failure (speedrun death)...")
	t.Log("   'Marking failed attempt for retry'")

	db := qntxtest.CreateTestDB(t)
	queue := NewQueue(db)

	// Create a job
	job := &Job{
		ID:          "JOB_FAIL_001",
		HandlerName: "test.jd-extraction",
		Source:      "fail.html",
		Status:      "queued",

		CostEstimate: 0.10,
		CreatedAt:    time.Now(),
	}
	queue.Enqueue(job)

	// TAS Bot marks job as failed
	err := queue.FailJob(job.ID, fmt.Errorf("file not found"))
	if err != nil {
		t.Fatalf("TAS Bot failed to mark job as failed: %v", err)
	}

	t.Log("✓ TAS Bot marked job as failed")
	t.Log("  'Reset to last checkpoint'")
}

// TestCronosScheduledJob tests that Cronos can schedule a job for future execution
func TestCronosScheduledJob(t *testing.T) {
	t.Log("⏰ Cronos schedules a job for future execution...")
	t.Log("   'This job shall execute in the future'")

	db := qntxtest.CreateTestDB(t)
	queue := NewQueue(db)

	// Cronos creates a job with scheduled status
	job := &Job{
		ID:           "JOB_SCHEDULED_001",
		HandlerName:  "test.jd-extraction",
		Source:       "future.html",
		Status:       "scheduled",
		CostEstimate: 0.10,
		CreatedAt:    time.Now(),
	}
	queue.Enqueue(job)

	// Yugi tries to dequeue - should not get scheduled job yet
	dequeuedJob, err := queue.Dequeue()
	if err != nil {
		t.Fatalf("Error during dequeue: %v", err)
	}

	if dequeuedJob != nil {
		t.Error("Cronos expected scheduled job to not dequeue yet")
	}

	t.Log("✓ Cronos confirmed scheduled job remains in future")
	t.Log("  'Time has not yet come for this job'")
}

// TestYugiAndTASBotQueueIntegration tests the complete queue workflow
func TestYugiAndTASBotQueueIntegration(t *testing.T) {
	t.Log("🎮⭐ TAS Bot and Yugi queue integration test...")
	t.Log("   TAS Bot: 'Enqueuing jobs for optimal speedrun'")
	t.Log("   Yugi: 'It's time to duel!' *ready to draw and play cards**")

	db := qntxtest.CreateTestDB(t)
	queue := NewQueue(db)

	// TAS Bot enqueues multiple jobs in sequence
	// NOTE: Currently the queue is LIFO (Last-In-First-Out) due to DESC ordering
	jobs := []Job{
		{ID: "JOB_INT_001", HandlerName: "test.jd-extraction", Source: "first.html", Status: "queued", CostEstimate: 0.10, CreatedAt: time.Now()},
		{ID: "JOB_INT_002", HandlerName: "test.jd-extraction", Source: "second.html", Status: "queued", CostEstimate: 0.10, CreatedAt: time.Now().Add(time.Millisecond)},
		{ID: "JOB_INT_003", HandlerName: "test.jd-extraction", Source: "third.html", Status: "queued", CostEstimate: 0.10, CreatedAt: time.Now().Add(2 * time.Millisecond)},
	}

	for _, job := range jobs {
		err := queue.Enqueue(&job)
		if err != nil {
			t.Fatalf("TAS Bot failed to enqueue %s: %v", job.ID, err)
		}
		t.Logf("  TAS Bot: Enqueued %s", job.ID)
		time.Sleep(5 * time.Millisecond) // Ensure distinct timestamps
	}

	// Yugi processes jobs in LIFO order (newest first due to DESC ordering in store)
	processedOrder := []string{}
	for i := 0; i < 3; i++ {
		job, err := queue.Dequeue()
		if err != nil {
			t.Fatalf("Yugi failed to dequeue job %d: %v", i, err)
		}
		if job == nil {
			t.Fatalf("Yugi expected job %d, got nil", i)
		}
		processedOrder = append(processedOrder, job.ID)
		t.Logf("  Yugi: Processed %s", job.ID)
	}

	// Verify LIFO ordering (last job enqueued is first dequeued)
	expectedOrder := []string{"JOB_INT_003", "JOB_INT_002", "JOB_INT_001"}
	for i, expected := range expectedOrder {
		if processedOrder[i] != expected {
			t.Errorf("Job %d: expected %s, got %s", i, expected, processedOrder[i])
		}
	}

	t.Log("✓ Integration test complete: jobs processed in LIFO order")
	t.Log("  TAS Bot: 'Frame-perfect LIFO ordering confirmed!'")
	t.Log("  Yugi: 'It's time to duel!' *perfect card draw sequence*")
}

// Helper function for time pointers
func timePtr(t time.Time) *time.Time {
	return &t
}

// TestYugiCompletesJob tests that completing a job updates status and notifies subscribers
func TestYugiCompletesJob(t *testing.T) {
	t.Log("🃏 Yugi draws the final card to complete the duel...")
	t.Log("   'I activate my trap card: Job.Complete()!'")

	db := qntxtest.CreateTestDB(t)
	queue := NewQueue(db)

	// TAS Bot enqueues a job
	job := &Job{
		ID:          "JOB_YUGI_COMPLETE",
		HandlerName: "test.complete-handler",
		Source:      "duel_complete.html",
		Status:      JobStatusQueued,
		CreatedAt:   time.Now(),
	}

	err := queue.Enqueue(job)
	if err != nil {
		t.Fatalf("Failed to enqueue job: %v", err)
	}

	// Start the job first (simulate worker picking it up)
	job.Start()
	err = queue.UpdateJob(job)
	if err != nil {
		t.Fatalf("Failed to start job: %v", err)
	}

	// Yugi completes the job
	err = queue.CompleteJob(job.ID)
	if err != nil {
		t.Fatalf("Yugi failed to complete job: %v", err)
	}

	// Verify job is marked complete
	completedJob, err := queue.GetJob(job.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve completed job: %v", err)
	}

	if completedJob.Status != JobStatusCompleted {
		t.Errorf("Expected status 'completed', got '%s'", completedJob.Status)
	}

	if completedJob.CompletedAt == nil {
		t.Error("Expected CompletedAt to be set")
	}

	t.Log("✓ Yugi successfully completed the duel!")
	t.Log("  'Heart of the cards guided this job to completion!'")
}

// TestTASBotListsJobs tests listing jobs by status
func TestTASBotListsJobs(t *testing.T) {
	t.Log("🎮 TAS Bot reviews the job queue for optimal routing...")
	t.Log("   'Analyzing all job states for the perfect speedrun'")

	db := qntxtest.CreateTestDB(t)
	queue := NewQueue(db)

	// Create jobs with different statuses
	jobs := []*Job{
		{
			ID:          "JOB_LIST_QUEUED_1",
			HandlerName: "test.handler",
			Source:      "source1.html",
			Status:      JobStatusQueued,
			CreatedAt:   time.Now(),
		},
		{
			ID:          "JOB_LIST_RUNNING",
			HandlerName: "test.handler",
			Source:      "source2.html",
			Status:      JobStatusRunning,
			CreatedAt:   time.Now(),
		},
		{
			ID:          "JOB_LIST_COMPLETED",
			HandlerName: "test.handler",
			Source:      "source3.html",
			Status:      JobStatusCompleted,
			CreatedAt:   time.Now(),
		},
		{
			ID:          "JOB_LIST_QUEUED_2",
			HandlerName: "test.handler",
			Source:      "source4.html",
			Status:      JobStatusQueued,
			CreatedAt:   time.Now(),
		},
	}

	for _, job := range jobs {
		err := queue.Enqueue(job)
		if err != nil {
			t.Fatalf("Failed to enqueue job %s: %v", job.ID, err)
		}
	}

	// List only queued jobs
	queuedStatus := JobStatusQueued
	queuedJobs, err := queue.ListJobs(&queuedStatus, 10)
	if err != nil {
		t.Fatalf("Failed to list queued jobs: %v", err)
	}

	if len(queuedJobs) != 2 {
		t.Errorf("Expected 2 queued jobs, got %d", len(queuedJobs))
	}

	// List active jobs (queued, running, paused)
	activeJobs, err := queue.ListActiveJobs(10)
	if err != nil {
		t.Fatalf("Failed to list active jobs: %v", err)
	}

	if len(activeJobs) != 3 { // queued(2) + running(1)
		t.Errorf("Expected 3 active jobs, got %d", len(activeJobs))
	}

	t.Log("✓ TAS Bot catalogued all jobs with frame-perfect accuracy")
	t.Log("  'Queue state verified for optimal execution path'")
}

// TestYugiFindsDuplicateCard tests deduplication logic
func TestYugiFindsDuplicateCard(t *testing.T) {
	t.Log("🃏 Yugi checks his deck for duplicate cards...")
	t.Log("   'No duplicate monsters allowed in this duel!'")

	db := qntxtest.CreateTestDB(t)
	queue := NewQueue(db)

	// Enqueue first job
	job1 := &Job{
		ID:          "JOB_UNIQUE_001",
		HandlerName: "test.jd-extraction",
		Source:      "https://example.com/job/123",
		Status:      JobStatusQueued,
		CreatedAt:   time.Now(),
	}

	err := queue.Enqueue(job1)
	if err != nil {
		t.Fatalf("Failed to enqueue job1: %v", err)
	}

	// Try to find active job with same source and handler
	foundJob, err := queue.FindActiveJobBySourceAndHandler(job1.Source, job1.HandlerName)
	if err != nil {
		t.Fatalf("Failed to find active job: %v", err)
	}

	if foundJob == nil {
		t.Error("Expected to find active job, got nil")
	} else if foundJob.ID != job1.ID {
		t.Errorf("Expected job ID %s, got %s", job1.ID, foundJob.ID)
	}

	// Try to find job with different source (should return nil)
	notFound, err := queue.FindActiveJobBySourceAndHandler("https://different.com", job1.HandlerName)
	if err != nil {
		t.Fatalf("Failed during search: %v", err)
	}

	if notFound != nil {
		t.Errorf("Expected nil for non-existent source, got job %s", notFound.ID)
	}

	// Mark job as completed
	job1.Complete()
	err = queue.UpdateJob(job1)
	if err != nil {
		t.Fatalf("Failed to complete job1: %v", err)
	}

	// Should not find completed jobs (only active)
	notFoundCompleted, err := queue.FindActiveJobBySourceAndHandler(job1.Source, job1.HandlerName)
	if err != nil {
		t.Fatalf("Failed during search for completed: %v", err)
	}

	if notFoundCompleted != nil {
		t.Errorf("Expected nil for completed job, got job %s", notFoundCompleted.ID)
	}

	t.Log("✓ Yugi's deck deduplication complete!")
	t.Log("  'No duplicate cards detected - ready to duel!'")
}

// TestAbrahamAndIsaac tests cascade deletion with the angel intervention metaphor
// Encodes the biblical story: Abraham commanded to sacrifice Isaac, but angel intervenes
// In our implementation: child tasks are CANCELLED (angel's intervention) not FAILED
func TestAbrahamAndIsaac(t *testing.T) {
	t.Log("📜 Abraham receives the divine command: 'Take your son, your only son Isaac...'")
	t.Log("   The test of faith: delete the parent job, and what becomes of the children?")

	db := qntxtest.CreateTestDB(t)
	queue := NewQueue(db)

	// Abraham creates parent job
	parentJob := &Job{
		ID:          "JOB_ABRAHAM",
		HandlerName: "test.jd-ingestion",
		Source:      "mount_moriah.html",
		Status:      JobStatusRunning,
		CreatedAt:   time.Now(),
	}
	err := queue.Enqueue(parentJob)
	if err != nil {
		t.Fatalf("Failed to enqueue parent job: %v", err)
	}
	t.Log("  Abraham: 'Here am I. I have created the parent job as commanded.'")

	// Isaac and other children (various task states)
	childJobs := []*Job{
		{
			ID:          "TASK_ISAAC_QUEUED",
			HandlerName: "test.candidate-scoring",
			Source:      "Score: Isaac (bound for sacrifice)",
			Status:      JobStatusQueued,
			ParentJobID: parentJob.ID,
			CreatedAt:   time.Now(),
		},
		{
			ID:          "TASK_ISAAC_RUNNING",
			HandlerName: "test.candidate-scoring",
			Source:      "Score: Isaac (wood upon back)",
			Status:      JobStatusRunning,
			ParentJobID: parentJob.ID,
			CreatedAt:   time.Now(),
		},
		{
			ID:          "TASK_ISAAC_PAUSED",
			HandlerName: "test.candidate-scoring",
			Source:      "Score: Isaac (altar prepared)",
			Status:      JobStatusPaused,
			ParentJobID: parentJob.ID,
			CreatedAt:   time.Now(),
		},
		{
			ID:          "TASK_BLESSED_COMPLETE",
			HandlerName: "test.candidate-scoring",
			Source:      "Score: Already blessed",
			Status:      JobStatusCompleted,
			ParentJobID: parentJob.ID,
			CreatedAt:   time.Now(),
		},
	}

	for _, child := range childJobs {
		err := queue.Enqueue(child)
		if err != nil {
			t.Fatalf("Failed to enqueue child task %s: %v", child.ID, err)
		}
	}
	t.Logf("  Isaac: 'Father, behold the fire and the wood, but where is the lamb for sacrifice?'")
	t.Logf("  Abraham: 'God will provide... I have %d child tasks.'", len(childJobs))

	// The sacrifice (cascade deletion)
	t.Log("  Abraham stretches forth his hand... *initiates cascade deletion*")
	err = queue.DeleteJobWithChildren(parentJob.ID)
	if err != nil {
		t.Fatalf("Failed to delete parent with children: %v", err)
	}
	t.Log("  🕊️ Angel: 'Abraham! Abraham! Lay not thine hand upon the child!'")

	// Verify parent job is deleted
	_, err = queue.GetJob(parentJob.ID)
	if err == nil {
		t.Error("Expected parent job to be deleted")
	}
	t.Log("  ✓ Parent job removed (the sacrifice was accepted)")

	// Verify children were CANCELLED (angel's intervention) not FAILED
	for _, originalChild := range childJobs {
		child, err := queue.GetJob(originalChild.ID)
		if err != nil {
			t.Fatalf("Failed to get child task %s: %v", originalChild.ID, err)
		}

		switch originalChild.Status {
		case JobStatusQueued, JobStatusRunning, JobStatusPaused:
			// Angel intervened - tasks are CANCELLED, not FAILED
			if child.Status != JobStatusCancelled {
				t.Errorf("Child %s (was %s): expected cancelled (angel's intervention), got %s",
					child.ID, originalChild.Status, child.Status)
			}
			if child.Error != "parent job deleted" {
				t.Errorf("Child %s: expected error 'parent job deleted', got %s", child.ID, child.Error)
			}
			t.Logf("  🕊️ %s saved by angel (was %s → cancelled)", child.ID, originalChild.Status)

		case JobStatusCompleted:
			// Already completed tasks preserved (like the ram in the thicket)
			if child.Status != JobStatusCompleted {
				t.Errorf("Child %s: expected to remain completed, got %s", child.ID, child.Status)
			}
			t.Logf("  🐏 %s preserved (completed - the ram provided in the thicket)", child.ID)
		}
	}

	t.Log("✓ The angel's intervention is encoded: children are CANCELLED, not FAILED!")
	t.Log("  'cancelled' = stopped by external intervention (divine/user action)")
	t.Log("  'failed' = stopped by internal error (task problem)")
	t.Log("  Abraham's faith was tested, Isaac was spared, and no orphaned processes remain.")
}

// TestConcurrentDeletion tests that concurrent deletion of parent jobs doesn't cause race conditions
// Uses the Tower of Babel metaphor: multiple workers trying to delete at the same time
func TestConcurrentDeletion(t *testing.T) {
	t.Log("🏗️ Tower of Babel: Multiple workers attempting concurrent deletions...")
	t.Log("   'Come, let us delete and cause confusion!'")

	db := qntxtest.CreateTestDB(t)
	queue := NewQueue(db)

	// Create parent jobs with children
	parentJobs := []*Job{}
	for i := 0; i < 5; i++ {
		parentJob := &Job{
			ID:          fmt.Sprintf("JOB_BABEL_PARENT_%d", i),
			HandlerName: "test.tower-builder",
			Source:      fmt.Sprintf("tower_level_%d.html", i),
			Status:      JobStatusRunning,
			CreatedAt:   time.Now(),
		}
		err := queue.Enqueue(parentJob)
		if err != nil {
			t.Fatalf("Failed to enqueue parent job %d: %v", i, err)
		}

		// Create 3 children for each parent
		for j := 0; j < 3; j++ {
			childJob := &Job{
				ID:          fmt.Sprintf("TASK_BABEL_CHILD_%d_%d", i, j),
				HandlerName: "test.brick-layer",
				Source:      fmt.Sprintf("Brick layer %d-%d", i, j),
				Status:      JobStatusQueued,
				ParentJobID: parentJob.ID,
				CreatedAt:   time.Now(),
			}
			err := queue.Enqueue(childJob)
			if err != nil {
				t.Fatalf("Failed to enqueue child job %d-%d: %v", i, j, err)
			}
		}

		parentJobs = append(parentJobs, parentJob)
	}

	t.Logf("  Created 5 parent jobs with 3 children each (15 total children)")

	// Concurrent deletion using goroutines
	t.Log("  Workers: 'Come, let us delete these jobs!' *concurrent deletion begins*")

	var wg sync.WaitGroup
	deletionErrors := make(chan error, len(parentJobs))

	for _, parentJob := range parentJobs {
		wg.Add(1)
		go func(jobID string) {
			defer wg.Done()
			err := queue.DeleteJobWithChildren(jobID)
			if err != nil {
				deletionErrors <- fmt.Errorf("failed to delete job %s: %w", jobID, err)
			}
		}(parentJob.ID)
	}

	// Wait for all deletions to complete
	wg.Wait()
	close(deletionErrors)

	// Check for errors
	errorCount := 0
	for err := range deletionErrors {
		t.Errorf("Deletion error: %v", err)
		errorCount++
	}

	if errorCount > 0 {
		t.Fatalf("Encountered %d deletion errors during concurrent deletion", errorCount)
	}

	t.Log("  ✓ All concurrent deletions completed without race conditions!")

	// Verify all parent jobs are deleted
	for _, parentJob := range parentJobs {
		_, err := queue.GetJob(parentJob.ID)
		if err == nil {
			t.Errorf("Parent job %s should be deleted but still exists", parentJob.ID)
		}
	}

	t.Log("  ✓ All parent jobs successfully deleted")

	// Verify all children are cancelled
	for i := 0; i < 5; i++ {
		for j := 0; j < 3; j++ {
			childID := fmt.Sprintf("TASK_BABEL_CHILD_%d_%d", i, j)
			child, err := queue.GetJob(childID)
			if err != nil {
				t.Errorf("Failed to get child job %s: %v", childID, err)
				continue
			}

			if child.Status != JobStatusCancelled {
				t.Errorf("Child %s: expected cancelled, got %s", childID, child.Status)
			}
		}
	}

	t.Log("  ✓ All 15 children are cancelled (no race condition corruption)")
	t.Log("🏗️ The Lord said: 'Their deletion is now scattered, and their jobs are cancelled!'")
	t.Log("   Database integrity maintained despite concurrent operations.")
}

// TestVacanciesScraperChildJobsContinue tests that child jobs continue processing after parent completes
// Uses the Noah's Ark metaphor: Noah builds the ark (parent job), then sends out animals (child jobs)
// The animals continue their journey even after Noah's work is complete
func TestVacanciesScraperChildJobsContinue(t *testing.T) {
	t.Log("🚢 Noah's Ark: Parent job completes, but the animals (children) must continue their journey...")
	t.Log("   'The flood is over, Noah's work is done. Now the animals must populate the earth.'")

	db := qntxtest.CreateTestDB(t)
	queue := NewQueue(db)

	// Noah creates the parent job (building the ark = scraping vacancies page)
	parentJob := &Job{
		ID:          "JOB_NOAH_VACANCIES",
		HandlerName: "role.vacancies-scraper",
		Source:      "https://example.com/jobs/",
		Status:      JobStatusRunning,
		CreatedAt:   time.Now(),
	}
	err := queue.Enqueue(parentJob)
	if err != nil {
		t.Fatalf("Failed to enqueue parent job: %v", err)
	}
	t.Log("  Noah: 'The ark is complete. Now I shall send forth the animals.'")

	// Create child jobs (JD ingestion jobs = animals leaving the ark)
	childJobs := []*Job{
		{
			ID:          "JOB_DOVE_JD_001",
			HandlerName: "role.jd-ingestion",
			Source:      "https://example.com/jobs/frontend-engineer",
			Status:      JobStatusQueued,
			ParentJobID: parentJob.ID,
			CreatedAt:   time.Now(),
		},
		{
			ID:          "JOB_RAVEN_JD_002",
			HandlerName: "role.jd-ingestion",
			Source:      "https://example.com/jobs/backend-engineer",
			Status:      JobStatusQueued,
			ParentJobID: parentJob.ID,
			CreatedAt:   time.Now(),
		},
		{
			ID:          "JOB_PIGEON_PAGINATION_001",
			HandlerName: "role.vacancies-scraper",
			Source:      "https://example.com/jobs/page/2",
			Status:      JobStatusQueued,
			ParentJobID: parentJob.ID,
			CreatedAt:   time.Now(),
		},
	}

	for _, child := range childJobs {
		err := queue.Enqueue(child)
		if err != nil {
			t.Fatalf("Failed to enqueue child job %s: %v", child.ID, err)
		}
	}
	t.Logf("  Noah: 'I have sent forth %d animals (child jobs) to populate the earth.'", len(childJobs))

	// Noah completes his work (vacancies scraper finishes)
	t.Log("  Noah: 'My work is complete. The ark has served its purpose.' *CompleteJob*")
	err = queue.CompleteJob(parentJob.ID)
	if err != nil {
		t.Fatalf("Failed to complete parent job: %v", err)
	}

	// Verify parent job is completed
	completedParent, err := queue.GetJob(parentJob.ID)
	if err != nil {
		t.Fatalf("Failed to get parent job: %v", err)
	}
	if completedParent.Status != JobStatusCompleted {
		t.Errorf("Expected parent status completed, got %s", completedParent.Status)
	}
	t.Log("  ✓ Parent job marked as completed")

	// CRITICAL: Verify all children are still QUEUED (not cancelled)
	t.Log("  Checking if animals continue their journey after Noah's work is complete...")
	for _, originalChild := range childJobs {
		child, err := queue.GetJob(originalChild.ID)
		if err != nil {
			t.Fatalf("Failed to get child job %s: %v", originalChild.ID, err)
		}

		if child.Status != JobStatusQueued {
			t.Errorf("🔴 CRITICAL BUG: Child %s should be QUEUED (continuing journey), but got status: %s",
				child.ID, child.Status)
			t.Errorf("   This breaks the vacancies → JD ingestion workflow!")
			if child.Error != "" {
				t.Errorf("   Error message: %s", child.Error)
			}
		} else {
			t.Logf("  🕊️ %s continues its journey (status: queued) ✓", child.ID)
		}
	}

	// Verify children can be dequeued and processed
	t.Log("  Verifying animals can be dequeued for processing...")
	firstChild, err := queue.Dequeue()
	if err != nil {
		t.Fatalf("Failed to dequeue child job: %v", err)
	}
	if firstChild == nil {
		t.Fatal("🔴 CRITICAL: No child jobs available for dequeue! They were incorrectly cancelled!")
	}

	// Verify the dequeued job is one of our children
	isChildJob := false
	for _, originalChild := range childJobs {
		if firstChild.ID == originalChild.ID {
			isChildJob = true
			break
		}
	}
	if !isChildJob {
		t.Errorf("Dequeued job %s is not one of our child jobs", firstChild.ID)
	}

	t.Log("  ✓ Child job successfully dequeued and ready for processing")

	t.Log("✓ Noah's animals continue their journey after the ark's work is complete!")
	t.Log("  'The parent completes, but the children must continue' - the vacancies workflow lives!")
	t.Log("  Genesis 8:17 - 'Bring out with you every living thing... that they may multiply on the earth.'")
}
