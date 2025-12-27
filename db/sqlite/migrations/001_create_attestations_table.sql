CREATE TABLE IF NOT EXISTS attestations (
    id TEXT PRIMARY KEY,
    subjects JSON NOT NULL,
    predicates JSON NOT NULL,
    contexts JSON NOT NULL,
    actors JSON NOT NULL,
    timestamp DATETIME NOT NULL,
    source TEXT NOT NULL DEFAULT 'cli',
    attributes JSON,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_attestations_subjects ON attestations(json_extract(subjects, '$'));
CREATE INDEX IF NOT EXISTS idx_attestations_predicates ON attestations(json_extract(predicates, '$'));
CREATE INDEX IF NOT EXISTS idx_attestations_contexts ON attestations(json_extract(contexts, '$'));
CREATE INDEX IF NOT EXISTS idx_attestations_timestamp ON attestations(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_attestations_actors ON attestations(json_extract(actors, '$'));
CREATE INDEX IF NOT EXISTS idx_attestations_actors_timestamp ON attestations(json_extract(actors, '$'), timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_attestations_actors_context_timestamp ON attestations(json_extract(actors, '$'), json_extract(contexts, '$'), timestamp DESC);
