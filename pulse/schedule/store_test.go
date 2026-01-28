package schedule

import (
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateJob(t *testing.T) {
	db := qntxtest.CreateTestDB(t)

	store := NewStore(db)

	job := &Job{
		ID:              "SPJ_test123",
		ATSCode:         "ix https://example.com/jobs",
		IntervalSeconds: 3600, // 1 hour
		NextRunAt:       ptr(time.Now().Add(1 * time.Hour)),
		State:           StateActive,
	}

	err := store.CreateJob(job)
	require.NoError(t, err)

	// Verify job was created
	retrieved, err := store.GetJob(job.ID)
	require.NoError(t, err)
	assert.Equal(t, job.ID, retrieved.ID)
	assert.Equal(t, job.ATSCode, retrieved.ATSCode)
	assert.Equal(t, job.IntervalSeconds, retrieved.IntervalSeconds)
	assert.Equal(t, job.State, retrieved.State)
}

func TestListJobsDue(t *testing.T) {
	db := qntxtest.CreateTestDB(t)

	store := NewStore(db)
	now := time.Now()

	// Create jobs with different next_run_at times
	jobs := []*Job{
		{
			ID:              "SPJ_past",
			ATSCode:         "ix https://past.com",
			IntervalSeconds: 3600,
			NextRunAt:       ptr(now.Add(-10 * time.Minute)), // Past due
			State:           StateActive,
		},
		{
			ID:              "SPJ_now",
			ATSCode:         "ix https://now.com",
			IntervalSeconds: 3600,
			NextRunAt:       ptr(now), // Due now
			State:           StateActive,
		},
		{
			ID:              "SPJ_future",
			ATSCode:         "ix https://future.com",
			IntervalSeconds: 3600,
			NextRunAt:       ptr(now.Add(10 * time.Minute)), // Future
			State:           StateActive,
		},
		{
			ID:              "SPJ_paused",
			ATSCode:         "ix https://paused.com",
			IntervalSeconds: 3600,
			NextRunAt:       ptr(now.Add(-5 * time.Minute)), // Past due but paused
			State:           StatePaused,
		},
	}

	for _, job := range jobs {
		err := store.CreateJob(job)
		require.NoError(t, err)
	}

	// List jobs due for execution
	due, err := store.ListJobsDue(now)
	require.NoError(t, err)

	// Should return only active jobs with next_run_at <= now
	assert.Len(t, due, 2)                  // SPJ_past and SPJ_now
	assert.Equal(t, "SPJ_past", due[0].ID) // Ordered by next_run_at
	assert.Equal(t, "SPJ_now", due[1].ID)
}

func TestUpdateState(t *testing.T) {
	db := qntxtest.CreateTestDB(t)

	store := NewStore(db)

	job := &Job{
		ID:              "SPJ_state_test",
		ATSCode:         "ix https://example.com",
		IntervalSeconds: 3600,
		NextRunAt:       ptr(time.Now().Add(1 * time.Hour)),
		State:           StateActive,
	}

	err := store.CreateJob(job)
	require.NoError(t, err)

	// Pause the job
	err = store.UpdateJobState(job.ID, StatePaused)
	require.NoError(t, err)

	// Verify state changed
	retrieved, err := store.GetJob(job.ID)
	require.NoError(t, err)
	assert.Equal(t, StatePaused, retrieved.State)

	// Resume the job
	err = store.UpdateJobState(job.ID, StateActive)
	require.NoError(t, err)

	retrieved, err = store.GetJob(job.ID)
	require.NoError(t, err)
	assert.Equal(t, StateActive, retrieved.State)
}

func TestUpdateJobAfterExecution(t *testing.T) {
	db := qntxtest.CreateTestDB(t)

	store := NewStore(db)
	now := time.Now()

	job := &Job{
		ID:              "SPJ_exec_test",
		ATSCode:         "ix https://example.com",
		IntervalSeconds: 3600, // 1 hour
		NextRunAt:       ptr(now),
		State:           StateActive,
	}

	err := store.CreateJob(job)
	require.NoError(t, err)

	// Execute the job
	executionID := "JB_execution123"
	nextRun := now.Add(1 * time.Hour)

	err = store.UpdateJobAfterExecution(job.ID, now, executionID, nextRun)
	require.NoError(t, err)

	// Verify updates
	retrieved, err := store.GetJob(job.ID)
	require.NoError(t, err)
	assert.NotNil(t, retrieved.LastRunAt)
	assert.WithinDuration(t, now, *retrieved.LastRunAt, 1*time.Second)
	assert.Equal(t, executionID, retrieved.LastExecutionID)
	assert.WithinDuration(t, nextRun, *retrieved.NextRunAt, 1*time.Second)
}

func TestJobTimeDrift(t *testing.T) {
	// Test that ticker handles time drift gracefully across restarts
	db := qntxtest.CreateTestDB(t)

	store := NewStore(db)
	now := time.Now()

	job := &Job{
		ID:              "SPJ_drift_test",
		ATSCode:         "ix https://example.com",
		IntervalSeconds: 3600,                         // 1 hour
		NextRunAt:       ptr(now.Add(-2 * time.Hour)), // Should have run 2 hours ago
		State:           StateActive,
	}

	err := store.CreateJob(job)
	require.NoError(t, err)

	// Simulate finding jobs due (should catch the overdue job)
	due, err := store.ListJobsDue(now)
	require.NoError(t, err)
	assert.Len(t, due, 1)
	assert.Equal(t, job.ID, due[0].ID)

	// After execution, next_run_at should be relative to now, not the old next_run_at
	// This prevents "catching up" on missed executions
	nextRun := now.Add(time.Duration(job.IntervalSeconds) * time.Second)
	err = store.UpdateJobAfterExecution(job.ID, now, "exec1", nextRun)
	require.NoError(t, err)

	retrieved, err := store.GetJob(job.ID)
	require.NoError(t, err)
	assert.WithinDuration(t, nextRun, *retrieved.NextRunAt, 1*time.Second)
}

func TestCreateJobWithMetadata(t *testing.T) {
	db := qntxtest.CreateTestDB(t)

	store := NewStore(db)

	job := &Job{
		ID:              "SPJ_metadata_test",
		ATSCode:         "ix https://example.com",
		IntervalSeconds: 3600,
		NextRunAt:       ptr(time.Now().Add(1 * time.Hour)),
		State:           StateActive,
		CreatedFromDoc:  "pm_doc_123",
		Metadata:        `{"scraper_type": "vacancies", "company": "Base Cyber Security"}`,
	}

	err := store.CreateJob(job)
	require.NoError(t, err)

	retrieved, err := store.GetJob(job.ID)
	require.NoError(t, err)
	assert.Equal(t, job.CreatedFromDoc, retrieved.CreatedFromDoc)
	assert.Equal(t, job.Metadata, retrieved.Metadata)
}

func TestListAllScheduledJobs(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewStore(db)
	now := time.Now()

	// Create jobs with different states
	jobs := []*Job{
		{
			ID:              "SPJ_active1",
			ATSCode:         "ix https://active1.com",
			IntervalSeconds: 3600,
			NextRunAt:       ptr(now.Add(1 * time.Hour)),
			State:           StateActive,
		},
		{
			ID:              "SPJ_paused1",
			ATSCode:         "ix https://paused1.com",
			IntervalSeconds: 3600,
			NextRunAt:       ptr(now.Add(2 * time.Hour)),
			State:           StatePaused,
		},
		{
			ID:              "SPJ_inactive1",
			ATSCode:         "ix https://inactive1.com",
			IntervalSeconds: 3600,
			NextRunAt:       ptr(now.Add(3 * time.Hour)),
			State:           StateInactive,
		},
		{
			ID:              "SPJ_deleted1",
			ATSCode:         "ix https://deleted1.com",
			IntervalSeconds: 3600,
			NextRunAt:       ptr(now.Add(4 * time.Hour)),
			State:           StateDeleted,
		},
	}

	for _, job := range jobs {
		err := store.CreateJob(job)
		require.NoError(t, err)
	}

	// List all jobs (should exclude deleted)
	allJobs, err := store.ListAllScheduledJobs()
	require.NoError(t, err)

	// Should return all jobs except deleted (3 jobs)
	assert.Len(t, allJobs, 3)

	// Verify deleted job is not in the list
	for _, job := range allJobs {
		assert.NotEqual(t, StateDeleted, job.State)
		assert.NotEqual(t, "SPJ_deleted1", job.ID)
	}

	// Verify the other states are present
	statesFound := make(map[string]bool)
	for _, job := range allJobs {
		statesFound[job.State] = true
	}
	assert.True(t, statesFound[StateActive])
	assert.True(t, statesFound[StatePaused])
	assert.True(t, statesFound[StateInactive])
}

func TestUpdateJobInterval(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewStore(db)
	now := time.Now()

	job := &Job{
		ID:              "SPJ_interval_test",
		ATSCode:         "ix https://example.com",
		IntervalSeconds: 3600, // 1 hour
		NextRunAt:       ptr(now.Add(1 * time.Hour)),
		State:           StateActive,
	}

	err := store.CreateJob(job)
	require.NoError(t, err)

	// Update interval to 7200 seconds (2 hours)
	newInterval := 7200
	err = store.UpdateJobInterval(job.ID, newInterval)
	require.NoError(t, err)

	// Verify interval was updated
	retrieved, err := store.GetJob(job.ID)
	require.NoError(t, err)
	assert.Equal(t, newInterval, retrieved.IntervalSeconds)

	// Test updating non-existent job
	err = store.UpdateJobInterval("SPJ_doesnotexist", 1800)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scheduled job not found")
}

func TestGetNextScheduledJob(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewStore(db)
	now := time.Now()

	t.Run("NoActiveJobs", func(t *testing.T) {
		// Create only paused and inactive jobs
		jobs := []*Job{
			{
				ID:              "SPJ_paused_next",
				ATSCode:         "ix https://paused.com",
				IntervalSeconds: 3600,
				NextRunAt:       ptr(now.Add(-1 * time.Hour)), // Past due but paused
				State:           StatePaused,
			},
			{
				ID:              "SPJ_inactive_next",
				ATSCode:         "ix https://inactive.com",
				IntervalSeconds: 3600,
				NextRunAt:       ptr(now.Add(-30 * time.Minute)), // Past due but inactive
				State:           StateInactive,
			},
		}

		for _, job := range jobs {
			err := store.CreateJob(job)
			require.NoError(t, err)
		}

		// Should return nil when no active jobs exist
		nextJob, err := store.GetNextScheduledJob()
		require.NoError(t, err)
		assert.Nil(t, nextJob)
	})

	t.Run("MultipleActiveJobs", func(t *testing.T) {
		// Create multiple active jobs with different next_run_at times
		jobs := []*Job{
			{
				ID:              "SPJ_future1",
				ATSCode:         "ix https://future1.com",
				IntervalSeconds: 3600,
				NextRunAt:       ptr(now.Add(2 * time.Hour)),
				State:           StateActive,
			},
			{
				ID:              "SPJ_soonest",
				ATSCode:         "ix https://soonest.com",
				IntervalSeconds: 3600,
				NextRunAt:       ptr(now.Add(30 * time.Minute)), // Earliest
				State:           StateActive,
			},
			{
				ID:              "SPJ_future2",
				ATSCode:         "ix https://future2.com",
				IntervalSeconds: 3600,
				NextRunAt:       ptr(now.Add(3 * time.Hour)),
				State:           StateActive,
			},
		}

		for _, job := range jobs {
			err := store.CreateJob(job)
			require.NoError(t, err)
		}

		// Should return the job with earliest next_run_at
		nextJob, err := store.GetNextScheduledJob()
		require.NoError(t, err)
		require.NotNil(t, nextJob)
		assert.Equal(t, "SPJ_soonest", nextJob.ID)
		assert.Equal(t, "ix https://soonest.com", nextJob.ATSCode)
	})
}
