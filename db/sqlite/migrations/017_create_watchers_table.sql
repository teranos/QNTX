CREATE TABLE IF NOT EXISTS watchers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,

    -- Filter (empty/null = match all)
    subjects JSON,
    predicates JSON,
    contexts JSON,
    actors JSON,
    time_start TEXT,
    time_end TEXT,

    -- Action
    action_type TEXT NOT NULL,
    action_data TEXT NOT NULL,

    -- Rate limiting
    max_fires_per_minute INTEGER NOT NULL DEFAULT 105,

    -- State
    enabled INTEGER NOT NULL DEFAULT 1,

    -- Stats
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    last_fired_at TEXT,
    fire_count INTEGER NOT NULL DEFAULT 0,
    error_count INTEGER NOT NULL DEFAULT 0,
    last_error TEXT
);

CREATE INDEX IF NOT EXISTS idx_watchers_enabled ON watchers(enabled);
