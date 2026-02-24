-- Persistent execution queue for watchers: replaces in-memory retry with SQLite-backed
-- deferral. Rate-limited attestations are enqueued instead of dropped; failed executions
-- are enqueued with exponential backoff instead of held in memory.

CREATE TABLE IF NOT EXISTS watcher_execution_queue (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    watcher_id TEXT NOT NULL,
    attestation_json TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued',
    reason TEXT NOT NULL DEFAULT 'rate_limited',
    attempt INTEGER NOT NULL DEFAULT 0,
    not_before TEXT NOT NULL,
    last_error TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (watcher_id) REFERENCES watchers(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_weq_drain ON watcher_execution_queue(status, not_before);
CREATE INDEX IF NOT EXISTS idx_weq_watcher ON watcher_execution_queue(watcher_id);
