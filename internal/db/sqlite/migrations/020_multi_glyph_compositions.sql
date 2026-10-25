-- Multi-glyph composition support
-- Replaces binary (initiator_id/target_id) with N-glyph junction table
-- SQLite doesn't support DROP COLUMN, so we recreate the table

-- Recreate canvas_compositions without initiator_id/target_id
-- Note: This is a breaking change - existing compositions will be lost
DROP TABLE IF EXISTS canvas_compositions;

CREATE TABLE canvas_compositions (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    x INTEGER NOT NULL,
    y INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- Create junction table for composition glyphs (after new canvas_compositions exists)
CREATE TABLE composition_glyphs (
    composition_id TEXT NOT NULL,
    glyph_id TEXT NOT NULL,
    position INTEGER NOT NULL,
    PRIMARY KEY (composition_id, glyph_id),
    FOREIGN KEY (composition_id) REFERENCES canvas_compositions(id) ON DELETE CASCADE,
    FOREIGN KEY (glyph_id) REFERENCES canvas_glyphs(id) ON DELETE CASCADE
);

-- Create indexes for efficient queries
CREATE INDEX idx_composition_glyphs_composition_id ON composition_glyphs(composition_id);
CREATE INDEX idx_composition_glyphs_position ON composition_glyphs(composition_id, position);
