-- Allow the same source to have embeddings from different models (ADR-019 Phase 2).
-- Relates to 024_optional_create_embeddings_table.sql which created the table
-- and idx_embeddings_source (non-unique, kept for queries that don't filter by model).
CREATE UNIQUE INDEX IF NOT EXISTS idx_embeddings_source_model
    ON embeddings(source_type, source_id, model);
