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
		Id:              "SPJ_test123",
		AtsCode:         "ix https://example.com/jobs",
		IntervalSeconds: 3600, // 1 hour
		NextRunAt:       time.Now().Add(1 * time.Hour).Format(time.RFC3339),
		State:           StateActive,
	}

	err := store.CreateJob(job)
	require.NoError(t, err)

	// Verify job was created
	retrieved, err := store.GetJob(job.Id)
	require.NoError(t, err)
	assert.Equal(t, job.Id, retrieved.Id)
	assert.Equal(t, job.AtsCode, retrieved.AtsCode)
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
			Id:              "SPJ_past",
			AtsCode:         "ix https://past.com",
			IntervalSeconds: 3600,
			NextRunAt:       now.Add(-10 * time.Minute).Format(time.RFC3339), // Past due
			State:           StateActive,
		},
		{
			Id:              "SPJ_now",
			AtsCode:         "ix https://now.com",
			IntervalSeconds: 3600,
			NextRunAt:       now.Format(time.RFC3339), // Due now
			State:           StateActive,
		},
		{
			Id:              "SPJ_future",
			AtsCode:         "ix https://future.com",
			IntervalSeconds: 3600,
			NextRunAt:       now.Add(10 * time.Minute).Format(time.RFC3339), // Future
			State:           StateActive,
		},
		{
			Id:              "SPJ_paused",
			AtsCode:         "ix https://paused.com",
			IntervalSeconds: 3600,
			NextRunAt:       now.Add(-5 * time.Minute).Format(time.RFC3339), // Past due but paused
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
	assert.Equal(t, "SPJ_past", due[0].Id) // Ordered by next_run_at
	assert.Equal(t, "SPJ_now", due[1].Id)
}

func TestUpdateState(t *testing.T) {
	db := qntxtest.CreateTestDB(t)

	store := NewStore(db)

	job := &Job{
		Id:              "SPJ_state_test",
		AtsCode:         "ix https://example.com",
		IntervalSeconds: 3600,
		NextRunAt:       time.Now().Add(1 * time.Hour).Format(time.RFC3339),
		State:           StateActive,
	}

	err := store.CreateJob(job)
	require.NoError(t, err)

	// Pause the job
	err = store.UpdateJobState(job.Id, StatePaused)
	require.NoError(t, err)

	// Verify state changed
	retrieved, err := store.GetJob(job.Id)
	require.NoError(t, err)
	assert.Equal(t, StatePaused, retrieved.State)

	// Resume the job
	err = store.UpdateJobState(job.Id, StateActive)
	require.NoError(t, err)

	retrieved, err = store.GetJob(job.Id)
	require.NoError(t, err)
	assert.Equal(t, StateActive, retrieved.State)
}

func TestUpdateJobAfterExecution(t *testing.T) {
	db := qntxtest.CreateTestDB(t)

	store := NewStore(db)
	now := time.Now()

	job := &Job{
		Id:              "SPJ_exec_test",
		AtsCode:         "ix https://example.com",
		IntervalSeconds: 3600, // 1 hour
		NextRunAt:       now.Format(time.RFC3339),
		State:           StateActive,
	}

	err := store.CreateJob(job)
	require.NoError(t, err)

	// Execute the job
	executionID := "JB_execution123"
	nextRun := now.Add(1 * time.Hour)

	err = store.UpdateJobAfterExecution(job.Id, now, executionID, nextRun)
	require.NoError(t, err)

	// Verify updates
	retrieved, err := store.GetJob(job.Id)
	require.NoError(t, err)
	assert.NotEmpty(t, retrieved.LastRunAt)
	assert.WithinDuration(t, now, mustParse(t, retrieved.LastRunAt), 1*time.Second)
	assert.Equal(t, executionID, retrieved.LastExecutionId)
	assert.WithinDuration(t, nextRun, mustParse(t, retrieved.NextRunAt), 1*time.Second)
}

func TestJobTimeDrift(t *testing.T) {
	// Test that ticker handles time drift gracefully across restarts
	db := qntxtest.CreateTestDB(t)

	store := NewStore(db)
	now := time.Now()

	job := &Job{
		Id:              "SPJ_drift_test",
		AtsCode:         "ix https://example.com",
		IntervalSeconds: 3600,                         // 1 hour
		NextRunAt:       now.Add(-2 * time.Hour).Format(time.RFC3339), // Should have run 2 hours ago
		State:           StateActive,
	}

	err := store.CreateJob(job)
	require.NoError(t, err)

	// Simulate finding jobs due (should catch the overdue job)
	due, err := store.ListJobsDue(now)
	require.NoError(t, err)
	assert.Len(t, due, 1)
	assert.Equal(t, job.Id, due[0].Id)

	// After execution, next_run_at should be relative to now, not the old next_run_at
	// This prevents "catching up" on missed executions
	nextRun := now.Add(time.Duration(job.IntervalSeconds) * time.Second)
	err = store.UpdateJobAfterExecution(job.Id, now, "exec1", nextRun)
	require.NoError(t, err)

	retrieved, err := store.GetJob(job.Id)
	require.NoError(t, err)
	assert.WithinDuration(t, nextRun, mustParse(t, retrieved.NextRunAt), 1*time.Second)
}

func TestCreateJobWithMetadata(t *testing.T) {
	db := qntxtest.CreateTestDB(t)

	store := NewStore(db)

	job := &Job{
		Id:              "SPJ_metadata_test",
		AtsCode:         "ix https://example.com",
		IntervalSeconds: 3600,
		NextRunAt:       time.Now().Add(1 * time.Hour).Format(time.RFC3339),
		State:           StateActive,
		CreatedFromDoc:  "pm_doc_123",
		Metadata:        `{"scraper_type": "vacancies", "company": "Base Cyber Security"}`,
	}

	err := store.CreateJob(job)
	require.NoError(t, err)

	retrieved, err := store.GetJob(job.Id)
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
			Id:              "SPJ_active1",
			AtsCode:         "ix https://active1.com",
			IntervalSeconds: 3600,
			NextRunAt:       now.Add(1 * time.Hour).Format(time.RFC3339),
			State:           StateActive,
		},
		{
			Id:              "SPJ_paused1",
			AtsCode:         "ix https://paused1.com",
			IntervalSeconds: 3600,
			NextRunAt:       now.Add(2 * time.Hour).Format(time.RFC3339),
			State:           StatePaused,
		},
		{
			Id:              "SPJ_inactive1",
			AtsCode:         "ix https://inactive1.com",
			IntervalSeconds: 3600,
			NextRunAt:       now.Add(3 * time.Hour).Format(time.RFC3339),
			State:           StateInactive,
		},
		{
			Id:              "SPJ_deleted1",
			AtsCode:         "ix https://deleted1.com",
			IntervalSeconds: 3600,
			NextRunAt:       now.Add(4 * time.Hour).Format(time.RFC3339),
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
		assert.NotEqual(t, "SPJ_deleted1", job.Id)
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
		Id:              "SPJ_interval_test",
		AtsCode:         "ix https://example.com",
		IntervalSeconds: 3600, // 1 hour
		NextRunAt:       now.Add(1 * time.Hour).Format(time.RFC3339),
		State:           StateActive,
	}

	err := store.CreateJob(job)
	require.NoError(t, err)

	// Update interval to 7200 seconds (2 hours)
	newInterval := 7200
	err = store.UpdateJobInterval(job.Id, newInterval)
	require.NoError(t, err)

	// Verify interval was updated
	retrieved, err := store.GetJob(job.Id)
	require.NoError(t, err)
	assert.Equal(t, int32(newInterval), retrieved.IntervalSeconds)

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
				Id:              "SPJ_paused_next",
				AtsCode:         "ix https://paused.com",
				IntervalSeconds: 3600,
				NextRunAt:       now.Add(-1 * time.Hour).Format(time.RFC3339), // Past due but paused
				State:           StatePaused,
			},
			{
				Id:              "SPJ_inactive_next",
				AtsCode:         "ix https://inactive.com",
				IntervalSeconds: 3600,
				NextRunAt:       now.Add(-30 * time.Minute).Format(time.RFC3339), // Past due but inactive
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
				Id:              "SPJ_future1",
				AtsCode:         "ix https://future1.com",
				IntervalSeconds: 3600,
				NextRunAt:       now.Add(2 * time.Hour).Format(time.RFC3339),
				State:           StateActive,
			},
			{
				Id:              "SPJ_soonest",
				AtsCode:         "ix https://soonest.com",
				IntervalSeconds: 3600,
				NextRunAt:       now.Add(30 * time.Minute).Format(time.RFC3339), // Earliest
				State:           StateActive,
			},
			{
				Id:              "SPJ_future2",
				AtsCode:         "ix https://future2.com",
				IntervalSeconds: 3600,
				NextRunAt:       now.Add(3 * time.Hour).Format(time.RFC3339),
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
		assert.Equal(t, "SPJ_soonest", nextJob.Id)
		assert.Equal(t, "ix https://soonest.com", nextJob.AtsCode)
	})

	// A job with no next run is not scheduled for any time, so it cannot be
	// the soonest. NULL sorts first in SQLite, which made one such row hide
	// every real job behind it and the ticker report an empty schedule.
	t.Run("JobWithNoNextRunIsNotTheSoonest", func(t *testing.T) {
		require.NoError(t, store.CreateJob(&Job{
			Id:              "SPJ_never",
			AtsCode:         "ix https://never.com",
			IntervalSeconds: 180,
			State:           StateActive,
		}))

		nextJob, err := store.GetNextScheduledJob()
		require.NoError(t, err)
		require.NotNil(t, nextJob)
		assert.Equal(t, "SPJ_soonest", nextJob.Id)
	})
}

// mustParse turns an RFC3339 column back into a time for comparison.
func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, s)
	require.NoError(t, err, "timestamp %q is not RFC3339", s)
	return parsed
}
