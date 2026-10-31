-- A read since a time walked created_at newest first and visited every row
-- to learn its timestamp: on k, all 543573 for a window nothing had come in.
-- Here the window is found, ordered and named without visiting a row; only
-- the rows answered with are read.
CREATE INDEX IF NOT EXISTS idx_attestations_window ON attestations(timestamp, created_at, id);
