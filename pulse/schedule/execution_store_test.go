package schedule

import (
	"fmt"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateExecution(t *testing.T) {
	db := qntxtest.CreateTestDB(t)

	// Create a scheduled job first
	jobStore := NewStore(db)
	job := &Job{
		ID:              "SPJ_test123",
		ATSCode:         "ix https://example.com/jobs",
		IntervalSeconds: 3600,
		NextRunAt:       time.Now().Add(1 * time.Hour),
		State:           StateActive,
	}
	require.NoError(t, jobStore.CreateJob(job))

	// Create an execution
	execStore := NewExecutionStore(db)
	startedAt := time.Now().Format(time.RFC3339)

	exec := &Execution{
		ID:             "PEX_test456",
		ScheduledJobID: job.ID,
		Status:         ExecutionStatusRunning,
		StartedAt:      startedAt,
		CreatedAt:      startedAt,
		UpdatedAt:      startedAt,
	}

	err := execStore.CreateExecution(exec)
	require.NoError(t, err)

	// Verify execution was created
	retrieved, err := execStore.GetExecution(exec.ID)
	require.NoError(t, err)
	assert.Equal(t, exec.ID, retrieved.ID)
	assert.Equal(t, exec.ScheduledJobID, retrieved.ScheduledJobID)
	assert.Equal(t, exec.Status, retrieved.Status)
	assert.Equal(t, exec.StartedAt, retrieved.StartedAt)
	assert.Nil(t, retrieved.CompletedAt)
	assert.Nil(t, retrieved.DurationMs)
}

func TestUpdateExecution(t *testing.T) {
	db := qntxtest.CreateTestDB(t)

	// Setup job and execution
	jobStore := NewStore(db)
	job := &Job{
		ID:              "SPJ_test123",
		ATSCode:         "ix https://example.com/jobs",
		IntervalSeconds: 3600,
		NextRunAt:       time.Now().Add(1 * time.Hour),
		State:           StateActive,
	}
	require.NoError(t, jobStore.CreateJob(job))

	execStore := NewExecutionStore(db)
	startedAt := time.Now().Format(time.RFC3339)

	exec := &Execution{
		ID:             "PEX_test456",
		ScheduledJobID: job.ID,
		Status:         ExecutionStatusRunning,
		StartedAt:      startedAt,
		CreatedAt:      startedAt,
		UpdatedAt:      startedAt,
	}
	require.NoError(t, execStore.CreateExecution(exec))

	// Update to completed
	completedAt := time.Now().Format(time.RFC3339)
	durationMs := 1234
	summary := "Ingested 3 JDs"

	exec.Status = ExecutionStatusCompleted
	exec.CompletedAt = &completedAt
	exec.DurationMs = &durationMs
	exec.ResultSummary = &summary
	exec.UpdatedAt = completedAt

	err := execStore.UpdateExecution(exec)
	require.NoError(t, err)

	// Verify updates
	retrieved, err := execStore.GetExecution(exec.ID)
	require.NoError(t, err)
	assert.Equal(t, ExecutionStatusCompleted, retrieved.Status)
	assert.NotNil(t, retrieved.CompletedAt)
	assert.Equal(t, completedAt, *retrieved.CompletedAt)
	assert.NotNil(t, retrieved.DurationMs)
	assert.Equal(t, durationMs, *retrieved.DurationMs)
	assert.NotNil(t, retrieved.ResultSummary)
	assert.Equal(t, summary, *retrieved.ResultSummary)
}

func TestUpdateExecutionWithError(t *testing.T) {
	db := qntxtest.CreateTestDB(t)

	// Setup job and execution
	jobStore := NewStore(db)
	job := &Job{
		ID:              "SPJ_test123",
		ATSCode:         "ix https://example.com/jobs",
		IntervalSeconds: 3600,
		NextRunAt:       time.Now().Add(1 * time.Hour),
		State:           StateActive,
	}
	require.NoError(t, jobStore.CreateJob(job))

	execStore := NewExecutionStore(db)
	startedAt := time.Now().Format(time.RFC3339)

	exec := &Execution{
		ID:             "PEX_test456",
		ScheduledJobID: job.ID,
		Status:         ExecutionStatusRunning,
		StartedAt:      startedAt,
		CreatedAt:      startedAt,
		UpdatedAt:      startedAt,
	}
	require.NoError(t, execStore.CreateExecution(exec))

	// Update to failed
	completedAt := time.Now().Format(time.RFC3339)
	durationMs := 500
	errorMsg := "Failed to fetch page: timeout"

	exec.Status = ExecutionStatusFailed
	exec.CompletedAt = &completedAt
	exec.DurationMs = &durationMs
	exec.ErrorMessage = &errorMsg
	exec.UpdatedAt = completedAt

	err := execStore.UpdateExecution(exec)
	require.NoError(t, err)

	// Verify error captured
	retrieved, err := execStore.GetExecution(exec.ID)
	require.NoError(t, err)
	assert.Equal(t, ExecutionStatusFailed, retrieved.Status)
	assert.NotNil(t, retrieved.ErrorMessage)
	assert.Equal(t, errorMsg, *retrieved.ErrorMessage)
}

func TestListExecutions(t *testing.T) {
	db := qntxtest.CreateTestDB(t)

	// Setup job
	jobStore := NewStore(db)
	job := &Job{
		ID:              "SPJ_test123",
		ATSCode:         "ix https://example.com/jobs",
		IntervalSeconds: 3600,
		NextRunAt:       time.Now().Add(1 * time.Hour),
		State:           StateActive,
	}
	require.NoError(t, jobStore.CreateJob(job))

	// Create multiple executions
	execStore := NewExecutionStore(db)
	now := time.Now()

	executions := []*Execution{
		{
			ID:             "PEX_1",
			ScheduledJobID: job.ID,
			Status:         ExecutionStatusCompleted,
			StartedAt:      now.Add(-2 * time.Hour).Format(time.RFC3339),
			CreatedAt:      now.Add(-2 * time.Hour).Format(time.RFC3339),
			UpdatedAt:      now.Add(-2 * time.Hour).Format(time.RFC3339),
		},
		{
			ID:             "PEX_2",
			ScheduledJobID: job.ID,
			Status:         ExecutionStatusFailed,
			StartedAt:      now.Add(-1 * time.Hour).Format(time.RFC3339),
			CreatedAt:      now.Add(-1 * time.Hour).Format(time.RFC3339),
			UpdatedAt:      now.Add(-1 * time.Hour).Format(time.RFC3339),
		},
		{
			ID:             "PEX_3",
			ScheduledJobID: job.ID,
			Status:         ExecutionStatusRunning,
			StartedAt:      now.Format(time.RFC3339),
			CreatedAt:      now.Format(time.RFC3339),
			UpdatedAt:      now.Format(time.RFC3339),
		},
	}

	for _, exec := range executions {
		require.NoError(t, execStore.CreateExecution(exec))
	}

	// Test listing all executions
	retrieved, total, err := execStore.ListExecutions(job.ID, 10, 0, "")
	require.NoError(t, err)
	assert.Equal(t, 3, total)
	assert.Len(t, retrieved, 3)

	// Should be ordered by started_at DESC (most recent first)
	assert.Equal(t, "PEX_3", retrieved[0].ID)
	assert.Equal(t, "PEX_2", retrieved[1].ID)
	assert.Equal(t, "PEX_1", retrieved[2].ID)
}

func TestListExecutionsWithPagination(t *testing.T) {
	db := qntxtest.CreateTestDB(t)

	// Setup job
	jobStore := NewStore(db)
	job := &Job{
		ID:              "SPJ_test123",
		ATSCode:         "ix https://example.com/jobs",
		IntervalSeconds: 3600,
		NextRunAt:       time.Now().Add(1 * time.Hour),
		State:           StateActive,
	}
	require.NoError(t, jobStore.CreateJob(job))

	// Create 5 executions
	execStore := NewExecutionStore(db)
	now := time.Now()

	for i := 0; i < 5; i++ {
		exec := &Execution{
			ID:             fmt.Sprintf("PEX_%d", i),
			ScheduledJobID: job.ID,
			Status:         ExecutionStatusCompleted,
			StartedAt:      now.Add(time.Duration(-i) * time.Hour).Format(time.RFC3339),
			CreatedAt:      now.Add(time.Duration(-i) * time.Hour).Format(time.RFC3339),
			UpdatedAt:      now.Add(time.Duration(-i) * time.Hour).Format(time.RFC3339),
		}
		require.NoError(t, execStore.CreateExecution(exec))
	}

	// First page (limit 2, offset 0)
	page1, total, err := execStore.ListExecutions(job.ID, 2, 0, "")
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Len(t, page1, 2)
	assert.Equal(t, "PEX_0", page1[0].ID)
	assert.Equal(t, "PEX_1", page1[1].ID)

	// Second page (limit 2, offset 2)
	page2, total, err := execStore.ListExecutions(job.ID, 2, 2, "")
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Len(t, page2, 2)
	assert.Equal(t, "PEX_2", page2[0].ID)
	assert.Equal(t, "PEX_3", page2[1].ID)
}

func TestListExecutionsWithStatusFilter(t *testing.T) {
	db := qntxtest.CreateTestDB(t)

	// Setup job
	jobStore := NewStore(db)
	job := &Job{
		ID:              "SPJ_test123",
		ATSCode:         "ix https://example.com/jobs",
		IntervalSeconds: 3600,
		NextRunAt:       time.Now().Add(1 * time.Hour),
		State:           StateActive,
	}
	require.NoError(t, jobStore.CreateJob(job))

	// Create executions with different statuses
	execStore := NewExecutionStore(db)
	now := time.Now()

	statuses := []string{
		ExecutionStatusCompleted,
		ExecutionStatusCompleted,
		ExecutionStatusFailed,
		ExecutionStatusRunning,
	}

	for i, status := range statuses {
		exec := &Execution{
			ID:             fmt.Sprintf("PEX_%d", i),
			ScheduledJobID: job.ID,
			Status:         status,
			StartedAt:      now.Add(time.Duration(-i) * time.Hour).Format(time.RFC3339),
			CreatedAt:      now.Add(time.Duration(-i) * time.Hour).Format(time.RFC3339),
			UpdatedAt:      now.Add(time.Duration(-i) * time.Hour).Format(time.RFC3339),
		}
		require.NoError(t, execStore.CreateExecution(exec))
	}

	// Filter by completed
	completed, total, err := execStore.ListExecutions(job.ID, 10, 0, ExecutionStatusCompleted)
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.Len(t, completed, 2)

	// Filter by failed
	failed, total, err := execStore.ListExecutions(job.ID, 10, 0, ExecutionStatusFailed)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, failed, 1)
	assert.Equal(t, ExecutionStatusFailed, failed[0].Status)

	// Filter by running
	running, total, err := execStore.ListExecutions(job.ID, 10, 0, ExecutionStatusRunning)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, running, 1)
	assert.Equal(t, ExecutionStatusRunning, running[0].Status)
}

func TestGetExecutionNotFound(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	execStore := NewExecutionStore(db)

	_, err := execStore.GetExecution("PEX_nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "execution not found")
}

func TestUpdateExecutionNotFound(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	execStore := NewExecutionStore(db)

	exec := &Execution{
		ID:        "PEX_nonexistent",
		Status:    ExecutionStatusCompleted,
		UpdatedAt: time.Now().Format(time.RFC3339),
	}

	err := execStore.UpdateExecution(exec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "execution not found")
}

func TestCleanupOldExecutions(t *testing.T) {
	db := qntxtest.CreateTestDB(t)

	// Create a scheduled job first
	jobStore := NewStore(db)
	job := &Job{
		ID:              "SPJ_cleanup_test",
		ATSCode:         "ix https://example.com/jobs",
		IntervalSeconds: 3600,
		NextRunAt:       time.Now().Add(1 * time.Hour),
		State:           StateActive,
	}
	require.NoError(t, jobStore.CreateJob(job))

	execStore := NewExecutionStore(db)
	now := time.Now()

	// Create 3 executions with different ages
	executions := []struct {
		id     string
		age    time.Duration
		status string
	}{
		{"PEX_old1", 100 * 24 * time.Hour, ExecutionStatusCompleted},  // 100 days old - should be deleted
		{"PEX_old2", 95 * 24 * time.Hour, ExecutionStatusFailed},      // 95 days old - should be deleted
		{"PEX_recent", 30 * 24 * time.Hour, ExecutionStatusCompleted}, // 30 days old - should be kept
	}

	for _, e := range executions {
		startedAt := now.Add(-e.age).Format(time.RFC3339)
		exec := &Execution{
			ID:             e.id,
			ScheduledJobID: job.ID,
			Status:         e.status,
			StartedAt:      startedAt,
			CreatedAt:      startedAt,
			UpdatedAt:      startedAt,
		}
		require.NoError(t, execStore.CreateExecution(exec))
	}

	// Cleanup executions older than 90 days (3 months)
	deleted, err := execStore.CleanupOldExecutions(90)
	require.NoError(t, err)
	assert.Equal(t, 2, deleted, "should delete 2 old executions")

	// Verify only recent execution remains
	execs, total, err := execStore.ListExecutions(job.ID, 10, 0, "")
	require.NoError(t, err)
	assert.Equal(t, 1, total, "should have 1 remaining execution")
	assert.Len(t, execs, 1)
	assert.Equal(t, "PEX_recent", execs[0].ID)
}

func TestCleanupOldExecutions_NoneToDelete(t *testing.T) {
	db := qntxtest.CreateTestDB(t)

	// Create a scheduled job first
	jobStore := NewStore(db)
	job := &Job{
		ID:              "SPJ_cleanup_empty_test",
		ATSCode:         "ix https://example.com/jobs",
		IntervalSeconds: 3600,
		NextRunAt:       time.Now().Add(1 * time.Hour),
		State:           StateActive,
	}
	require.NoError(t, jobStore.CreateJob(job))

	execStore := NewExecutionStore(db)
	now := time.Now()

	// Create 2 recent executions
	for i := 0; i < 2; i++ {
		startedAt := now.Add(-time.Duration(i*10) * 24 * time.Hour).Format(time.RFC3339) // 0 and 10 days old
		exec := &Execution{
			ID:             fmt.Sprintf("PEX_recent%d", i),
			ScheduledJobID: job.ID,
			Status:         ExecutionStatusCompleted,
			StartedAt:      startedAt,
			CreatedAt:      startedAt,
			UpdatedAt:      startedAt,
		}
		require.NoError(t, execStore.CreateExecution(exec))
	}

	// Cleanup executions older than 90 days - should delete nothing
	deleted, err := execStore.CleanupOldExecutions(90)
	require.NoError(t, err)
	assert.Equal(t, 0, deleted, "should delete 0 executions when all are recent")

	// Verify both executions remain
	execs, total, err := execStore.ListExecutions(job.ID, 10, 0, "")
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.Len(t, execs, 2)
}
