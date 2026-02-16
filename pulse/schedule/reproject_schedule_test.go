package schedule

import (
	"fmt"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	qntxtest "github.com/teranos/QNTX/internal/testing"
)

// simulateReprojectSetup mirrors the idempotent schedule creation logic in
// server.setupEmbeddingReprojectSchedule. It returns the job ID if a schedule
// was created or found, or "" if the interval is disabled.
func simulateReprojectSetup(store *Store, handlerName string, interval int) (string, error) {
	if interval <= 0 {
		return "", nil
	}

	existing, err := store.ListAllScheduledJobs()
	if err != nil {
		return "", err
	}
	for _, j := range existing {
		if j.HandlerName == handlerName && j.State == StateActive {
			if j.IntervalSeconds != interval {
				if err := store.UpdateJobInterval(j.ID, interval); err != nil {
					return "", err
				}
			}
			return j.ID, nil
		}
	}

	now := time.Now()
	job := &Job{
		ID:              fmt.Sprintf("SPJ_reproject_%d", now.Unix()),
		HandlerName:     handlerName,
		IntervalSeconds: interval,
		State:           StateActive,
		NextRunAt:       &now,
	}
	if err := store.CreateJob(job); err != nil {
		return "", err
	}
	return job.ID, nil
}

func TestReprojectScheduleSetup(t *testing.T) {
	const handler = "embeddings.reproject"

	t.Run("ZeroIntervalCreatesNothing", func(t *testing.T) {
		db := qntxtest.CreateTestDB(t)
		store := NewStore(db)

		id, err := simulateReprojectSetup(store, handler, 0)
		require.NoError(t, err)
		assert.Empty(t, id)

		jobs, err := store.ListAllScheduledJobs()
		require.NoError(t, err)
		assert.Len(t, jobs, 0)
	})

	t.Run("CreatesSchedule", func(t *testing.T) {
		db := qntxtest.CreateTestDB(t)
		store := NewStore(db)

		id, err := simulateReprojectSetup(store, handler, 3600)
		require.NoError(t, err)
		assert.NotEmpty(t, id)

		job, err := store.GetJob(id)
		require.NoError(t, err)
		assert.Equal(t, handler, job.HandlerName)
		assert.Equal(t, 3600, job.IntervalSeconds)
		assert.Equal(t, StateActive, job.State)
	})

	t.Run("IdempotentOnRestart", func(t *testing.T) {
		db := qntxtest.CreateTestDB(t)
		store := NewStore(db)

		id1, err := simulateReprojectSetup(store, handler, 3600)
		require.NoError(t, err)

		id2, err := simulateReprojectSetup(store, handler, 3600)
		require.NoError(t, err)

		assert.Equal(t, id1, id2, "second call should reuse existing schedule")

		jobs, err := store.ListAllScheduledJobs()
		require.NoError(t, err)
		assert.Len(t, jobs, 1, "should not create duplicate")
	})

	t.Run("UpdatesIntervalOnConfigChange", func(t *testing.T) {
		db := qntxtest.CreateTestDB(t)
		store := NewStore(db)

		id, err := simulateReprojectSetup(store, handler, 3600)
		require.NoError(t, err)

		// Simulate restart with different interval
		_, err = simulateReprojectSetup(store, handler, 1800)
		require.NoError(t, err)

		job, err := store.GetJob(id)
		require.NoError(t, err)
		assert.Equal(t, 1800, job.IntervalSeconds, "interval should be updated to new config value")
	})
}
