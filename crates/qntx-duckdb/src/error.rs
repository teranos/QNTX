//! Error type for the DuckDB storage backend.

use thiserror::Error;

pub type Result<T> = std::result::Result<T, DuckdbError>;

#[derive(Debug, Error)]
pub enum DuckdbError {
    #[error("duckdb error: {0}")]
    Duckdb(#[from] duckdb::Error),

    #[error("io error: {0}")]
    Io(#[from] std::io::Error),

    #[error("serialization error: {0}")]
    Serde(#[from] serde_json::Error),

    #[error("backend error: {0}")]
    Backend(String),
}
