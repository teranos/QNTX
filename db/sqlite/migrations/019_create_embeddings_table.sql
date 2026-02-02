-- Create embeddings table for semantic search using sqlite-vec
CREATE TABLE IF NOT EXISTS embeddings (
    id TEXT PRIMARY KEY,

    -- Source reference
    source_type TEXT NOT NULL,      -- 'attestation', 'task_log', etc.
    source_id TEXT NOT NULL,        -- ID of the source record

    -- Content
    text TEXT NOT NULL,              -- Original text that was embedded
    embedding FLOAT32_BLOB(384) NOT NULL,  -- sqlite-vec vector type for 384-dimensional embeddings

    -- Metadata
    model TEXT NOT NULL,             -- Model name used for embedding (e.g., 'all-MiniLM-L6-v2')
    dimensions INTEGER NOT NULL,     -- Number of dimensions in the embedding

    -- Timestamps
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- Create virtual table for vector similarity search using sqlite-vec
CREATE VIRTUAL TABLE IF NOT EXISTS vec_embeddings USING vec0(
    embedding_id TEXT PRIMARY KEY,
    embedding FLOAT32[384]  -- 384 dimensions for all-MiniLM-L6-v2
);

-- Indexes for efficient queries
CREATE INDEX IF NOT EXISTS idx_embeddings_source ON embeddings(source_type, source_id);
CREATE INDEX IF NOT EXISTS idx_embeddings_created_at ON embeddings(created_at);