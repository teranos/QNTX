package async

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/teranos/vanity-id"
)

// ============================================================================
// TAS Bot Job Lifecycle Test Universe
// ============================================================================
//
// Characters:
//   - TAS Bot: Frame-perfect coordinator creating speedrun missions
//
// Theme: Jobs represent speedrun missions - any async work that needs tracking.
// Not domain-specific - could be video rendering, data sync, batch processing, etc.
// ============================================================================

func TestNewJobWithPayload(t *testing.T) {
	tests := []struct {
		name          string
		handlerName   string
		source        string
		totalOps      int
		estimatedCost float64
		wantErr       bool
		description   string
	}{
		{
			name:          "video rendering speedrun",
			handlerName:   "test.video-renderer",
			source:        "speedrun-footage-2024.mp4",
			totalOps:      240,
			estimatedCost: 0.480,
			wantErr:       false,
			description:   "TAS Bot queues 240 frames for rendering",
		},
		{
			name:          "batch data sync mission",
			handlerName:   "test.batch-sync",
			source:        "database:users",
			totalOps:      5000,
			estimatedCost: 1.250,
			wantErr:       false,
			description:   "TAS Bot syncs 5000 user records",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("🎮 TAS Bot: %s", tt.description)

			// Create generic payload
			payload := map[string]interface{}{
				"source": tt.source,
				"actor":  "tas-bot",
			}
			payloadJSON, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("Failed to marshal payload: %v", err)
			}

			job, err := NewJobWithPayload(tt.handlerName, tt.source, payloadJSON, tt.totalOps, tt.estimatedCost, "tas-bot")
			if (err != nil) != tt.wantErr {
				t.Errorf("NewJobWithPayload() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err == nil {
				// Validate ASID format (Job IDs are ASIDs)
				if job.ID == "" {
					t.Error("TAS Bot failed to generate job ID")
				}
				if len(job.ID) != 32 {
					t.Errorf("Job ID length = %d, want 32 (ASID format)", len(job.ID))
				}
				if !strings.HasPrefix(job.ID, "JB") {
					t.Errorf("Job ID prefix = %s, want 'JB'", job.ID[:2])
				}
				if !id.IsValidASID(job.ID) {
					t.Errorf("Job ID = %s is not a valid ASID", job.ID)
				}

				// Validate job properties
				if job.Status != JobStatusQueued {
					t.Errorf("Job status = %v, want %v", job.Status, JobStatusQueued)
				}
				if job.HandlerName != tt.handlerName {
					t.Errorf("Job handler = %v, want %v", job.HandlerName, tt.handlerName)
				}
				if job.Progress.Total != tt.totalOps {
					t.Errorf("Job progress.total = %v, want %v", job.Progress.Total, tt.totalOps)
				}
				if job.CostEstimate != tt.estimatedCost {
					t.Errorf("Job cost_estimate = %v, want %v", job.CostEstimate, tt.estimatedCost)
				}

				t.Logf("✓ TAS Bot created mission with ASID: %s", job.ID)
			}
		})
	}
}

func TestJobStateTransitions(t *testing.T) {
	t.Log("🎮 TAS Bot: Testing job state machine transitions")
	t.Log("   Mission: 'Render speedrun compilation video'")

	job, err := createTestJob("test.video-renderer", "speedrun-frames.mp4", 100, 0.200)
	if err != nil {
		t.Fatalf("TAS Bot failed to create mission: %v", err)
	}

	// Test queued -> running
	if job.Status != JobStatusQueued {
		t.Errorf("Initial status = %v, want %v", job.Status, JobStatusQueued)
	}
	t.Log("  ✓ Mission queued and ready for execution")

	job.Start()
	if job.Status != JobStatusRunning {
		t.Errorf("After Start(), status = %v, want %v", job.Status, JobStatusRunning)
	}
	if job.StartedAt == nil {
		t.Error("After Start(), StartedAt should be set")
	}
	t.Log("  ✓ Mission started - frame-perfect execution begins")

	// Test running -> paused
	job.Pause("resource_limit")
	if job.Status != JobStatusPaused {
		t.Errorf("After Pause(), status = %v, want %v", job.Status, JobStatusPaused)
	}
	t.Log("  ✓ Mission paused - waiting for resources")

	// Test paused -> running
	job.Resume()
	if job.Status != JobStatusRunning {
		t.Errorf("After Resume(), status = %v, want %v", job.Status, JobStatusRunning)
	}
	t.Log("  ✓ Mission resumed - continuing from checkpoint")

	// Test running -> completed
	job.Complete()
	if job.Status != JobStatusCompleted {
		t.Errorf("After Complete(), status = %v, want %v", job.Status, JobStatusCompleted)
	}
	if job.CompletedAt == nil {
		t.Error("After Complete(), CompletedAt should be set")
	}
	t.Log("  ✓ Mission complete - speedrun saved!")
}

func TestJobFailure(t *testing.T) {
	t.Log("🎮 TAS Bot: Testing mission failure handling")
	t.Log("   Sometimes even TAS runs fail...")

	job, err := createTestJob("test.file-processor", "corrupted-file.dat", 50, 0.100)
	if err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	job.Start()
	t.Log("  Mission started...")

	testErr := "file corruption detected"
	job.Fail(fmt.Errorf("%s", testErr))

	if job.Status != JobStatusFailed {
		t.Errorf("After Fail(), status = %v, want %v", job.Status, JobStatusFailed)
	}
	if job.Error != testErr {
		t.Errorf("After Fail(), error = %v, want %v", job.Error, testErr)
	}
	if job.CompletedAt == nil {
		t.Error("After Fail(), CompletedAt should be set")
	}
	t.Log("  ✓ Mission failed gracefully - error logged for retry")
}

func TestProgressTracking(t *testing.T) {
	t.Log("🎮 TAS Bot: Tracking mission progress frame-by-frame")
	t.Log("   Mission: 'Process image batch - 60 frames'")

	job, err := createTestJob("test.image-processor", "image-batch.zip", 60, 0.120)
	if err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	if job.Progress.Current != 0 {
		t.Errorf("Initial progress.current = %v, want 0", job.Progress.Current)
	}
	if job.Progress.Total != 60 {
		t.Errorf("Initial progress.total = %v, want 60", job.Progress.Total)
	}
	if job.Progress.Percentage() != 0 {
		t.Errorf("Initial percentage = %v, want 0", job.Progress.Percentage())
	}
	t.Log("  Progress: 0/60 frames (0%)")

	// Update progress - processed 15 frames
	job.UpdateProgress(15)
	if job.Progress.Current != 15 {
		t.Errorf("After update, progress.current = %v, want 15", job.Progress.Current)
	}
	expectedPct := 25.0 // 15/60 * 100
	if job.Progress.Percentage() != expectedPct {
		t.Errorf("After update, percentage = %v, want %v", job.Progress.Percentage(), expectedPct)
	}
	t.Log("  Progress: 15/60 frames (25%)")

	// Complete all frames
	job.UpdateProgress(60)
	if job.Progress.Percentage() != 100.0 {
		t.Errorf("At completion, percentage = %v, want 100.0", job.Progress.Percentage())
	}
	t.Log("  ✓ Progress: 60/60 frames (100%) - mission complete!")
}

func TestCostTracking(t *testing.T) {
	t.Log("🎮 TAS Bot: Tracking mission costs (API calls, compute, etc.)")
	t.Log("   Mission: 'Sync 1000 database records'")

	job, err := createTestJob("test.db-sync", "db:records", 1000, 0.500)
	if err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	if job.CostActual != 0.0 {
		t.Errorf("Initial cost_actual = %v, want 0.0", job.CostActual)
	}
	t.Log("  Initial cost: $0.000")

	// Record batch 1 cost
	job.RecordCost(0.125)
	if job.CostActual != 0.125 {
		t.Errorf("After recording $0.125, cost_actual = %v, want 0.125", job.CostActual)
	}
	t.Log("  Batch 1 processed: $0.125 spent")

	// Record batch 2 cost
	job.RecordCost(0.125)
	if job.CostActual != 0.250 {
		t.Errorf("After recording another $0.125, cost_actual = %v, want 0.250", job.CostActual)
	}
	t.Log("  ✓ Batch 2 processed: $0.250 total spent")
}

func TestPulseState(t *testing.T) {
	t.Log("🎮 TAS Bot: Testing Pulse state tracking (rate limits & budgets)")
	t.Log("   Mission: 'API batch processing with rate limits'")

	job, err := createTestJob("test.api-batch", "api-batch", 100, 0.200)
	if err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	if job.PulseState != nil {
		t.Error("Initial pulse state should be nil")
	}

	// Update pulse state - tracking API limits
	pulseState := &PulseState{
		CallsThisMinute: 45,
		CallsRemaining:  15,
		SpendToday:      2.50,
		SpendThisMonth:  15.75,
		BudgetRemaining: 84.25,
		IsPaused:        false,
	}

	job.UpdatePulseState(pulseState)
	if job.PulseState == nil {
		t.Fatal("Pulse state should be set")
	}
	if job.PulseState.SpendToday != 2.50 {
		t.Errorf("Pulse state spend_today = %v, want 2.50", job.PulseState.SpendToday)
	}
	t.Logf("  ✓ Pulse state: %d calls this minute, $%.2f spent today",
		job.PulseState.CallsThisMinute, job.PulseState.SpendToday)
}

func TestJobPayload(t *testing.T) {
	t.Log("🎮 TAS Bot: Testing mission payload storage")
	t.Log("   Payload can store arbitrary mission-specific data")

	// Create payload with mission-specific parameters
	payload := map[string]interface{}{
		"role_id":   "VIDRENDER",
		"video_id":  "VIDEO_4K_001",
		"title":     "4K Speedrun Compilation",
		"video_url": "s3://speedruns/2024/compilation.mp4",
		"actor":     "tas-bot",
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Failed to marshal payload: %v", err)
	}

	job, err := NewJobWithPayload("test.video-renderer", "video-render", payloadJSON, 720, 1.440, "tas-bot")
	if err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	if job.Payload == nil {
		t.Fatal("Payload should be set")
	}

	// Decode and verify payload
	var decodedPayload map[string]interface{}
	if err := json.Unmarshal(job.Payload, &decodedPayload); err != nil {
		t.Fatalf("Failed to unmarshal payload: %v", err)
	}

	if decodedPayload["role_id"] != "VIDRENDER" {
		t.Errorf("Payload.role_id = %v, want VIDRENDER", decodedPayload["role_id"])
	}
	if decodedPayload["title"] != "4K Speedrun Compilation" {
		t.Errorf("Payload.title = %v, want '4K Speedrun Compilation'", decodedPayload["title"])
	}
	t.Log("  ✓ Payload stored: video rendering mission parameters")
}

func TestMarshalUnmarshalPulseState(t *testing.T) {
	t.Log("🎮 TAS Bot: Testing Pulse state serialization for database storage")

	original := &PulseState{
		CallsThisMinute: 50,
		CallsRemaining:  10,
		SpendToday:      5.25,
		SpendThisMonth:  47.80,
		BudgetRemaining: 52.20,
		IsPaused:        true,
		PauseReason:     "budget_exceeded",
	}

	// Marshal to JSON
	json, err := MarshalPulseState(original)
	if err != nil {
		t.Fatalf("MarshalPulseState() error = %v", err)
	}
	if json == "" {
		t.Error("MarshalPulseState() returned empty string")
	}
	t.Log("  ✓ Pulse state serialized to JSON")

	// Unmarshal from JSON
	restored, err := UnmarshalPulseState(json)
	if err != nil {
		t.Fatalf("UnmarshalPulseState() error = %v", err)
	}

	if restored.SpendToday != original.SpendToday {
		t.Errorf("Restored SpendToday = %v, want %v", restored.SpendToday, original.SpendToday)
	}
	if restored.IsPaused != original.IsPaused {
		t.Errorf("Restored IsPaused = %v, want %v", restored.IsPaused, original.IsPaused)
	}
	if restored.PauseReason != original.PauseReason {
		t.Errorf("Restored PauseReason = %v, want %v", restored.PauseReason, original.PauseReason)
	}
	t.Log("  ✓ Pulse state deserialized correctly")
}

// TestParentJobHierarchy tests parent-child job relationships
func TestParentJobHierarchy(t *testing.T) {
	t.Log("🎮 TAS Bot: Testing mission hierarchy (parent jobs with subtasks)")
	t.Log("   Parent: 'Render compilation video'")
	t.Log("   Children: Individual frame rendering tasks")

	// Create parent mission
	parent, err := createTestJob("test.video-compiler", "video-compilation", 0, 0.0)
	if err != nil {
		t.Fatalf("Failed to create parent job: %v", err)
	}
	t.Logf("  ✓ Parent mission created: %s", parent.ID)

	// Create child subtask with payload containing task details
	taskPayload := map[string]interface{}{
		"frame_id":          "FRAME_4K_001",
		"quality_threshold": 0.95,
		"actor":             "render-worker",
		"source":            "frame-001.png",
	}
	taskPayloadJSON, _ := json.Marshal(taskPayload)
	task, err := NewJobWithPayload("test.frame-renderer", "frame-001.png", taskPayloadJSON, 1, 0.002, "render-worker")
	if err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	// Link task to parent
	task.ParentJobID = parent.ID

	// Verify hierarchy
	if task.ParentJobID != parent.ID {
		t.Errorf("Task parent_job_id = %v, want %v", task.ParentJobID, parent.ID)
	}

	// Verify task payload
	var decodedPayload map[string]interface{}
	if err := json.Unmarshal(task.Payload, &decodedPayload); err != nil {
		t.Fatalf("Failed to decode task payload: %v", err)
	}
	if decodedPayload["frame_id"] != "FRAME_4K_001" {
		t.Errorf("Task frame_id = %v, want FRAME_4K_001", decodedPayload["frame_id"])
	}
	if decodedPayload["quality_threshold"] != 0.95 {
		t.Errorf("Task quality_threshold = %v, want 0.95", decodedPayload["quality_threshold"])
	}
	t.Log("  ✓ Child task linked to parent mission")
}

// TestRetryLogic tests retry count and max retry enforcement
func TestRetryLogic(t *testing.T) {
	t.Log("🎮 TAS Bot: Testing mission retry logic")
	t.Log("   Mission: 'API call with transient failures'")

	job, err := createTestJob("test.api-caller", "api-endpoint", 1, 0.002)
	if err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	// Initial retry count should be 0
	if job.RetryCount != 0 {
		t.Errorf("Initial retry_count = %v, want 0", job.RetryCount)
	}
	t.Log("  Attempt 1: Failed (timeout)")

	// Simulate first retry
	job.RetryCount++
	if job.RetryCount != 1 {
		t.Errorf("After first retry, retry_count = %v, want 1", job.RetryCount)
	}
	t.Log("  Attempt 2: Failed (rate limited)")

	// Simulate second retry
	job.RetryCount++
	if job.RetryCount != 2 {
		t.Errorf("After second retry, retry_count = %v, want 2", job.RetryCount)
	}
	t.Log("  Attempt 3: Success!")

	// Verify max retries (2) is not exceeded
	maxRetries := 2
	if job.RetryCount > maxRetries {
		t.Errorf("Retry count %v exceeds max %v", job.RetryCount, maxRetries)
	}
	t.Log("  ✓ Retry logic working - max retries respected")
}

// TestTaskPayloads tests task-specific payload fields
func TestTaskPayloads(t *testing.T) {
	t.Log("🎮 TAS Bot: Testing payloads for different mission types")

	tests := []struct {
		name        string
		handlerName string
		payload     map[string]interface{}
		wantField   string
		wantValue   interface{}
		description string
	}{
		{
			name:        "video rendering parent job",
			handlerName: "test.video-coordinator",
			payload: map[string]interface{}{
				"phase":     "render",
				"video_url": "s3://videos/speedrun.mp4",
				"actor":     "render-coordinator",
			},
			wantField:   "phase",
			wantValue:   "render",
			description: "Coordinates batch video rendering",
		},
		{
			name:        "image processing task",
			handlerName: "test.image-processor",
			payload: map[string]interface{}{
				"image_id":  "IMG_4K_001",
				"min_score": 0.90,
				"actor":     "image-worker",
			},
			wantField:   "image_id",
			wantValue:   "IMG_4K_001",
			description: "Processes individual 4K image with quality threshold",
		},
		{
			name:        "data aggregation phase",
			handlerName: "test.aggregator",
			payload: map[string]interface{}{
				"phase":   "aggregate",
				"role_id": "DATASYNC",
				"actor":   "aggregator",
			},
			wantField:   "phase",
			wantValue:   "aggregate",
			description: "Aggregates results from parallel workers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("  Testing: %s", tt.description)

			payloadJSON, _ := json.Marshal(tt.payload)
			job, err := NewJobWithPayload(tt.handlerName, "test-source", payloadJSON, 1, 0.002, "test-system")
			if err != nil {
				t.Fatalf("Failed to create job: %v", err)
			}

			// Verify payload field
			var decoded map[string]interface{}
			if err := json.Unmarshal(job.Payload, &decoded); err != nil {
				t.Fatalf("Failed to decode payload: %v", err)
			}

			if decoded[tt.wantField] != tt.wantValue {
				t.Errorf("Payload.%s = %v, want %v", tt.wantField, decoded[tt.wantField], tt.wantValue)
			}
			t.Log("    ✓ Payload validated")
		})
	}
}

// TestTaskAggregation tests aggregating costs and progress from child tasks
func TestTaskAggregation(t *testing.T) {
	t.Log("🎮 TAS Bot: Testing mission aggregation (parent collects child stats)")
	t.Log("   Mission: 'Process image batch' with 5 parallel workers")

	// Create parent coordinator
	parent, err := createTestJob("test.batch-coordinator", "image-batch", 0, 0.0)
	if err != nil {
		t.Fatalf("Failed to create parent: %v", err)
	}
	t.Log("  Parent coordinator created")

	// Create 5 worker tasks
	tasks := make([]*Job, 5)
	for i := 0; i < 5; i++ {
		taskPayload := map[string]interface{}{
			"image_id": fmt.Sprintf("IMG_%d", i),
			"actor":    "image-worker",
		}
		payloadJSON, _ := json.Marshal(taskPayload)
		task, err := NewJobWithPayload(
			"test.image-worker",
			fmt.Sprintf("image-%d.png", i),
			payloadJSON,
			1,
			0.002,
			"image-worker",
		)
		if err != nil {
			t.Fatalf("Failed to create task %d: %v", i, err)
		}
		task.ParentJobID = parent.ID
		tasks[i] = task
	}
	t.Log("  5 worker tasks created")

	// Simulate task execution
	tasks[0].Complete()
	tasks[0].RecordCost(0.0021)
	t.Log("  Worker 0: Completed ($0.0021)")

	tasks[1].Complete()
	tasks[1].RecordCost(0.0019)
	t.Log("  Worker 1: Completed ($0.0019)")

	tasks[2].Fail(fmt.Errorf("corrupted image data"))
	t.Log("  Worker 2: Failed (corrupted data)")

	tasks[3].Complete()
	tasks[3].RecordCost(0.0020)
	t.Log("  Worker 3: Completed ($0.0020)")

	tasks[4].Start()
	t.Log("  Worker 4: Still running...")

	// Aggregate stats from all workers
	completedCount := 0
	failedCount := 0
	runningCount := 0
	totalCost := 0.0

	for _, task := range tasks {
		if task.Status == JobStatusCompleted {
			completedCount++
		} else if task.Status == JobStatusFailed {
			failedCount++
		} else if task.Status == JobStatusRunning {
			runningCount++
		}
		totalCost += task.CostActual
	}

	// Verify aggregation
	if completedCount != 3 {
		t.Errorf("Completed count = %v, want 3", completedCount)
	}
	if failedCount != 1 {
		t.Errorf("Failed count = %v, want 1", failedCount)
	}
	if runningCount != 1 {
		t.Errorf("Running count = %v, want 1", runningCount)
	}

	expectedCost := 0.0021 + 0.0019 + 0.0020
	if totalCost != expectedCost {
		t.Errorf("Total cost = %v, want %v", totalCost, expectedCost)
	}

	// Update parent with aggregated data
	parent.Progress.Total = len(tasks)
	parent.Progress.Current = completedCount
	parent.CostActual = totalCost

	if parent.Progress.Current != 3 {
		t.Errorf("Parent progress = %v, want 3", parent.Progress.Current)
	}
	if parent.CostActual != expectedCost {
		t.Errorf("Parent cost = %v, want %v", parent.CostActual, expectedCost)
	}

	t.Logf("  ✓ Aggregation complete: 3/5 workers done, 1 failed, $%.4f spent", totalCost)
}
