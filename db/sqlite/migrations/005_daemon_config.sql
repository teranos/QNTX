-- Migration 005: Daemon configuration and state
-- Stores daemon desired state (enabled/disabled) for persistence across restarts
--
-- DEAD. This migration still runs and still does nothing that lasts:
-- 063_drop_daemon_config.sql removes what it creates, a few migrations later in
-- the same run. Nothing in any language reads or writes daemon_config. Pulse
-- starts because the node starts, so there is no desired state to keep.
--
-- Kept because it has already been applied on deployments and a migration list
-- is a history, not a wish. This file and 063 are a pair and mean nothing apart:
-- when the migrations are normalised — a 1.0.0 blocker — both go together.

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
