-- Attestations schema for the DuckDB/Parquet backend (ADR-024).
-- Multi-value fields use DuckDB's native VARCHAR[] (Parquet LIST<VARCHAR> on flush).
-- Attributes are stored as JSON strings; a follow-up may switch to MAP or JSON type.
CREATE TABLE IF NOT EXISTS attestations (
    id VARCHAR PRIMARY KEY,
    subjects VARCHAR[] NOT NULL,
    predicates VARCHAR[] NOT NULL,
    contexts VARCHAR[] NOT NULL,
    actors VARCHAR[] NOT NULL,
    timestamp BIGINT NOT NULL,
    source VARCHAR NOT NULL,
    attributes VARCHAR,
    created_at BIGINT NOT NULL,
    signature BLOB,
    signer_did VARCHAR
);
