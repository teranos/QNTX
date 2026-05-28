-- 052: canvases as a first-class table
--
-- A canvas is the surface where glyphs live. Every canvas has a row here.
-- canvas_glyphs.canvas_id and canvas_compositions.canvas_id are FKs to canvases(id).
-- Kills the implicit root canvas (canvas_id = '' magic).
--
-- Clean-slate behavior: works on empty dbs. On dbs with existing canvas_glyphs
-- rows where canvas_id = '', the canvas_glyphs recreate fails the CHECK
-- constraint and the whole migration aborts — surfacing the orphans.

PRAGMA foreign_keys = OFF;

-- 1. The canvases table
CREATE TABLE canvases (
    id TEXT PRIMARY KEY,
    name TEXT,
    anchor TEXT NOT NULL CHECK(anchor IN ('filesystem', 'floating', 'nested')),
    parent_canvas_id TEXT REFERENCES canvases(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_canvases_parent ON canvases(parent_canvas_id);

-- 2. Recreate canvas_glyphs so canvas_id is a real FK to canvases(id)
CREATE TABLE canvas_glyphs_new (
    id TEXT PRIMARY KEY,
    symbol TEXT NOT NULL,
    x INTEGER NOT NULL,
    y INTEGER NOT NULL,
    width INTEGER,
    height INTEGER,
    content TEXT,
    canvas_id TEXT NOT NULL CHECK(canvas_id != '') REFERENCES canvases(id) ON DELETE CASCADE,
    plugin_name TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

INSERT INTO canvas_glyphs_new (id, symbol, x, y, width, height, content, canvas_id, plugin_name, created_at, updated_at)
  SELECT id, symbol, x, y, width, height, content, canvas_id, plugin_name, created_at, updated_at
  FROM canvas_glyphs;

DROP TABLE canvas_glyphs;
ALTER TABLE canvas_glyphs_new RENAME TO canvas_glyphs;
CREATE INDEX idx_canvas_glyphs_canvas_id ON canvas_glyphs(canvas_id);

-- 3. Recreate canvas_compositions with canvas_id (NOT NULL, non-empty, FK)
CREATE TABLE canvas_compositions_new (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL CHECK(type IN ('ax-prompt', 'ax-py', 'py-prompt')),
    initiator_id TEXT NOT NULL,
    target_id TEXT NOT NULL,
    x INTEGER NOT NULL,
    y INTEGER NOT NULL,
    canvas_id TEXT NOT NULL CHECK(canvas_id != '') REFERENCES canvases(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(initiator_id),
    UNIQUE(target_id),
    FOREIGN KEY (initiator_id) REFERENCES canvas_glyphs(id) ON DELETE CASCADE,
    FOREIGN KEY (target_id) REFERENCES canvas_glyphs(id) ON DELETE CASCADE
);

-- Derive canvas_id from initiator glyph (compositions live on the same canvas as their initiator).
-- Step 2 already ensured every canvas_glyphs row has a valid canvas_id.
INSERT INTO canvas_compositions_new (id, type, initiator_id, target_id, x, y, canvas_id, created_at, updated_at)
  SELECT c.id, c.type, c.initiator_id, c.target_id, c.x, c.y,
         g.canvas_id,
         c.created_at, c.updated_at
  FROM canvas_compositions c
  JOIN canvas_glyphs g ON g.id = c.initiator_id;

DROP TABLE canvas_compositions;
ALTER TABLE canvas_compositions_new RENAME TO canvas_compositions;
CREATE INDEX idx_canvas_compositions_canvas_id ON canvas_compositions(canvas_id);
CREATE INDEX idx_canvas_compositions_initiator ON canvas_compositions(initiator_id);
CREATE INDEX idx_canvas_compositions_target ON canvas_compositions(target_id);

PRAGMA foreign_keys = ON;
