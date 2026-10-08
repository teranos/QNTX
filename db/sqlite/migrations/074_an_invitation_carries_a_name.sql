-- "and i want to set Name"
--
-- "if both google and apple, then we set both, and  if set then we set the mail address of that provider"
--
-- "or the username"
--
-- An invitation names the friend, and every provider they may sign in with,
-- each with the account there that is theirs. 073 held one provider and one
-- account; it held only the invitations ROOT sent while trying the first shape.
DROP TABLE invitations;

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

CREATE TABLE invitation_accounts (
    invitation_id TEXT NOT NULL REFERENCES invitations(id),
    provider TEXT NOT NULL CHECK (provider <> ''),
    account TEXT NOT NULL CHECK (account <> ''),
    PRIMARY KEY (invitation_id, provider)
);
