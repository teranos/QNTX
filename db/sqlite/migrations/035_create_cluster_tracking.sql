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

-- Bootstrap: if cluster_centroids has rows from a previous run, create
-- a synthetic run and seed clusters + snapshots so existing IDs are preserved.
INSERT INTO cluster_runs (id, n_points, n_clusters, n_noise, min_cluster_size, duration_ms, created_at)
SELECT 'CR_bootstrap',
       (SELECT COUNT(*) FROM embeddings),
       COUNT(*),
       (SELECT COUNT(*) FROM embeddings WHERE cluster_id < 0),
       5,
       0,
       strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
FROM cluster_centroids
WHERE EXISTS (SELECT 1 FROM cluster_centroids LIMIT 1);

INSERT INTO clusters (id, label, first_seen_run_id, last_seen_run_id, status, created_at)
SELECT cluster_id, NULL, 'CR_bootstrap', 'CR_bootstrap', 'active', strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
FROM cluster_centroids;

INSERT INTO cluster_snapshots (cluster_id, run_id, centroid, n_members, created_at)
SELECT cluster_id, 'CR_bootstrap', centroid, n_members, strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
FROM cluster_centroids;
