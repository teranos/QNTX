-- 「[Opus 5.5] An embedding's row is deleted before its vector is taken out of vector search's index;」
-- 「[Opus 5.5] when the provider does not take it out, the id would be lost with the row. Each is kept」
-- 「[Opus 5.5] here until the index has taken it out, and asked again until then.」
CREATE TABLE IF NOT EXISTS embedding_vectors_to_forget (
    embedding_id TEXT NOT NULL,
    model TEXT NOT NULL,
    PRIMARY KEY (embedding_id, model)
);
