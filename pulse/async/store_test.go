package async

import (
	"fmt"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"testing"
	"time"
)

// ============================================================================
// TAS Bot & Kirby Store Test Universe
// ============================================================================
//
// Characters:
//   - TAS Bot: Frame-perfect coordinator who persists job data
//   - Kirby: The worker who retrieves and updates stored jobs
//   - Cronos: Greek god of time, appears for cleanup and time-based operations
//
// Theme: TAS Bot stores save states (jobs) in the database, Kirby loads
// and updates them, and Cronos manages old saves (cleanup).
// ============================================================================

// TestTASBotCreatesJob tests that TAS Bot can create and persist a job
func TestTASBotCreatesJob(t *testing.T) {
	t.Log("🎮 TAS Bot creates save state (persists job to database)...")
	t.Log("   'Saving game state at optimal frame'")

	db := qntxtest.CreateTestDB(t)
	store := NewStore(db)

	// TAS Bot creates a job save state
	job := &Job{
		ID:          "JOB_SAVE_001",
		HandlerName: "test.weather-sensor",
		Source:      "speedrun_checkpoint.html",
		Status:      "queued",

		CostEstimate: 0.15,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	// TAS Bot saves the game state
	err := store.CreateJob(job)
	if err != nil {
		t.Fatalf("TAS Bot failed to create job: %v", err)
	}

	t.Log("✓ TAS Bot successfully saved game state JOB_SAVE_001")
	t.Log("  'Save state written to database'")
}

// TestKirbyRetrievesJob tests that Kirby can retrieve a stored job
func TestKirbyRetrievesJob(t *testing.T) {
	t.Log("⭐ Kirby loads save state (retrieves job from database)...")
	t.Log("   'Poyo!' *loading game state*")

	db := qntxtest.CreateTestDB(t)
	store := NewStore(db)

	// TAS Bot creates a save state first
	originalJob := &Job{
		ID:          "JOB_LOAD_001",
		HandlerName: "test.weather-sensor",
		Source:      "kirby_checkpoint.html",
		Status:      "queued",

		CostEstimate: 0.20,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	store.CreateJob(originalJob)

	// Kirby loads the save state
	loadedJob, err := store.GetJob("JOB_LOAD_001")
	if err != nil {
		t.Fatalf("Kirby failed to retrieve job: %v", err)
	}

	if loadedJob == nil {
		t.Fatal("Kirby found no save state")
	}

	if loadedJob.ID != "JOB_LOAD_001" {
		t.Errorf("Kirby loaded wrong save: got %s", loadedJob.ID)
	}

	if loadedJob.Source != "kirby_checkpoint.html" {
		t.Errorf("Kirby's save state corrupted: expected kirby_checkpoint.html, got %s", loadedJob.Source)
	}

	t.Log("✓ Kirby successfully loaded save state JOB_LOAD_001")
	t.Log("  'Poyo!' *game state loaded successfully*")
}

// TestTASBotUpdatesJob tests that TAS Bot can update an existing job
func TestTASBotUpdatesJob(t *testing.T) {
	t.Log("🎮 TAS Bot updates save state (modifies existing job)...")
	t.Log("   'Updating save state with new progress'")

	db := qntxtest.CreateTestDB(t)
	store := NewStore(db)

	// Create initial save state
	job := &Job{
		ID:          "JOB_UPDATE_001",
		HandlerName: "test.weather-sensor",
		Source:      "update_test.html",
		Status:      "queued",

		CostEstimate: 0.10,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Progress:     Progress{Current: 0, Total: 100},
	}
	store.CreateJob(job)

	// TAS Bot updates the save state
	job.Status = "running"
	job.Progress.Current = 50
	startedTime := time.Now()
	job.StartedAt = &startedTime
	job.UpdatedAt = time.Now()

	err := store.UpdateJob(job)
	if err != nil {
		t.Fatalf("TAS Bot failed to update job: %v", err)
	}

	// Verify the update
	updated, _ := store.GetJob("JOB_UPDATE_001")
	if updated.Status != "running" {
		t.Errorf("TAS Bot expected status 'running', got '%s'", updated.Status)
	}
	if updated.Progress.Current != 50 {
		t.Errorf("TAS Bot expected progress 50, got %d", updated.Progress.Current)
	}

	t.Log("✓ TAS Bot successfully updated save state to 50% progress")
	t.Log("  'Save state updated at optimal frame'")
}

// TestKirbyListsJobs tests that Kirby can list jobs by status
func TestKirbyListsJobs(t *testing.T) {
	t.Log("⭐ Kirby lists available save states (queries jobs by status)...")
	t.Log("   'Poyo!' *browsing save files*")

	db := qntxtest.CreateTestDB(t)
	store := NewStore(db)

	// TAS Bot creates multiple save states
	jobs := []*Job{
		{ID: "JOB_LIST_001", HandlerName: "test.weather-sensor", Source: "station-1.dat", Status: "queued", CostEstimate: 0.10, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{ID: "JOB_LIST_002", HandlerName: "test.weather-sensor", Source: "station-2.dat", Status: "running", CostEstimate: 0.10, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{ID: "JOB_LIST_003", HandlerName: "test.weather-sensor", Source: "station-3.dat", Status: "queued", CostEstimate: 0.10, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{ID: "JOB_LIST_004", HandlerName: "test.weather-sensor", Source: "station-4.dat", Status: "completed", CostEstimate: 0.10, CreatedAt: time.Now(), UpdatedAt: time.Now()},
	}

	for _, job := range jobs {
		store.CreateJob(job)
	}

	// Kirby lists queued save states
	queuedStatus := JobStatus("queued")
	queuedJobs, err := store.ListJobs(&queuedStatus, 10)
	if err != nil {
		t.Fatalf("Kirby failed to list queued jobs: %v", err)
	}

	if len(queuedJobs) != 2 {
		t.Errorf("Kirby expected 2 queued saves, found %d", len(queuedJobs))
	}

	t.Logf("✓ Kirby found %d queued save states", len(queuedJobs))
	t.Log("  'Poyo!' *found save files to load*")
}

// TestTASBotListsActiveJobs tests that TAS Bot can list all active jobs
func TestTASBotListsActiveJobs(t *testing.T) {
	t.Log("🎮 TAS Bot lists active save states (running + queued jobs)...")
	t.Log("   'Checking active speedrun attempts'")

	db := qntxtest.CreateTestDB(t)
	store := NewStore(db)

	// Create various save states
	jobs := []*Job{
		{ID: "JOB_ACTIVE_001", HandlerName: "test.weather-sensor", Source: "sensor-1.dat", Status: "queued", CostEstimate: 0.10, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{ID: "JOB_ACTIVE_002", HandlerName: "test.weather-sensor", Source: "sensor-2.dat", Status: "running", CostEstimate: 0.10, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{ID: "JOB_ACTIVE_003", HandlerName: "test.weather-sensor", Source: "sensor-3.dat", Status: "completed", CostEstimate: 0.10, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{ID: "JOB_ACTIVE_004", HandlerName: "test.weather-sensor", Source: "sensor-4.dat", Status: "running", CostEstimate: 0.10, CreatedAt: time.Now(), UpdatedAt: time.Now()},
	}

	for _, job := range jobs {
		store.CreateJob(job)
	}

	// TAS Bot lists active saves (running + queued + paused)
	activeJobs, err := store.ListActiveJobs(10)
	if err != nil {
		t.Fatalf("TAS Bot failed to list active jobs: %v", err)
	}

	// Should have 3 active (1 queued + 2 running)
	if len(activeJobs) != 3 {
		t.Errorf("TAS Bot expected 3 active saves, found %d", len(activeJobs))
	}

	t.Logf("✓ TAS Bot found %d active speedrun attempts", len(activeJobs))
	t.Log("  'Optimal runs in progress'")
}

// TestKirbyDeletesJob tests that Kirby can delete a save state
func TestKirbyDeletesJob(t *testing.T) {
	t.Log("⭐ Kirby deletes save state (removes job from database)...")
	t.Log("   'Poyo!' *deleting old save file*")

	db := qntxtest.CreateTestDB(t)
	store := NewStore(db)

	// Create a save state
	job := &Job{
		ID:          "JOB_DELETE_001",
		HandlerName: "test.weather-sensor",
		Source:      "delete_me.html",
		Status:      "failed",

		CostEstimate: 0.10,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	store.CreateJob(job)

	// Kirby deletes the save state
	err := store.DeleteJob("JOB_DELETE_001")
	if err != nil {
		t.Fatalf("Kirby failed to delete job: %v", err)
	}

	// Verify deletion
	deleted, err := store.GetJob("JOB_DELETE_001")
	if err == nil && deleted != nil {
		t.Error("Kirby failed to delete save state - still exists")
	}

	t.Log("✓ Kirby successfully deleted save state JOB_DELETE_001")
	t.Log("  'Poyo!' *old save file removed*")
}

// TestTASBotParentJobHierarchy tests that TAS Bot can query child tasks
func TestTASBotParentJobHierarchy(t *testing.T) {
	t.Log("🎮 TAS Bot queries parent-child save state hierarchy...")
	t.Log("   'Checking sub-tasks in speedrun strategy'")

	db := qntxtest.CreateTestDB(t)
	store := NewStore(db)

	// Create parent save state
	parentJob := &Job{
		ID:          "JOB_PARENT_001",
		HandlerName: "test.weather-sensor",
		Source:      "parent.html",
		Status:      "running",

		CostEstimate: 0.50,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	store.CreateJob(parentJob)

	// Create child task save states
	childJobs := []*Job{
		{ID: "JOB_CHILD_001", ParentJobID: "JOB_PARENT_001", HandlerName: "test.task", Source: "task1", Status: "completed", CostEstimate: 0.10, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{ID: "JOB_CHILD_002", ParentJobID: "JOB_PARENT_001", HandlerName: "test.task", Source: "task2", Status: "running", CostEstimate: 0.10, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{ID: "JOB_CHILD_003", ParentJobID: "JOB_PARENT_001", HandlerName: "test.task", Source: "task3", Status: "queued", CostEstimate: 0.10, CreatedAt: time.Now(), UpdatedAt: time.Now()},
	}

	for _, child := range childJobs {
		store.CreateJob(child)
	}

	// TAS Bot queries child tasks
	tasks, err := store.ListTasksByParent("JOB_PARENT_001")
	if err != nil {
		t.Fatalf("TAS Bot failed to list child tasks: %v", err)
	}

	if len(tasks) != 3 {
		t.Errorf("TAS Bot expected 3 child tasks, found %d", len(tasks))
	}

	t.Logf("✓ TAS Bot found %d sub-tasks under parent job", len(tasks))
	t.Log("  'Speedrun strategy breakdown confirmed'")
}

// TestCronosCleanupOldJobs tests that Cronos can cleanup old save states
func TestCronosCleanupOldJobs(t *testing.T) {
	t.Log("⏰ Cronos performs cleanup of ancient save states...")
	t.Log("   'Removing saves lost to the passage of time'")

	db := qntxtest.CreateTestDB(t)
	store := NewStore(db)

	// Create old save states (simulate old timestamps)
	oldTime := time.Now().Add(-48 * time.Hour)
	recentTime := time.Now().Add(-1 * time.Hour)

	oldJobs := []*Job{
		{ID: "JOB_OLD_001", HandlerName: "test.weather-sensor", Source: "archive-1.dat", Status: "completed", CostEstimate: 0.10, CreatedAt: oldTime, UpdatedAt: oldTime},
		{ID: "JOB_OLD_002", HandlerName: "test.weather-sensor", Source: "archive-2.dat", Status: "failed", CostEstimate: 0.10, CreatedAt: oldTime, UpdatedAt: oldTime},
		{ID: "JOB_RECENT_001", HandlerName: "test.weather-sensor", Source: "recent.dat", Status: "completed", CostEstimate: 0.10, CreatedAt: recentTime, UpdatedAt: recentTime},
	}

	for _, job := range oldJobs {
		store.CreateJob(job)
	}

	// Cronos cleans up jobs older than 24 hours
	deleted, err := store.CleanupOldJobs(24 * time.Hour)
	if err != nil {
		t.Fatalf("Cronos failed to cleanup old jobs: %v", err)
	}

	if deleted != 2 {
		t.Errorf("Cronos expected to delete 2 old saves, deleted %d", deleted)
	}

	// Verify recent save still exists
	recent, _ := store.GetJob("JOB_RECENT_001")
	if recent == nil {
		t.Error("Cronos accidentally deleted recent save state")
	}

	t.Logf("✓ Cronos removed %d ancient save states (older than 24h)", deleted)
	t.Log("  'Time has claimed the old saves'")
}

// TestKirbyPulseStateStorage tests that Kirby can store and retrieve Pulse state
func TestKirbyPulseStateStorage(t *testing.T) {
	t.Log("⭐ Kirby tests Pulse state storage (save with metrics)...")
	t.Log("   'Poyo!' *saving job with rate limit info*")

	db := qntxtest.CreateTestDB(t)
	store := NewStore(db)

	// Kirby creates a job with Pulse state
	pulseState := &PulseState{
		CallsThisMinute: 10,
		CallsRemaining:  50,
		SpendToday:      0.75,
		SpendThisMonth:  5.20,
		BudgetRemaining: 4.25,
		IsPaused:        false,
		PauseReason:     "",
	}

	job := &Job{
		ID:          "JOB_PULSE_001",
		HandlerName: "test.weather-sensor",
		Source:      "pulse_test.html",
		Status:      "running",

		CostEstimate: 0.10,
		PulseState:   pulseState,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	err := store.CreateJob(job)
	if err != nil {
		t.Fatalf("Kirby failed to create job with Pulse state: %v", err)
	}

	// Retrieve and verify Pulse state
	retrieved, err := store.GetJob("JOB_PULSE_001")
	if err != nil {
		t.Fatalf("Kirby failed to retrieve job: %v", err)
	}

	if retrieved.PulseState == nil {
		t.Fatal("Kirby's Pulse state was lost")
	}

	if retrieved.PulseState.CallsThisMinute != 10 {
		t.Errorf("Kirby expected 10 calls, got %d", retrieved.PulseState.CallsThisMinute)
	}

	if retrieved.PulseState.SpendToday != 0.75 {
		t.Errorf("Kirby expected $0.75 spend, got $%.2f", retrieved.PulseState.SpendToday)
	}

	t.Log("✓ Kirby successfully stored and retrieved Pulse state")
	t.Log("  'Poyo!' *rate limit metrics preserved*")
}

// TestTASBotAndKirbyStoreIntegration tests complete store workflow
func TestTASBotAndKirbyStoreIntegration(t *testing.T) {
	t.Log("🎮⭐ TAS Bot and Kirby store integration test...")
	t.Log("   TAS Bot: 'Managing save states for optimal speedrun'")
	t.Log("   Kirby: 'Poyo!' *loading and updating saves*")

	db := qntxtest.CreateTestDB(t)
	store := NewStore(db)

	// TAS Bot creates initial save state
	t.Log("  TAS Bot: Creating initial save state...")
	job := &Job{
		ID:          "JOB_INTEGRATION_001",
		HandlerName: "test.weather-sensor",
		Source:      "integration_test.html",
		Status:      "queued",

		CostEstimate: 0.25,
		Progress:     Progress{Current: 0, Total: 100},
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	store.CreateJob(job)

	// Kirby loads the save state
	t.Log("  Kirby: 'Poyo!' *loading save state*")
	loaded, _ := store.GetJob("JOB_INTEGRATION_001")
	if loaded == nil {
		t.Fatal("Kirby failed to load save state")
	}

	// Kirby starts processing (update status)
	t.Log("  Kirby: 'Poyo!' *starting job execution*")
	loaded.Status = "running"
	startTime := time.Now()
	loaded.StartedAt = &startTime
	loaded.Progress.Current = 30
	loaded.UpdatedAt = time.Now()
	store.UpdateJob(loaded)

	// Kirby completes the job
	t.Log("  Kirby: 'Poyo!' *completing job*")
	loaded.Status = "completed"
	loaded.Progress.Current = 100
	completedTime := time.Now()
	loaded.CompletedAt = &completedTime
	loaded.UpdatedAt = time.Now()
	store.UpdateJob(loaded)

	// TAS Bot verifies final state
	final, _ := store.GetJob("JOB_INTEGRATION_001")
	if final.Status != "completed" {
		t.Errorf("TAS Bot expected 'completed', got '%s'", final.Status)
	}
	if final.Progress.Current != 100 {
		t.Errorf("TAS Bot expected 100%% progress, got %d%%", final.Progress.Current)
	}

	// Cronos cleans up after speedrun
	time.Sleep(10 * time.Millisecond)
	t.Log("  Cronos: 'Speedrun complete, archiving save state...'")

	t.Log("✓ Integration test complete: full save state lifecycle")
	t.Log("  TAS Bot: 'Frame-perfect save management!'")
	t.Log("  Kirby: 'Poyo!' *successful speedrun*")
	t.Log("  Cronos: 'Time has been measured and recorded'")
}

// storeCountJob is the smallest job the counting tests need. What is being
// counted never depends on a job's contents, so nothing here is filled in
// beyond the columns the schema requires.
func storeCountJob(id string, status JobStatus, now time.Time) *Job {
	return &Job{
		ID:          id,
		HandlerName: "test.counter",
		Source:      "count_me.html",
		Status:      status,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// TestTASBotCountsEachStatusSeparately checks the grouping: every status gets
// its own number, and a status nobody is holding is absent rather than zero.
func TestTASBotCountsEachStatusSeparately(t *testing.T) {
	t.Log("🎮 TAS Bot tallies the save states by kind...")

	db := qntxtest.CreateTestDB(t)
	store := NewStore(db)

	now := time.Now()
	written := map[JobStatus]int{
		JobStatusQueued:    3,
		JobStatusRunning:   1,
		JobStatusCompleted: 4,
	}
	for status, n := range written {
		for i := 0; i < n; i++ {
			job := storeCountJob(fmt.Sprintf("JOB_%s_%02d", status, i), status, now)
			if err := store.CreateJob(job); err != nil {
				t.Fatalf("TAS Bot failed to store a %s job: %v", status, err)
			}
		}
	}

	counts, err := store.CountByStatus()
	if err != nil {
		t.Fatalf("TAS Bot failed to count jobs: %v", err)
	}
	for status, want := range written {
		if counts[status] != want {
			t.Errorf("TAS Bot counted %d %s jobs, want %d", counts[status], status, want)
		}
	}
	if _, present := counts[JobStatusFailed]; present {
		t.Errorf("TAS Bot found a 'failed' entry for jobs that were never written")
	}

	t.Log("✓ TAS Bot tallied each status on its own")
}

// TestKirbyCountsPastTheListingCap is the regression this method exists for.
// Counting used to mean listing, and listing stops at MaxJobsLimit — so a
// status holding more than that reported the cap as though it were the truth.
func TestKirbyCountsPastTheListingCap(t *testing.T) {
	t.Log("🌟 Kirby counts a pile taller than one page...")

	db := qntxtest.CreateTestDB(t)
	store := NewStore(db)

	const overflow = MaxJobsLimit + 1
	now := time.Now()
	for i := 0; i < overflow; i++ {
		job := storeCountJob(fmt.Sprintf("JOB_COUNT_%06d", i), JobStatusQueued, now)
		if err := store.CreateJob(job); err != nil {
			t.Fatalf("Kirby failed to store job %d: %v", i, err)
		}
	}

	counts, err := store.CountByStatus()
	if err != nil {
		t.Fatalf("Kirby failed to count jobs: %v", err)
	}
	if counts[JobStatusQueued] != overflow {
		t.Fatalf("Kirby counted %d queued jobs, want %d — counting is capped again",
			counts[JobStatusQueued], overflow)
	}

	t.Logf("✓ Kirby counted all %d, past the %d listing cap", overflow, MaxJobsLimit)
}
