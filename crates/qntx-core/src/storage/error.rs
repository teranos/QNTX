//! Storage error types

use std::fmt;

/// Errors that can occur during storage operations
#[derive(Debug, Clone)]
pub enum StoreError {
    /// Attestation with this ID already exists
    AlreadyExists(String),

    /// Attestation not found
    NotFound(String),

    /// Invalid attestation data
    InvalidData(String),

    /// Storage backend error (database, filesystem, etc.)
    Backend(String),

    /// Query error
    Query(String),

    /// Serialization/deserialization error
    Serialization(String),

    /// Storage quota exceeded
    QuotaExceeded {
        actor: String,
        context: String,
        current: usize,
        limit: usize,
    },
}

impl fmt::Display for StoreError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            StoreError::AlreadyExists(id) => write!(f, "attestation already exists: {}", id),
            StoreError::NotFound(id) => write!(f, "attestation not found: {}", id),
            StoreError::InvalidData(msg) => write!(f, "invalid attestation data: {}", msg),
            StoreError::Backend(msg) => write!(f, "storage backend error: {}", msg),
            StoreError::Query(msg) => write!(f, "query error: {}", msg),
            StoreError::Serialization(msg) => write!(f, "serialization error: {}", msg),
            StoreError::QuotaExceeded {
                actor,
                context,
                current,
                limit,
            } => write!(
                f,
                "quota exceeded for actor '{}' in context '{}': {} >= {}",
                actor, context, current, limit
            ),
        }
    }
}

impl std::error::Error for StoreError {}

/// Result type for storage operations
pub type StoreResult<T> = Result<T, StoreError>;
