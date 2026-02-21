CREATE TABLE IF NOT EXISTS node_identity (
    id TEXT PRIMARY KEY DEFAULT 'self',
    private_key BLOB NOT NULL,
    public_key BLOB NOT NULL,
    did TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now'))
);
