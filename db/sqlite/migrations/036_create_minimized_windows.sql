CREATE TABLE IF NOT EXISTS minimized_windows (
    glyph_id TEXT PRIMARY KEY,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now'))
);
