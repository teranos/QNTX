-- ROOT being a User that is not ROOT (ADR-031), held by the session ROOT
-- became them in. Kept here so a restart neither ends one nor lets them be
-- themselves while ROOT is them. Removed before 1.0.0.
--
-- own_tokens is the ids of the tokens they held when ROOT became them, as a
-- JSON array: those are refused until ROOT unbecomes them.
CREATE TABLE auth_becomings (
    session TEXT PRIMARY KEY CHECK (session <> ''),
    user_id TEXT NOT NULL UNIQUE,
    by_user TEXT NOT NULL,
    became_at INTEGER NOT NULL,
    own_tokens TEXT NOT NULL
);
