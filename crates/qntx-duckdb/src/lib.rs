//! DuckDB-backed Parquet attestation store.
//!
//! Skeleton — populated in follow-up commits driven by the ADR-024 performance floor.
//! Peer of `qntx_sqlite::SqliteStore`; implements the same storage traits from `qntx-core`.

pub mod error;

pub use error::{DuckdbError, Result};

/// Attestation store backed by Parquet files at a location URL.
///
/// Location can be `s3://bucket/prefix` (production) or `file:///path` (development).
/// DuckDB reads and writes Parquet directly at that location via the `httpfs` extension.
pub struct DuckdbStore {
    location: String,
    conn: duckdb::Connection,
}

impl DuckdbStore {
    /// Open a store at the given location URL.
    /// The DuckDB connection is in-memory; Parquet files at `location` are the durable store.
    pub fn open(location: impl Into<String>) -> Result<Self> {
        let conn = duckdb::Connection::open_in_memory()?;
        Ok(Self {
            location: location.into(),
            conn,
        })
    }

    /// The location URL this store reads and writes.
    pub fn location(&self) -> &str {
        &self.location
    }

    /// Access the underlying DuckDB connection (temporary — for skeleton phase).
    pub fn connection(&self) -> &duckdb::Connection {
        &self.conn
    }
}
