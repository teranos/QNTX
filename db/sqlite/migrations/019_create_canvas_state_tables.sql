-- Canvas state persistence: glyphs and compositions
-- Stores the spatial arrangement of glyphs on the canvas workspace

CREATE TABLE IF NOT EXISTS canvas_glyphs (
    id TEXT PRIMARY KEY,
    symbol TEXT NOT NULL,
    x INTEGER NOT NULL,
    y INTEGER NOT NULL,
    width INTEGER,
    height INTEGER,
    -- Optional execution result for result glyphs (stored as JSON)
    result_data TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS canvas_compositions (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL CHECK(type IN ('ax-prompt', 'ax-py', 'py-prompt')),
    initiator_id TEXT NOT NULL,
    target_id TEXT NOT NULL,
    x INTEGER NOT NULL,
    y INTEGER NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    -- Ensure a glyph can only be in one composition
    UNIQUE(initiator_id),
    UNIQUE(target_id)
);

-- Index for looking up compositions by glyph ID
CREATE INDEX IF NOT EXISTS idx_compositions_initiator ON canvas_compositions(initiator_id);
CREATE INDEX IF NOT EXISTS idx_compositions_target ON canvas_compositions(target_id);
