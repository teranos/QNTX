package grpc

import (
	"context"
	"database/sql"
	"time"

	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/pulse/async"
	"go.uber.org/zap"
)

// PluginProxyHandler forwards job execution to a plugin via gRPC.
// This allows plugins to register async handlers without Go code changes.
//
// Architecture:
// - Pulse worker picks up job with handler_name "python.script"
// - PluginProxyHandler routes job to Python plugin via ExecuteJob RPC
// - Plugin executes script and returns result
// - Handler updates job state (progress, cost, error) and writes logs to task_logs
type PluginProxyHandler struct {
	handlerName string
	plugin      *ExternalDomainProxy
	db          *sql.DB
	logger      *zap.SugaredLogger
}

// NewPluginProxyHandler creates a handler that forwards execution to a plugin.
func NewPluginProxyHandler(handlerName string, plugin *ExternalDomainProxy, db *sql.DB, logger *zap.SugaredLogger) *PluginProxyHandler {
	return &PluginProxyHandler{
		handlerName: handlerName,
		plugin:      plugin,
		db:          db,
		logger:      logger,
	}
}

// Name returns the handler identifier (e.g., "python.script", "python.webhook").
func (h *PluginProxyHandler) Name() string {
	return h.handlerName
}

// Execute forwards the job to the plugin for execution.
func (h *PluginProxyHandler) Execute(ctx context.Context, job *async.Job) error {
	timeout := int64(300) // TODO: Make configurable or derive from job
	req := &protocol.ExecuteJobRequest{
		JobId:       job.ID,
		HandlerName: h.handlerName,
		Payload:     job.Payload,
		TimeoutSecs: &timeout,
	}

	client := h.plugin.Client()
	resp, err := client.ExecuteJob(ctx, req)
	if err != nil {
		return errors.Wrapf(err, "plugin execution failed for handler %s", h.handlerName)
	}

	// Write plugin logs to task_logs table (even on failure)
	h.writeLogs(job.ID, resp.LogEntries)

	// Stamp plugin version directly on the DB record.
	// Can't rely on the worker to persist this — CompleteJob/FailJob re-fetch from DB,
	// discarding any in-memory mutations the handler made to the job struct.
	if resp.PluginVersion != "" {
		job.PluginVersion = resp.PluginVersion
		if _, err := h.db.Exec(`UPDATE async_ix_jobs SET plugin_version = ? WHERE id = ?`, resp.PluginVersion, job.ID); err != nil {
			h.logger.Warnw("Failed to stamp plugin version on job", "job_id", job.ID, "version", resp.PluginVersion, "error", err)
		}
	}

	if !resp.Success {
		if resp.Error != "" {
			return errors.Newf("plugin execution error (job=%s, handler=%s): %s", job.ID, h.handlerName, resp.Error)
		}
		return errors.Newf("plugin execution failed with no error message (job=%s, handler=%s)", job.ID, h.handlerName)
	}

	if resp.ProgressTotal > 0 {
		job.Progress = async.Progress{
			Current: int(resp.ProgressCurrent),
			Total:   int(resp.ProgressTotal),
		}
	}

	if resp.CostActual > 0 {
		job.CostActual = resp.CostActual
	}

	return nil
}

// writeLogs persists plugin log entries to the task_logs table.
func (h *PluginProxyHandler) writeLogs(jobID string, entries []*protocol.JobLogEntry) {
	if len(entries) == 0 {
		return
	}

	for _, entry := range entries {
		ts := entry.Timestamp
		if ts == "" {
			ts = time.Now().Format(time.RFC3339)
		}

		var metaPtr *string
		if entry.Metadata != "" {
			metaPtr = &entry.Metadata
		}

		_, err := h.db.Exec(
			`INSERT INTO task_logs (job_id, stage, timestamp, level, message, metadata) VALUES (?, ?, ?, ?, ?, ?)`,
			jobID, entry.Stage, ts, entry.Level, entry.Message, metaPtr,
		)
		if err != nil {
			h.logger.Warnw("Failed to write plugin task log",
				"job_id", jobID,
				"handler", h.handlerName,
				"error", err,
			)
		}
	}
}
