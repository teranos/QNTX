-- Create storage_events table for bounded storage observability
-- Tracks when attestations are deleted due to storage limits

CREATE TABLE IF NOT EXISTS storage_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type TEXT NOT NULL,        -- 'actor_context_limit', 'actor_contexts_limit', 'entity_actors_limit'
    actor TEXT,                       -- Actor involved (may be null for entity limits)
    context TEXT,                     -- Context involved (may be null for actor limits)
    entity TEXT,                      -- Entity/subject involved (may be null for context limits)
    deletions_count INTEGER NOT NULL, -- Number of attestations deleted
    timestamp TEXT NOT NULL,          -- When the enforcement happened
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Indexes for efficient querying
CREATE INDEX IF NOT EXISTS idx_storage_events_type ON storage_events(event_type);
CREATE INDEX IF NOT EXISTS idx_storage_events_actor ON storage_events(actor);
CREATE INDEX IF NOT EXISTS idx_storage_events_entity ON storage_events(entity);
CREATE INDEX IF NOT EXISTS idx_storage_events_timestamp ON storage_events(timestamp);
