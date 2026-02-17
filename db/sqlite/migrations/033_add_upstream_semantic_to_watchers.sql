ALTER TABLE watchers ADD COLUMN upstream_semantic_query TEXT DEFAULT NULL;
ALTER TABLE watchers ADD COLUMN upstream_semantic_threshold REAL DEFAULT NULL;
