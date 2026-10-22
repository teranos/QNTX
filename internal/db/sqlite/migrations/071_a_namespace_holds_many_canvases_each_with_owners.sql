-- "given a namespace there can be multiple canvasses"
-- "A canvas can have multiple owners"
-- "when anyone deletes a canvas, its not actually deleted, its disabled."
--
-- The one canvas a file held becomes the namespace's canvas, and the rows on
-- it say so. ROOT and the SUPERs of a namespace own every canvas in it
-- implicitly, so no row here names them.

CREATE TABLE canvases_new (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('namespace', 'user')),
    created_by TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    disabled_by TEXT NOT NULL DEFAULT ''
);

INSERT INTO canvases_new (id, name, kind, created_by, created_at, disabled_by)
    SELECT 'CV-' || upper(hex(randomblob(6))), name, 'namespace', '', created_at, ''
    FROM canvases;

DROP TABLE canvases;
ALTER TABLE canvases_new RENAME TO canvases;

CREATE TABLE canvas_owners (
    canvas_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    PRIMARY KEY (canvas_id, user_id)
);

CREATE TABLE canvas_access (
    canvas_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    PRIMARY KEY (canvas_id, user_id)
);

CREATE TABLE canvas_invitations (
    token TEXT PRIMARY KEY,
    canvas_id TEXT NOT NULL,
    inviter TEXT NOT NULL,
    invitee TEXT NOT NULL,
    email TEXT NOT NULL,
    created_at TEXT NOT NULL,
    accepted_at TEXT
);

ALTER TABLE canvas_elements ADD COLUMN in_canvas TEXT NOT NULL DEFAULT '';
ALTER TABLE canvas_compositions ADD COLUMN in_canvas TEXT NOT NULL DEFAULT '';
ALTER TABLE minimized_windows ADD COLUMN in_canvas TEXT NOT NULL DEFAULT '';

UPDATE canvas_elements SET in_canvas = (SELECT id FROM canvases WHERE kind = 'namespace' LIMIT 1)
    WHERE EXISTS (SELECT 1 FROM canvases WHERE kind = 'namespace');
UPDATE canvas_compositions SET in_canvas = (SELECT id FROM canvases WHERE kind = 'namespace' LIMIT 1)
    WHERE EXISTS (SELECT 1 FROM canvases WHERE kind = 'namespace');
UPDATE minimized_windows SET in_canvas = (SELECT id FROM canvases WHERE kind = 'namespace' LIMIT 1)
    WHERE EXISTS (SELECT 1 FROM canvases WHERE kind = 'namespace');

CREATE INDEX idx_canvas_elements_in_canvas ON canvas_elements(in_canvas);
CREATE INDEX idx_canvas_compositions_in_canvas ON canvas_compositions(in_canvas);
CREATE INDEX idx_minimized_windows_in_canvas ON minimized_windows(in_canvas);
