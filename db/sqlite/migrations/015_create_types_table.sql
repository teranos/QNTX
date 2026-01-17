-- Create types table to store type definitions with searchable fields
-- This enables the fuzzy search functionality for RichStringFields
CREATE TABLE IF NOT EXISTS types (
    name TEXT PRIMARY KEY,
    label TEXT NOT NULL,
    description TEXT,
    rich_string_fields TEXT, -- JSON array of field names that should be searchable
    array_fields TEXT,        -- JSON array of field names that contain arrays
    created_at INTEGER DEFAULT (strftime('%s', 'now')),
    updated_at INTEGER DEFAULT (strftime('%s', 'now'))
);

-- Index for faster lookups
CREATE INDEX IF NOT EXISTS idx_types_created_at ON types(created_at);