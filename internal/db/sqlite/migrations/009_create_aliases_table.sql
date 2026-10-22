-- Migration: Create aliases table for bidirectional identifier mappings
--
-- Enables ATS (Attestation Query Language) alias resolution where:
-- - Aliases are bidirectional (A->B implies B->A)
-- - Multiple identifiers can resolve to the same entity
-- - Used for name variations, ID mappings, and entity consolidation
--
-- Business Context:
-- - Users need to query entities by multiple identifiers (nicknames, IDs, etc.)
-- - Alias resolution happens transparently during ATS queries
-- - Critical for contact deduplication and entity identification
--
-- Technical Details:
-- - Composite primary key ensures unique (alias, target) pairs
-- - Both directions are stored explicitly for fast lookups
-- - Indexes on both alias and target for bidirectional queries
-- - Created_by tracks who/what created the mapping (user, system, ingester)

CREATE TABLE IF NOT EXISTS aliases (
    alias TEXT NOT NULL,
    target TEXT NOT NULL,
    created_by TEXT NOT NULL DEFAULT 'system',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (alias, target)
);

-- Index for fast lookups by alias (most common: resolve alias to targets)
CREATE INDEX IF NOT EXISTS idx_aliases_alias ON aliases(alias);

-- Index for fast reverse lookups (find all aliases pointing to a target)
CREATE INDEX IF NOT EXISTS idx_aliases_target ON aliases(target);
