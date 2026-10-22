-- On parquet a watcher is an object under <location>/watchers/, not a row here,
-- so a key to watchers(id) refused every enqueue (QNTX-GO-X). The drain
-- completes an entry whose watcher is not loaded, which is what the cascade did.

-- SQLite cannot ALTER foreign keys; recreate the table without it.

CREATE TABLE watcher_execution_queue_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    watcher_id TEXT NOT NULL,
    attestation_json TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued',
    reason TEXT NOT NULL DEFAULT 'rate_limited',
    attempt INTEGER NOT NULL DEFAULT 0,
    not_before TEXT NOT NULL,
    last_error TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

INSERT INTO watcher_execution_queue_new
    SELECT id, watcher_id, attestation_json, status, reason, attempt, not_before, last_error, created_at, updated_at
    FROM watcher_execution_queue;

DROP TABLE watcher_execution_queue;
ALTER TABLE watcher_execution_queue_new RENAME TO watcher_execution_queue;

CREATE INDEX idx_weq_drain ON watcher_execution_queue(status, not_before);
CREATE INDEX idx_weq_watcher ON watcher_execution_queue(watcher_id);
