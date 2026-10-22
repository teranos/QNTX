-- Track last processed attestation per meld edge to prevent reprocessing on restart
CREATE TABLE IF NOT EXISTS composition_edge_cursors (
    composition_id TEXT NOT NULL,
    from_glyph_id TEXT NOT NULL,
    to_glyph_id TEXT NOT NULL,
    last_processed_id TEXT NOT NULL,
    last_processed_at DATETIME NOT NULL,
    PRIMARY KEY (composition_id, from_glyph_id, to_glyph_id)
);
