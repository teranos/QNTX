CREATE TABLE async_ix_jobs (
    id TEXT PRIMARY KEY,
    source TEXT NOT NULL,
    status TEXT NOT NULL,
    progress_current INTEGER DEFAULT 0,
    progress_total INTEGER DEFAULT 0,
    cost_estimate REAL DEFAULT 0.0,
    cost_actual REAL DEFAULT 0.0,
    pulse_state TEXT,
    error TEXT,
    parent_job_id TEXT,
    retry_count INTEGER DEFAULT 0,
    handler_name TEXT,
    payload TEXT,
    created_at DATETIME NOT NULL,
    started_at DATETIME,
    completed_at DATETIME,
    updated_at DATETIME NOT NULL
);

CREATE INDEX idx_async_ix_jobs_status ON async_ix_jobs(status);
CREATE INDEX idx_async_ix_jobs_created ON async_ix_jobs(created_at DESC);
CREATE INDEX idx_async_ix_jobs_parent ON async_ix_jobs(parent_job_id);
