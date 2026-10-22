-- Migration: Create pulse_executions table for tracking scheduled job execution history
--
-- Stores detailed records of each scheduled job execution including:
-- - Execution status and timing
-- - Captured logs and output
-- - Error messages for failed runs
-- - Link to async job for detailed processing info
--
-- Business Context:
-- - Users need visibility into past job runs for debugging
-- - Execution history helps identify patterns in failures
-- - Performance tracking enables query optimization

CREATE TABLE IF NOT EXISTS pulse_executions (
    -- Identity
    id TEXT PRIMARY KEY,                    -- PEX_{random}_{timestamp} format
    scheduled_job_id TEXT NOT NULL,         -- FK to scheduled_jobs(id)
    async_job_id TEXT,                      -- Optional FK to async_jobs(id) for detailed output

    -- Execution status
    status TEXT NOT NULL CHECK (status IN ('running', 'completed', 'failed')),

    -- Timing information
    started_at TEXT NOT NULL,               -- RFC3339 timestamp when execution began
    completed_at TEXT,                      -- RFC3339 timestamp when execution finished (null if still running)
    duration_ms INTEGER,                    -- Total execution time in milliseconds (null if still running)

    -- Output capture
    logs TEXT,                              -- Captured stdout/stderr (may be truncated if >10KB)
    result_summary TEXT,                    -- Brief summary (e.g., "Ingested 3 JDs from https://example.com")
    error_message TEXT,                     -- Error details if status = 'failed'

    -- Metadata
    created_at TEXT NOT NULL,               -- RFC3339 timestamp (record creation)
    updated_at TEXT NOT NULL,               -- RFC3339 timestamp (last update)

    FOREIGN KEY (scheduled_job_id) REFERENCES scheduled_pulse_jobs(id) ON DELETE CASCADE
);

-- Indexes for common query patterns
CREATE INDEX IF NOT EXISTS idx_pulse_executions_job ON pulse_executions(scheduled_job_id);      -- List executions for a job
CREATE INDEX IF NOT EXISTS idx_pulse_executions_status ON pulse_executions(status);             -- Filter by status (running/failed)
CREATE INDEX IF NOT EXISTS idx_pulse_executions_started ON pulse_executions(started_at DESC);   -- Sort by most recent first
