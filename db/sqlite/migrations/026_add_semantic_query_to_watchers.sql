-- Add semantic search fields to watchers for ⊨ (semantic search) glyphs.
-- SemanticQuery holds the natural language query; SemanticThreshold is the
-- minimum cosine similarity (0-1) required for a match to fire.
ALTER TABLE watchers ADD COLUMN semantic_query TEXT;
ALTER TABLE watchers ADD COLUMN semantic_threshold REAL;
