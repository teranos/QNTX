-- Migration 043: Daemon configuration and state
-- Stores daemon desired state (enabled/disabled) for persistence across restarts

CREATE TABLE IF NOT EXISTS daemon_config (
    id INTEGER PRIMARY KEY CHECK (id = 1), -- Single row table
    enabled BOOLEAN NOT NULL DEFAULT 1,     -- Desired daemon state: 1=running, 0=stopped
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Insert default state (enabled)
INSERT INTO daemon_config (id, enabled) VALUES (1, 1)
ON CONFLICT(id) DO NOTHING;

-- Index for quick lookups (though single-row table doesn't really need it)
CREATE INDEX IF NOT EXISTS idx_daemon_config_enabled ON daemon_config(enabled);
