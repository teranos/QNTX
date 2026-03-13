-- Add optional cluster scope to semantic watchers.
-- NULL means search all clusters (backwards-compatible).
ALTER TABLE watchers ADD COLUMN semantic_cluster_id INTEGER;
