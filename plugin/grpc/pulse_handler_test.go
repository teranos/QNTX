package grpc

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/pulse/async"
	"go.uber.org/zap/zaptest"
)

func TestWriteLogs(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zaptest.NewLogger(t).Sugar()

	// Create a job so the FK constraint on task_logs is satisfied
	store := async.NewStore(db)
	job := &async.Job{
		ID:          "JOB_test_write_logs",
		HandlerName: "test.handler",
		Source:      "test",
		Status:      "queued",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	require.NoError(t, store.CreateJob(job))

	handler := &PluginProxyHandler{
		handlerName: "test.handler",
		db:          db,
		logger:      logger,
	}

	entries := []*protocol.JobLogEntry{
		{
			Stage:     "timeline-sync",
			Timestamp: "2026-02-24T15:15:15+01:00",
			Level:     "info",
			Message:   "Starting timeline sync",
		},
		{
			Stage:    "timeline-sync",
			Level:    "info",
			Message:  "Timeline sync completed",
			Metadata: `{"items_synced": 42}`,
			// No timestamp — handler should fill in current time
		},
	}

	handler.writeLogs(job.ID, entries)

	// Verify logs were written
	rows, err := db.Query(`SELECT stage, level, message, metadata FROM task_logs WHERE job_id = ? ORDER BY id`, job.ID)
	require.NoError(t, err)
	defer rows.Close()

	type logRow struct {
		stage, level, message string
		metadata              *string
	}
	var logs []logRow
	for rows.Next() {
		var r logRow
		require.NoError(t, rows.Scan(&r.stage, &r.level, &r.message, &r.metadata))
		logs = append(logs, r)
	}
	require.NoError(t, rows.Err())

	require.Len(t, logs, 2)

	assert.Equal(t, "timeline-sync", logs[0].stage)
	assert.Equal(t, "info", logs[0].level)
	assert.Equal(t, "Starting timeline sync", logs[0].message)
	assert.Nil(t, logs[0].metadata)

	assert.Equal(t, "Timeline sync completed", logs[1].message)
	require.NotNil(t, logs[1].metadata)
	assert.Equal(t, `{"items_synced": 42}`, *logs[1].metadata)
}

func TestWriteLogsEmpty(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zaptest.NewLogger(t).Sugar()

	handler := &PluginProxyHandler{
		handlerName: "test.handler",
		db:          db,
		logger:      logger,
	}

	// Empty entries should be a no-op (no panic, no DB writes)
	handler.writeLogs("JOB_nonexistent", nil)
	handler.writeLogs("JOB_nonexistent", []*protocol.JobLogEntry{})

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM task_logs`).Scan(&count))
	assert.Equal(t, 0, count)
}
