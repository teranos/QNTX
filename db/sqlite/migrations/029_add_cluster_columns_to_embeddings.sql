ALTER TABLE embeddings ADD COLUMN cluster_id INTEGER DEFAULT -1;
ALTER TABLE embeddings ADD COLUMN cluster_probability REAL DEFAULT 0.0;
CREATE INDEX IF NOT EXISTS idx_embeddings_cluster_id ON embeddings(cluster_id);
