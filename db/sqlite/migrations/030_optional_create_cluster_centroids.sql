CREATE TABLE IF NOT EXISTS cluster_centroids (
    cluster_id INTEGER PRIMARY KEY,
    centroid BLOB NOT NULL,
    n_members INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL
);
