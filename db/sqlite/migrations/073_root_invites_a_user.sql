-- "As ROOT i send an invite link to a friend, i enter their e-mail address"
--
-- "so, if ROOT selected Mastodon, the invited user only sees the mastodon link,
-- and only the mastodon acc specified by ROOT would be applicable"
--
-- One row per invitation. The link the friend is mailed carries a token, and
-- the row is keyed by its hash, never the token: a copy of the db is not a way
-- in. The id is what ROOT's cancel names.
CREATE TABLE invitations (
    id TEXT PRIMARY KEY CHECK (id <> ''),
    token_hash TEXT NOT NULL UNIQUE CHECK (token_hash <> ''),
    email TEXT NOT NULL CHECK (email <> ''),
    provider TEXT NOT NULL CHECK (provider <> ''),
    account TEXT NOT NULL CHECK (account <> ''),
    invited_by TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    cancelled_at INTEGER,
    accepted_by TEXT,
    accepted_at INTEGER
);
