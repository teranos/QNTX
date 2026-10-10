package grpc

import (
	"context"
	"database/sql"
	"time"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

// PluginProxyHandler forwards job execution to a plugin via gRPC.
// This allows plugins to register async handlers without Go code changes.
//
// Architecture:
// - Pulse worker picks up job with handler_name "python.script"
// - PluginProxyHandler routes job to Python plugin via ExecuteJob RPC
// - Plugin executes script and returns result
// - Handler updates job state (progress, error) and writes logs to task_logs
// PluginHandlerName returns the namespaced registry key for a plugin handler.
// This is the single source of truth for the naming convention.
func PluginHandlerName(pluginName, handlerName string) string {
	return pluginName + "/" + handlerName
}

type PluginProxyHandler struct {
	pluginName  string // plugin that owns this handler
	handlerName string // raw name the plugin uses internally
	plugin      *ExternalDomainProxy
	db          *sql.DB
	logger      *zap.SugaredLogger
	openRun     OpenRun // mints a run's store token for a job a caller's schedule started
}

// NewPluginProxyHandler creates a handler that forwards execution to a plugin.
// The registry key is automatically namespaced as "pluginName/handlerName"
// so multiple plugins can declare the same raw handler name without collision.
func NewPluginProxyHandler(pluginName, handlerName string, plugin *ExternalDomainProxy, db *sql.DB, logger *zap.SugaredLogger, openRun OpenRun) *PluginProxyHandler {
	return &PluginProxyHandler{
		pluginName:  pluginName,
		handlerName: handlerName,
		plugin:      plugin,
		db:          db,
		logger:      logger,
		openRun:     openRun,
	}
}

// Name returns the namespaced registry key (e.g. "duif/route-changed").
func (h *PluginProxyHandler) Name() string {
	return PluginHandlerName(h.pluginName, h.handlerName)
}

// Execute forwards the job to the plugin for execution.
func (h *PluginProxyHandler) Execute(ctx context.Context, job *async.Job) error {
	timeout := int64(300) // TODO: Make configurable or derive from job
	req := &protocol.ExecuteJobRequest{
		JobId:       job.ID,
		HandlerName: h.handlerName, // raw name — the plugin doesn't know about namespacing
		Payload:     job.Payload,
		TimeoutSecs: &timeout,
	}

	// A job a caller's schedule started runs where that caller acted, as them.
	if job.Namespace != "" {
		token, done, err := h.openRun(job.UserID, job.Namespace)
		if err != nil {
			return errors.Wrapf(err, "no store token for job %s in namespace %s", job.ID, job.Namespace)
		}
		defer done()
		req.StoreToken = token
		req.UserId = job.UserID
	}

	client := h.plugin.Client()
	resp, err := client.ExecuteJob(ctx, req)
	if err != nil {
		return errors.Wrapf(err, "plugin execution failed for handler %s", h.handlerName)
	}

	// Write plugin logs to task_logs table (even on failure)
	if err := h.writeLogs(job.ID, resp.LogEntries); err != nil {
		return errors.Wrapf(err, "the log of job %s (handler %s) was not kept", job.ID, h.handlerName)
	}

	// Stamp plugin version directly on the DB record, as the plugin said it.
	// Can't rely on the worker to persist this — CompleteJob/FailJob re-fetch from DB,
	// discarding any in-memory mutations the handler made to the job struct.
	// A job whose version is not recorded is one where which build ran it is
	// unknowable, so it fails.
	job.PluginVersion = resp.PluginVersion
	stamped, err := h.db.Exec(`UPDATE async_ix_jobs SET plugin_version = ? WHERE id = ?`, resp.PluginVersion, job.ID)
	if err != nil {
		return errors.Wrapf(err, "job %s has no plugin version %q recorded", job.ID, resp.PluginVersion)
	}
	if err := oneRow(stamped); err != nil {
		return errors.Wrapf(err, "job %s has no plugin version %q recorded", job.ID, resp.PluginVersion)
	}

	if !resp.Success {
		return errors.Newf("plugin execution failed (job=%s, handler=%s): %q", job.ID, h.handlerName, resp.Error)
	}

	// The progress the plugin said, as it said it.
	job.Progress = async.Progress{
		Current: int(resp.ProgressCurrent),
		Total:   int(resp.ProgressTotal),
	}

	return nil
}

// oneRow is a write that touched exactly one row, or why it did not.
func oneRow(result sql.Result) error {
	n, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "rows written not known")
	}
	if n != 1 {
		return errors.Newf("%d rows written where one was meant", n)
	}
	return nil
}

// writeLogs persists plugin log entries to the task_logs table, each with
// the metadata it carried, as it carried it.
func (h *PluginProxyHandler) writeLogs(jobID string, entries []*protocol.JobLogEntry) error {
	for i, entry := range entries {
		// The time a line was said is the plugin's to say. Stamping it with when it
		// arrived would put it after the job that said it. A line saying no
		// time, or not one in RFC 3339, is not written.
		ts := entry.Timestamp
		said, err := time.Parse(time.RFC3339, ts)
		if err != nil {
			h.logger.Errorw("A plugin's job log line says no RFC 3339 time, so it is not written to task_logs",
				"job_id", jobID, "plugin", h.pluginName, "handler", h.handlerName,
				"timestamp", ts, "stage", entry.Stage, "level", entry.Level, "message", entry.Message,
				"error", err)
			continue
		}

		written, err := h.db.Exec(
			`INSERT INTO task_logs (job_id, stage, timestamp, level, message, metadata) VALUES (?, ?, ?, ?, ?, ?)`,
			jobID, entry.Stage, said.Format(time.RFC3339Nano), entry.Level, entry.Message, entry.Metadata,
		)
		if err != nil {
			return errors.Wrapf(err, "line %d of job %s (stage %s) not written to task_logs", i, jobID, entry.Stage)
		}
		if err := oneRow(written); err != nil {
			return errors.Wrapf(err, "line %d of job %s (stage %s) not written to task_logs", i, jobID, entry.Stage)
		}
	}
	return nil
}
