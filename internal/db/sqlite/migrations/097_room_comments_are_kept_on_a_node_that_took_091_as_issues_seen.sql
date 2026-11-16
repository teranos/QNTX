-- 「[Fable 5.2] k took 091 while 091 was issues_seen; bb4f7ef2a renumbered that to 094 and left the room」
-- 「[Fable 5.2] comments as the only 091. A node applies one migration per number, so on k the table below」
-- 「[Fable 5.2] was never made and k logged its absence at every start (#1747, 2026-11-14).」
-- 「[Fable 5.2] The same statements as 091, made only where they are missing.」
CREATE TABLE IF NOT EXISTS github_room_comments (
    key TEXT PRIMARY KEY,
    payload BLOB,
    provider TEXT NOT NULL DEFAULT '',
    room TEXT NOT NULL DEFAULT '',
    message_id TEXT NOT NULL DEFAULT '',
    received_at TEXT NOT NULL,
    delivered_at TEXT,
    last_error TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS github_room_comments_pending ON github_room_comments(received_at)
    WHERE delivered_at IS NULL;
