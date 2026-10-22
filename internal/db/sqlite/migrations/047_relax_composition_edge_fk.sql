-- Relax composition_edges foreign keys on glyph IDs.
-- The canvas sync queue is eventually-consistent: compositions may sync
-- before their member glyphs, causing FK violations (HTTP 500).
-- The composition_id FK on canvas_compositions is kept.

-- SQLite cannot ALTER foreign keys; recreate the table without glyph FKs.

CREATE TABLE composition_edges_new (
    composition_id TEXT NOT NULL,
    from_glyph_id TEXT NOT NULL,
    to_glyph_id TEXT NOT NULL,
    direction TEXT NOT NULL CHECK(direction IN ('right', 'top', 'bottom')),
    position INTEGER DEFAULT 0,
    PRIMARY KEY (composition_id, from_glyph_id, to_glyph_id, direction),
    FOREIGN KEY (composition_id) REFERENCES canvas_compositions(id) ON DELETE CASCADE
);

INSERT INTO composition_edges_new
    SELECT composition_id, from_glyph_id, to_glyph_id, direction, position
    FROM composition_edges;

DROP TABLE composition_edges;
ALTER TABLE composition_edges_new RENAME TO composition_edges;

CREATE INDEX idx_composition_edges_composition_id ON composition_edges(composition_id);
