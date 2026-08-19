-- A fire was a counter: fire_count went up and last_fired_at moved. The
-- attestation that caused it reached a log line and stopped there, so a watcher
-- could say it fired 48 times and name none of them.
CREATE TABLE IF NOT EXISTS watcher_fires (
    watcher_id     TEXT NOT NULL,
    at_ms          INTEGER NOT NULL,
    -- What caused it. NULL for a run nothing triggered.
    attestation_id TEXT,
    -- NULL on a fire, set on a failed execution.
    error          TEXT
);

-- The panel asks for one watcher's last three, newest first.
CREATE INDEX IF NOT EXISTS idx_watcher_fires_recent
    ON watcher_fires(watcher_id, at_ms DESC);
