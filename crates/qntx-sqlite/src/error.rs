//! Error types for SQLite storage backend

use qntx_core::storage::StoreError;
use thiserror::Error;

/// Result type for storage operations
pub type Result<T> = std::result::Result<T, SqliteError>;

/// Errors that can occur during SQLite storage operations
#[derive(Debug, Error)]
pub enum SqliteError {
    /// Database connection or query error
    #[error("SQLite error: {0}")]
    Database(#[from] rusqlite::Error),

    /// JSON serialization/deserialization error
    #[error("JSON error: {0}")]
    Json(#[from] serde_json::Error),

    /// Attestation with given ID already exists
    #[error("Attestation {0} already exists")]
    AlreadyExists(String),

    /// Attestation with given ID not found
    #[error("Attestation {0} not found")]
    NotFound(String),

    /// Migration error
    #[error("Migration error: {0}")]
    Migration(String),

    /// IO error (for file operations)
    #[error("IO error: {0}")]
    Io(#[from] std::io::Error),
}

/// Convert SqliteError to StoreError for the storage trait
impl From<SqliteError> for StoreError {
    fn from(err: SqliteError) -> Self {
        match err {
            SqliteError::AlreadyExists(id) => StoreError::AlreadyExists(id),
            SqliteError::NotFound(id) => StoreError::NotFound(id),
            SqliteError::Json(e) => StoreError::Serialization(e.to_string()),
            SqliteError::Database(e) => StoreError::Backend(format!("SQLite: {}", e)),
            SqliteError::Migration(msg) => StoreError::Backend(format!("Migration: {}", msg)),
            SqliteError::Io(e) => StoreError::Backend(format!("IO: {}", e)),
        }
    }
}
