-- Add 'left' direction to composition_edges for nested canvas port glyphs
-- SQLite doesn't support ALTER CHECK, so recreate the table

-- Disable FK checks during table rebuild (standard SQLite pattern)
PRAGMA foreign_keys = OFF;

-- Delete orphaned edges before rebuild
DELETE FROM composition_edges WHERE composition_id NOT IN (SELECT id FROM canvas_compositions);
DELETE FROM composition_edges WHERE from_glyph_id NOT IN (SELECT id FROM canvas_glyphs);
DELETE FROM composition_edges WHERE to_glyph_id NOT IN (SELECT id FROM canvas_glyphs);

-- Save existing data
CREATE TABLE composition_edges_backup AS SELECT * FROM composition_edges;

-- Drop and recreate with updated constraint
DROP TABLE composition_edges;

CREATE TABLE composition_edges (
    composition_id TEXT NOT NULL,
    from_glyph_id TEXT NOT NULL,
    to_glyph_id TEXT NOT NULL,
    direction TEXT NOT NULL CHECK(direction IN ('right', 'left', 'top', 'bottom')),
    position INTEGER DEFAULT 0,
    PRIMARY KEY (composition_id, from_glyph_id, to_glyph_id, direction),
    FOREIGN KEY (composition_id) REFERENCES canvas_compositions(id) ON DELETE CASCADE,
    FOREIGN KEY (from_glyph_id) REFERENCES canvas_glyphs(id) ON DELETE CASCADE,
    FOREIGN KEY (to_glyph_id) REFERENCES canvas_glyphs(id) ON DELETE CASCADE
);

-- Restore clean data
INSERT INTO composition_edges SELECT * FROM composition_edges_backup;

-- Drop backup
DROP TABLE composition_edges_backup;

-- Recreate index
CREATE INDEX idx_composition_edges_composition_id ON composition_edges(composition_id);

-- Re-enable FK checks
PRAGMA foreign_keys = ON;
