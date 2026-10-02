-- A session lives in the operational db. Before this it lived in the process,
-- so every deploy signed everybody out.
--
-- Keyed by the hash of the token, never the token: a copy of the db is not a
-- way in.
CREATE TABLE auth_sessions (
    hash TEXT PRIMARY KEY CHECK (hash <> ''),
    identity TEXT NOT NULL,
    user_id TEXT NOT NULL,
    display_name TEXT NOT NULL,
    namespace TEXT NOT NULL,
    expires_at INTEGER NOT NULL
);

CREATE INDEX idx_auth_sessions_expires_at ON auth_sessions(expires_at);
