-- "and for every other namespace the canvas needs to be explicitly created and named."
--
-- A file is one namespace's, so it holds at most one canvas.
CREATE TABLE IF NOT EXISTS canvases (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    name TEXT NOT NULL,
    created_at TEXT NOT NULL
);
