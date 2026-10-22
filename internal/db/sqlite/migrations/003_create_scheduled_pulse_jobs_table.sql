-- TODO: Consider renaming created_from_doc_id -> created_from
-- Rationale: Column stores document IDs, glyph IDs, or special markers like '__force_trigger__'
-- Current name implies it only stores document IDs, but it's more generic
-- Would require migration to rename column and update all Go/TS code

CREATE TABLE IF NOT EXISTS scheduled_pulse_jobs (
    id TEXT PRIMARY KEY,
    ats_code TEXT NOT NULL,
    interval_seconds INTEGER NOT NULL,
    next_run_at TIMESTAMP,
    last_run_at TIMESTAMP,
    last_execution_id TEXT,
    state TEXT NOT NULL DEFAULT 'active',
    created_from_doc_id TEXT,
    metadata TEXT,
    handler_name TEXT,
    payload TEXT,
    source_url TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_scheduled_next_run
ON scheduled_pulse_jobs(state, next_run_at)
WHERE state = 'active';

CREATE INDEX IF NOT EXISTS idx_scheduled_doc
ON scheduled_pulse_jobs(created_from_doc_id)
WHERE created_from_doc_id IS NOT NULL;
