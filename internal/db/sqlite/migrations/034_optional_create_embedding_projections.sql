CREATE TABLE IF NOT EXISTS embedding_projections (
    embedding_id TEXT NOT NULL,
    method       TEXT NOT NULL,
    x            REAL NOT NULL,
    y            REAL NOT NULL,
    created_at   TEXT NOT NULL,
    PRIMARY KEY (embedding_id, method),
    FOREIGN KEY (embedding_id) REFERENCES embeddings(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_embedding_projections_method
    ON embedding_projections(method);
