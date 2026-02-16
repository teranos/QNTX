package schedule

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	qntxtest "github.com/teranos/QNTX/internal/testing"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// insertAsyncJob creates a minimal async_ix_jobs row to satisfy the task_logs FK
func insertAsyncJob(t *testing.T, db *sql.DB, jobID string) {
	t.Helper()
	now := time.Now().Format(time.RFC3339)
	_, err := db.Exec(
		`INSERT INTO async_ix_jobs (id, source, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		jobID, "test", "completed", now, now,
	)
	require.NoError(t, err)
}

func TestListStagesForJob(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	insertAsyncJob(t, db, "JB_stages_test")

	now := time.Now()
	// Insert logs across 3 stages, multiple tasks per stage, in a realistic order
	logs := []struct {
		stage  string
		taskID *string
		msg    string
		offset time.Duration
	}{
		// Stage 1: fetch_jd — 2 tasks
		{"fetch_jd", ptr("jd_001"), "Fetching JD", 0},
		{"fetch_jd", ptr("jd_001"), "JD fetched", 1 * time.Second},
		{"fetch_jd", ptr("jd_002"), "Fetching JD", 2 * time.Second},
		// Stage 2: extract_requirements — 1 task
		{"extract_requirements", ptr("jd_001"), "Extracting", 3 * time.Second},
		{"extract_requirements", ptr("jd_001"), "Extracted 5 requirements", 4 * time.Second},
		// Stage 3: score_candidates — 2 tasks
		{"score_candidates", ptr("CNT_abc"), "Scoring candidate", 5 * time.Second},
		{"score_candidates", ptr("CNT_def"), "Scoring candidate", 6 * time.Second},
		{"score_candidates", ptr("CNT_def"), "Score: 0.85", 7 * time.Second},
	}

	for _, l := range logs {
		ts := now.Add(l.offset).Format(time.RFC3339)
		_, err := db.Exec(
			`INSERT INTO task_logs (job_id, stage, task_id, timestamp, level, message) VALUES (?, ?, ?, ?, ?, ?)`,
			"JB_stages_test", l.stage, l.taskID, ts, "info", l.msg,
		)
		require.NoError(t, err)
	}

	store := NewTaskLogStore(db)
	stages, err := store.ListStagesForJob("JB_stages_test")
	require.NoError(t, err)

	// 3 stages in execution order
	require.Len(t, stages, 3)
	assert.Equal(t, "fetch_jd", stages[0].Stage)
	assert.Equal(t, "extract_requirements", stages[1].Stage)
	assert.Equal(t, "score_candidates", stages[2].Stage)

	// fetch_jd: 2 tasks, jd_001 has 2 logs, jd_002 has 1
	require.Len(t, stages[0].Tasks, 2)
	assert.Equal(t, "jd_001", stages[0].Tasks[0].TaskID)
	assert.Equal(t, 2, stages[0].Tasks[0].LogCount)
	assert.Equal(t, "jd_002", stages[0].Tasks[1].TaskID)
	assert.Equal(t, 1, stages[0].Tasks[1].LogCount)

	// extract_requirements: 1 task with 2 logs
	require.Len(t, stages[1].Tasks, 1)
	assert.Equal(t, "jd_001", stages[1].Tasks[0].TaskID)
	assert.Equal(t, 2, stages[1].Tasks[0].LogCount)

	// score_candidates: 2 tasks
	require.Len(t, stages[2].Tasks, 2)
	assert.Equal(t, "CNT_abc", stages[2].Tasks[0].TaskID)
	assert.Equal(t, 1, stages[2].Tasks[0].LogCount)
	assert.Equal(t, "CNT_def", stages[2].Tasks[1].TaskID)
	assert.Equal(t, 2, stages[2].Tasks[1].LogCount)

	// Empty job returns empty slice, not error
	empty, err := store.ListStagesForJob("JB_nonexistent")
	require.NoError(t, err)
	assert.Empty(t, empty)
}

func TestListLogsForTask(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	insertAsyncJob(t, db, "JB_logs_test")

	now := time.Now()
	meta := map[string]any{"score": 0.92, "model": "gpt-4"}
	metaJSON, err := json.Marshal(meta)
	require.NoError(t, err)

	// Insert logs: 2 with explicit task_id, 1 stage-level (task_id NULL, matched by stage)
	entries := []struct {
		taskID   *string
		stage    string
		level    string
		msg      string
		metadata *string
		offset   time.Duration
	}{
		// Stage-level log (task_id is NULL) — should match when querying by stage name
		{nil, "score_candidates", "info", "Stage started", nil, 0},
		// Task-level logs
		{ptr("score_candidates"), "score_candidates", "info", "Scoring started", nil, 1 * time.Second},
		{ptr("score_candidates"), "score_candidates", "info", "Score complete", ptr(string(metaJSON)), 2 * time.Second},
		// Different task — should NOT appear
		{ptr("other_task"), "score_candidates", "info", "Other task log", nil, 3 * time.Second},
	}

	for _, e := range entries {
		ts := now.Add(e.offset).Format(time.RFC3339)
		_, err := db.Exec(
			`INSERT INTO task_logs (job_id, stage, task_id, timestamp, level, message, metadata) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			"JB_logs_test", e.stage, e.taskID, ts, e.level, e.msg, e.metadata,
		)
		require.NoError(t, err)
	}

	store := NewTaskLogStore(db)
	logs, err := store.ListLogsForTask("JB_logs_test", "score_candidates")
	require.NoError(t, err)

	// Should return 3 logs: the NULL task_id row (matched by stage) + 2 explicit task_id rows
	// NOT the "other_task" row
	require.Len(t, logs, 3)

	// Ordered by timestamp ASC
	assert.Equal(t, "Stage started", logs[0].Message)
	assert.Equal(t, "Scoring started", logs[1].Message)
	assert.Equal(t, "Score complete", logs[2].Message)

	// Metadata parsed correctly on the third entry
	assert.NotNil(t, logs[2].Metadata)
	assert.Equal(t, 0.92, logs[2].Metadata["score"])
	assert.Equal(t, "gpt-4", logs[2].Metadata["model"])

	// First two have nil metadata
	assert.Nil(t, logs[0].Metadata)
	assert.Nil(t, logs[1].Metadata)

	// Nonexistent task returns empty, not error
	empty, err := store.ListLogsForTask("JB_logs_test", "nonexistent_task")
	require.NoError(t, err)
	assert.Empty(t, empty)
}
