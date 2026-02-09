-- Edge-based composition DAG
-- Migrates from composition_glyphs junction table to composition_edges
-- Removes type field (edges ARE the type information)
-- See ADR-009 for rationale

-- Drop composition_glyphs (breaking change - no migration path)
DROP TABLE IF EXISTS composition_glyphs;

-- Recreate canvas_compositions without type field
-- SQLite doesn't support DROP COLUMN, so we recreate
DROP TABLE IF EXISTS canvas_compositions;

CREATE TABLE canvas_compositions (
    id TEXT PRIMARY KEY,
    x INTEGER NOT NULL,
    y INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- Create composition_edges table for DAG structure
CREATE TABLE composition_edges (
    composition_id TEXT NOT NULL,
    from_glyph_id TEXT NOT NULL,
    to_glyph_id TEXT NOT NULL,
    direction TEXT NOT NULL CHECK(direction IN ('right', 'top', 'bottom')),
    position INTEGER DEFAULT 0,
    PRIMARY KEY (composition_id, from_glyph_id, to_glyph_id, direction),
    FOREIGN KEY (composition_id) REFERENCES canvas_compositions(id) ON DELETE CASCADE,
    FOREIGN KEY (from_glyph_id) REFERENCES canvas_glyphs(id) ON DELETE CASCADE,
    FOREIGN KEY (to_glyph_id) REFERENCES canvas_glyphs(id) ON DELETE CASCADE
);

-- Create index for efficient queries
CREATE INDEX idx_composition_edges_composition_id ON composition_edges(composition_id);
