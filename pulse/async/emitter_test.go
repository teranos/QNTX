package async

import (
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"errors"
	"testing"

	"github.com/teranos/QNTX/ats/ix"
	"go.uber.org/zap/zaptest"
)

// ============================================================================
// Progress Reporter Test Universe
// ============================================================================
//
// Characters:
//   - Mission Control: Emits progress updates for ongoing missions
//
// Theme: Emitters report progress as work happens - attestations created,
// stages completed, errors encountered. Like mission control calling out
// "Stage 1 complete", "10 satellites deployed", "Houston, we have a problem".
// ============================================================================

func TestEmitter_CoreFunctionality(t *testing.T) {
	// Setup: Create test database and queue
	testDB := qntxtest.CreateTestDB(t)
	queue := NewQueue(testDB)
	logger := zaptest.NewLogger(t).Sugar()

	t.Run("mission control creates emitter", func(t *testing.T) {
		// Mission Control sets up reporting for a new mission
		job := &Job{
			ID:          "mission-001",
			HandlerName: "test.satellite-deployment",
			Source:      "starlink-batch-42",
			Progress:    Progress{Current: 0, Total: 100},
		}

		emitter := NewJobProgressEmitter(job, queue, nil, logger)

		if emitter.job != job {
			t.Error("Expected emitter to track the mission job")
		}
		if emitter.queue != queue {
			t.Error("Expected emitter to have queue for updates")
		}
		if emitter.log == nil {
			t.Error("Expected emitter to have logger")
		}
	})

	t.Run("mission control reports stage transitions", func(t *testing.T) {
		// Create a mission and save it to database
		job := &Job{
			ID:          "mission-002",
			HandlerName: "test.rocket-launch",
			Source:      "falcon-9",
			Status:      JobStatusRunning,
			Progress:    Progress{Current: 0, Total: 100},
		}
		if err := queue.store.CreateJob(job); err != nil {
			t.Fatalf("Failed to save job: %v", err)
		}

		emitter := NewJobProgressEmitter(job, queue, nil, logger)

		// Mission Control: "Stage 1 ignition complete"
		emitter.EmitStage("ignition", "Main engines started")

		// Verify job was updated in database
		updated, err := queue.GetJob(job.ID)
		if err != nil {
			t.Fatalf("Failed to get updated job: %v", err)
		}
		if updated.ID != job.ID {
			t.Error("Expected job to be updated in database")
		}
	})

	t.Run("mission control reports attestation creation", func(t *testing.T) {
		// Create a mission
		job := &Job{
			ID:          "mission-003",
			HandlerName: "test.satellite-deployment",
			Source:      "starlink",
			Status:      JobStatusRunning,
			Progress:    Progress{Current: 0, Total: 100},
		}
		if err := queue.store.CreateJob(job); err != nil {
			t.Fatalf("Failed to save job: %v", err)
		}

		emitter := NewJobProgressEmitter(job, queue, nil, logger)

		// Mission Control: "10 satellites deployed successfully"
		satellites := []ix.AttestationEntity{
			{Entity: "sat-001"},
			{Entity: "sat-002"},
			{Entity: "sat-003"},
			{Entity: "sat-004"},
			{Entity: "sat-005"},
			{Entity: "sat-006"},
			{Entity: "sat-007"},
			{Entity: "sat-008"},
			{Entity: "sat-009"},
			{Entity: "sat-010"},
		}
		emitter.EmitAttestations(10, satellites)

		// Verify progress increased
		if job.Progress.Current != 10 {
			t.Errorf("Expected progress 10, got %d", job.Progress.Current)
		}

		// Verify job was updated in database
		updated, err := queue.GetJob(job.ID)
		if err != nil {
			t.Fatalf("Failed to get updated job: %v", err)
		}
		if updated.Progress.Current != 10 {
			t.Errorf("Expected database progress 10, got %d", updated.Progress.Current)
		}
	})

	t.Run("mission control reports candidate scoring", func(t *testing.T) {
		// Create a mission
		job := &Job{
			ID:          "mission-004",
			HandlerName: "test.candidate-evaluation",
			Source:      "astronaut-applications",
			Status:      JobStatusRunning,
			Progress:    Progress{Current: 0, Total: 50},
			CostActual:  0.0,
		}
		if err := queue.store.CreateJob(job); err != nil {
			t.Fatalf("Failed to save job: %v", err)
		}

		emitter := NewJobProgressEmitter(job, queue, nil, logger)

		// Mission Control: "Candidate AST-042 scored 0.95, qualified"
		emitter.EmitCandidateMatch("AST-042", 0.95, true, "Excellent physical fitness")

		// Verify progress increased by 1
		if job.Progress.Current != 1 {
			t.Errorf("Expected progress 1, got %d", job.Progress.Current)
		}

		// Verify cost was recorded
		if job.CostActual != 0.002 {
			t.Errorf("Expected cost 0.002, got %f", job.CostActual)
		}

		// Verify job was updated in database
		updated, err := queue.GetJob(job.ID)
		if err != nil {
			t.Fatalf("Failed to get updated job: %v", err)
		}
		if updated.Progress.Current != 1 {
			t.Errorf("Expected database progress 1, got %d", updated.Progress.Current)
		}
	})

	t.Run("mission control logs informational messages", func(t *testing.T) {
		job := &Job{
			ID:          "mission-005",
			HandlerName: "test.weather-monitoring",
			Source:      "weather-station",
			Status:      JobStatusRunning,
		}

		emitter := NewJobProgressEmitter(job, queue, nil, logger)

		// Mission Control: "Weather conditions nominal"
		// This should just log, no error should occur
		emitter.EmitInfo("Weather conditions nominal for launch")

		// If we get here without panic, test passes
		// (Logger output is captured by zaptest)
	})

	t.Run("mission control reports errors", func(t *testing.T) {
		// Create a mission
		job := &Job{
			ID:          "mission-006",
			HandlerName: "test.rocket-launch",
			Source:      "falcon-heavy",
			Status:      JobStatusRunning,
			Progress:    Progress{Current: 50, Total: 100},
		}
		if err := queue.store.CreateJob(job); err != nil {
			t.Fatalf("Failed to save job: %v", err)
		}

		emitter := NewJobProgressEmitter(job, queue, nil, logger)

		// Mission Control: "Houston, we have a problem"
		testErr := errors.New("fuel line pressure critical")
		emitter.EmitError("fuel-system", testErr)

		// Verify error was recorded in job
		if job.Error == "" {
			t.Error("Expected error to be recorded in job")
		}

		// Verify job was updated in database
		updated, err := queue.GetJob(job.ID)
		if err != nil {
			t.Fatalf("Failed to get updated job: %v", err)
		}
		if updated.Error == "" {
			t.Error("Expected error to be saved in database")
		}
	})
}
