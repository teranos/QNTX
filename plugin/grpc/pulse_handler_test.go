package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
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

	handler := NewPluginProxyHandler("test", "handler", nil, db, logger, nil)

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

// Two plugins both declare a handler with the same raw name.
// The constructor namespaces the registry key as "pluginName/handlerName"
// so both coexist without collision.
func TestTwoPluginsSameHandlerName(t *testing.T) {
	registry := async.NewHandlerRegistry()
	db := qntxtest.CreateTestDB(t)
	logger := zaptest.NewLogger(t).Sugar()

	rawName := "data-sync"

	gazeHandler := NewPluginProxyHandler("gaze", rawName, nil, db, logger, nil)
	registry.Register(gazeHandler)

	scryHandler := NewPluginProxyHandler("scry", rawName, nil, db, logger, nil)
	registry.Register(scryHandler) // must not panic

	assert.True(t, registry.Has("gaze/data-sync"))
	assert.True(t, registry.Has("scry/data-sync"))
	assert.False(t, registry.Has("data-sync"), "raw name must not appear in registry")
	assert.Equal(t, 2, len(registry.Names()))
}

func TestWriteLogsEmpty(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zaptest.NewLogger(t).Sugar()

	handler := NewPluginProxyHandler("test", "handler", nil, db, logger, nil)

	// Empty entries should be a no-op (no panic, no DB writes)
	handler.writeLogs("JOB_nonexistent", nil)
	handler.writeLogs("JOB_nonexistent", []*protocol.JobLogEntry{})

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM task_logs`).Scan(&count))
	assert.Equal(t, 0, count)
}

// runPlugin is a plugin that keeps the job requests it is handed.
type runPlugin struct {
	protocol.DomainPluginServiceClient
	handed []*protocol.ExecuteJobRequest
	during func(*protocol.ExecuteJobRequest)
}

func (p *runPlugin) ExecuteJob(_ context.Context, req *protocol.ExecuteJobRequest, _ ...grpc.CallOption) (*protocol.ExecuteJobResponse, error) {
	p.handed = append(p.handed, req)
	if p.during != nil {
		p.during(req)
	}
	return &protocol.ExecuteJobResponse{Success: true}, nil
}

// A run of a caller's schedule is handed a store token for the namespace that
// caller acted in, and their User, and the token ends with the run.
func TestARunOfACallersScheduleCarriesTheirStoreTokenAndUser(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	open := map[string]string{}
	openRun := func(userID, namespace string) (string, func(), error) {
		open["run-token"] = userID + "@" + namespace
		return "run-token", func() { delete(open, "run-token") }, nil
	}
	p := &runPlugin{}
	var openDuring bool
	p.during = func(req *protocol.ExecuteJobRequest) { _, openDuring = open[req.StoreToken] }
	h := NewPluginProxyHandler("datapunt", "observe", &ExternalDomainProxy{client: p}, db, zap.NewNop().Sugar(), openRun)

	err := h.Execute(context.Background(), &async.Job{ID: "JBtim", UserID: "UStim", Namespace: "defacile"})
	require.NoError(t, err)

	require.Len(t, p.handed, 1)
	assert.Equal(t, "run-token", p.handed[0].StoreToken)
	assert.Equal(t, "UStim", p.handed[0].UserId)
	assert.True(t, openDuring, "the run's token was not open while the plugin ran")
	assert.Empty(t, open, "the run's token outlived the run")
}

// A job no caller made is handed no store token.
func TestARunNoCallerMadeCarriesNoStoreToken(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	p := &runPlugin{}
	h := NewPluginProxyHandler("datapunt", "observe", &ExternalDomainProxy{client: p}, db, zap.NewNop().Sugar(), nil)

	require.NoError(t, h.Execute(context.Background(), &async.Job{ID: "JBnobody"}))
	require.Len(t, p.handed, 1)
	assert.Empty(t, p.handed[0].StoreToken)
	assert.Empty(t, p.handed[0].UserId)
}

// A run whose namespace the node no longer serves does not run elsewhere.
func TestARunWhoseNamespaceIsNotServedDoesNotRun(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	p := &runPlugin{}
	openRun := func(_, namespace string) (string, func(), error) {
		return "", nil, errors.Newf("namespace %s is not served", namespace)
	}
	h := NewPluginProxyHandler("datapunt", "observe", &ExternalDomainProxy{client: p}, db, zap.NewNop().Sugar(), openRun)

	err := h.Execute(context.Background(), &async.Job{ID: "JBgone", UserID: "UStim", Namespace: "gone"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gone")
	assert.Empty(t, p.handed)
}
