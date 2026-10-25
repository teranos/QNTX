-- "As ROOT i send an invite link to a friend, i enter their e-mail address"
--
-- "and i want to set Name"
--
-- "if both google and apple, then we set both, and  if set then we set the mail address of that provider"
--
-- "or the username"
--
-- One row per invitation, keyed by the hash of the link's token, never the
-- token: a copy of the db is not a way in. The id is what ROOT's cancel names.
CREATE TABLE invitations (
    id TEXT PRIMARY KEY CHECK (id <> ''),
    token_hash TEXT NOT NULL UNIQUE CHECK (token_hash <> ''),
    email TEXT NOT NULL CHECK (email <> ''),
    display_name TEXT NOT NULL DEFAULT '',
    invited_by TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    cancelled_at INTEGER,
    accepted_by TEXT,
    accepted_at INTEGER
);

-- Every provider the friend may sign in with, and the account there.
CREATE TABLE invitation_accounts (
    invitation_id TEXT NOT NULL REFERENCES invitations(id),
    provider TEXT NOT NULL CHECK (provider <> ''),
    account TEXT NOT NULL CHECK (account <> ''),
    PRIMARY KEY (invitation_id, provider)
);
