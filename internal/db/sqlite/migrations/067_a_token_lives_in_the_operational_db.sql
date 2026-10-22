-- An access token lives in the operational db (ADR-037). Before this the gate
-- read a map the open had filled from S3, one LIST and one GET per token, and
-- recording a use rewrote the token's object — one PUT per authenticated
-- request, which measured as three quarters of everything the node asked its
-- location for.
--
-- Keyed by hash rather than by id: a request carries a bearer, and a bearer
-- reduces to a hash. The id is what the endpoints name, so it is indexed.
--
-- The row is the record as the node writes it, so this table and the object on
-- S3 hold the same bytes — except last_used_at, which is a watch rather than a
-- record and is never sent. S3 is the record for host loss, not for reading.
CREATE TABLE access_tokens (
    hash TEXT PRIMARY KEY CHECK (hash <> ''),
    id TEXT NOT NULL CHECK (id <> ''),
    record TEXT NOT NULL,
    created_at INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_access_tokens_id ON access_tokens(id);
