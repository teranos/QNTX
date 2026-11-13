-- Keep the local schedule history in the same shape as the Parquet tick stream.
-- The scheduled_pulse_jobs row remains the hot projection; this table is the
-- durable event history used to rebuild that projection when needed.
CREATE TABLE IF NOT EXISTS schedule_ticks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    schedule_id TEXT NOT NULL,
    at_ms INTEGER NOT NULL,
    execution_id TEXT,
    next_run_at_ms INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_schedule_ticks_schedule_at
ON schedule_ticks(schedule_id, at_ms);
