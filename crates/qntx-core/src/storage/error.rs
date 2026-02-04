//! Storage error types

use thiserror::Error;

/// Errors that can occur during storage operations
#[derive(Debug, Clone, Error)]
pub enum StoreError {
    /// Attestation with this ID already exists
    #[error("attestation already exists: {0}")]
    AlreadyExists(String),

    /// Attestation not found
    #[error("attestation not found: {0}")]
    NotFound(String),

    /// Invalid attestation data
    #[error("invalid attestation data: {0}")]
    InvalidData(String),

    /// Storage backend error (database, filesystem, etc.)
    #[error("storage backend error: {0}")]
    Backend(String),

    /// Query error
    #[error("query error: {0}")]
    Query(String),

    /// Serialization/deserialization error
    #[error("serialization error: {0}")]
    Serialization(String),

    /// Storage quota exceeded
    #[error("quota exceeded for actor '{actor}' in context '{context}': {current} >= {limit}")]
    QuotaExceeded {
        actor: String,
        context: String,
        current: usize,
        limit: usize,
    },
}

/// Result type for storage operations
pub type StoreResult<T> = Result<T, StoreError>;
