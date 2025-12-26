-- Migration: Create task_logs table for comprehensive job execution logging
--
-- Enables granular log capture for all job executions including:
-- - Per-job logging (all output from a job execution)
-- - Per-stage logging (logs grouped by execution phase)
-- - Per-task logging (logs for atomic work units like individual candidates)
-- - Structured metadata for rich context (API responses, scores, timings)
--
-- Business Context:
-- - Users need complete visibility into job execution for debugging
-- - Logs are critical for understanding failures, performance issues, and business logic
-- - Per-task granularity enables filtering to specific candidates or work units
-- - Stage-aware logging helps identify which phase failed
--
-- Technical Details:
-- - Supports unlimited log entries (no size constraints)
-- - Indexed for fast retrieval by job, task, or time range
-- - Cascade delete ensures logs are removed with parent jobs
-- - Metadata JSON field supports arbitrary structured context
-- - NO TRUNCATION - full logs stored for comprehensive troubleshooting
--
-- Future Cleanup:
-- - TTL-based cleanup (3 months retention) will be implemented separately
-- - Monitor database growth and adjust retention as needed

CREATE TABLE IF NOT EXISTS task_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,

    -- Job context
    job_id TEXT NOT NULL,              -- Links to async_ix_jobs.id

    -- Execution context
    stage TEXT,                        -- Execution phase: "fetch_jd", "extract_requirements", "score_candidates"
    task_id TEXT,                      -- Work unit ID: candidate_id, jd_id, or other atomic work identifier

    -- Log entry
    timestamp DATETIME NOT NULL,       -- RFC3339 timestamp when log entry was created
    level TEXT NOT NULL,               -- Log level: "info", "warn", "error", "debug"
    message TEXT NOT NULL,             -- Human-readable log message
    metadata TEXT,                     -- JSON blob for structured context (API responses, scores, etc.)

    FOREIGN KEY (job_id) REFERENCES async_ix_jobs(id) ON DELETE CASCADE
);

-- Index for retrieving all logs for a specific job (most common query)
CREATE INDEX idx_task_logs_job_id ON task_logs(job_id);

-- Index for filtering logs by task (e.g., "show me logs for candidate X")
CREATE INDEX idx_task_logs_task_id ON task_logs(task_id);

-- Index for time-based queries and TTL cleanup
CREATE INDEX idx_task_logs_timestamp ON task_logs(timestamp DESC);

-- Composite index for stage-filtered queries (e.g., "show me all score_candidates stage logs")
CREATE INDEX idx_task_logs_job_stage ON task_logs(job_id, stage);
