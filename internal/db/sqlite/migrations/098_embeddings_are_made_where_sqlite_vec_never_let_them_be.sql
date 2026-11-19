-- 「[Opus 5.5] 024 made embeddings beside a sqlite-vec table, so where vec0 was not loaded it failed, and 029,」
-- 「[Opus 5.5] 030, 032, 034, 035, 036 and 051 failed after it: l has no embeddings table. sqlite-vec is gone,」
-- 「[Opus 5.5] the vectors are vector search's (ADR-016), so from here every new database is such a node.」
-- 「[Opus 5.5] What those migrations leave, made only where it is missing; where they ran this changes nothing.」
CREATE TABLE IF NOT EXISTS embeddings (
    id TEXT PRIMARY KEY,
    source_type TEXT NOT NULL,
    source_id TEXT NOT NULL,
    text TEXT NOT NULL,
    embedding FLOAT32_BLOB(384) NOT NULL,
    model TEXT NOT NULL,
    dimensions INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    cluster_id INTEGER DEFAULT -1,
    cluster_probability REAL DEFAULT 0.0,
    projection_x REAL,
    projection_y REAL
);

CREATE INDEX IF NOT EXISTS idx_embeddings_source ON embeddings(source_type, source_id);
CREATE INDEX IF NOT EXISTS idx_embeddings_created_at ON embeddings(created_at);
CREATE INDEX IF NOT EXISTS idx_embeddings_cluster_id ON embeddings(cluster_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_embeddings_source_model
    ON embeddings(source_type, source_id, model);

CREATE TABLE IF NOT EXISTS cluster_centroids (
    cluster_id INTEGER PRIMARY KEY,
    centroid BLOB NOT NULL,
    n_members INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL
);

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

CREATE TABLE IF NOT EXISTS cluster_runs (
    id TEXT PRIMARY KEY,
    n_points INTEGER NOT NULL,
    n_clusters INTEGER NOT NULL,
    n_noise INTEGER NOT NULL,
    min_cluster_size INTEGER NOT NULL,
    duration_ms INTEGER NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS clusters (
    id INTEGER PRIMARY KEY,
    label TEXT,
    first_seen_run_id TEXT NOT NULL,
    last_seen_run_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TEXT NOT NULL,
    labeled_at TEXT,
    FOREIGN KEY (first_seen_run_id) REFERENCES cluster_runs(id),
    FOREIGN KEY (last_seen_run_id) REFERENCES cluster_runs(id)
);

CREATE TABLE IF NOT EXISTS cluster_snapshots (
    cluster_id INTEGER NOT NULL,
    run_id TEXT NOT NULL,
    centroid BLOB NOT NULL,
    n_members INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (cluster_id, run_id),
    FOREIGN KEY (cluster_id) REFERENCES clusters(id),
    FOREIGN KEY (run_id) REFERENCES cluster_runs(id)
);

CREATE TABLE IF NOT EXISTS cluster_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    cluster_id INTEGER NOT NULL,
    similarity REAL,
    created_at TEXT NOT NULL,
    FOREIGN KEY (run_id) REFERENCES cluster_runs(id),
    FOREIGN KEY (cluster_id) REFERENCES clusters(id)
);
