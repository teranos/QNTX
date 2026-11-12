-- "When a PR comment lands on QNTX, QNTX-Web, QNTX-App"
-- "It get’s said into the Room"
CREATE TABLE github_room_comments (
    key TEXT PRIMARY KEY,
    payload BLOB,
    provider TEXT NOT NULL DEFAULT '',
    room TEXT NOT NULL DEFAULT '',
    message_id TEXT NOT NULL DEFAULT '',
    received_at TEXT NOT NULL,
    delivered_at TEXT,
    last_error TEXT NOT NULL DEFAULT ''
);
CREATE INDEX github_room_comments_pending ON github_room_comments(received_at)
    WHERE delivered_at IS NULL;
