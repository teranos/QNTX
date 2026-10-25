-- Migration: Add COLLATE NOCASE to aliases table for efficient case-insensitive lookups
--
-- Why this migration:
-- - Original schema used default BINARY collation (case-sensitive)
-- - Queries used inline COLLATE NOCASE which bypasses indexes
-- - This migration adds COLLATE NOCASE at schema level for:
--   1. Index-backed case-insensitive lookups
--   2. Case-insensitive primary key uniqueness (no duplicate "John"/"john")
--   3. Consistent behavior without per-query COLLATE clauses
--
-- Technical approach:
-- - SQLite doesn't support ALTER COLUMN, so we recreate the table
-- - Existing data is preserved (case is kept, but comparisons become case-insensitive)

-- Create new table with proper collation
CREATE TABLE IF NOT EXISTS aliases_new (
    alias TEXT NOT NULL COLLATE NOCASE,
    target TEXT NOT NULL COLLATE NOCASE,
    created_by TEXT NOT NULL DEFAULT 'system',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (alias, target)
);

-- Copy existing data (deduplicating if case variants exist)
INSERT OR IGNORE INTO aliases_new (alias, target, created_by, created_at)
SELECT alias, target, created_by, created_at FROM aliases;

-- Drop old table
DROP TABLE IF EXISTS aliases;

-- Rename new table
ALTER TABLE aliases_new RENAME TO aliases;

-- Recreate indexes (now with proper collation from columns)
CREATE INDEX IF NOT EXISTS idx_aliases_alias ON aliases(alias);
CREATE INDEX IF NOT EXISTS idx_aliases_target ON aliases(target);
