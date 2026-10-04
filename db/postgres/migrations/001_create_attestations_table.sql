-- Attestations for the Postgres backend. The multi-value fields are TEXT[],
-- and attributes the JSON text as written, so a signature over it still holds.
CREATE TABLE IF NOT EXISTS attestations (
    id TEXT PRIMARY KEY,
    subjects TEXT[] NOT NULL,
    predicates TEXT[] NOT NULL,
    contexts TEXT[] NOT NULL,
    actors TEXT[] NOT NULL,
    timestamp BIGINT NOT NULL,
    source TEXT NOT NULL,
    attributes TEXT,
    created_at BIGINT NOT NULL,
    signature BYTEA,
    signer_did TEXT
);

CREATE INDEX IF NOT EXISTS idx_attestations_subjects ON attestations USING GIN (subjects);
CREATE INDEX IF NOT EXISTS idx_attestations_predicates ON attestations USING GIN (predicates);
CREATE INDEX IF NOT EXISTS idx_attestations_contexts ON attestations USING GIN (contexts);
CREATE INDEX IF NOT EXISTS idx_attestations_actors ON attestations USING GIN (actors);
CREATE INDEX IF NOT EXISTS idx_attestations_timestamp ON attestations (timestamp);
