//! Common error types for QNTX Rust components.

use thiserror::Error;

/// Common error type for QNTX operations.
#[derive(Error, Debug)]
pub enum Error {
    /// IO error
    #[error("io error: {0}")]
    Io(#[from] std::io::Error),

    /// Serialization error
    #[error("serialization error: {0}")]
    Serialization(#[from] serde_json::Error),

    /// gRPC transport error
    #[cfg(feature = "plugin")]
    #[error("grpc transport error: {0}")]
    Transport(#[from] tonic::transport::Error),

    /// gRPC status error
    #[cfg(feature = "plugin")]
    #[error("grpc error: {0}")]
    Grpc(#[from] tonic::Status),

    /// Configuration error
    #[error("configuration error: {0}")]
    Config(String),

    /// Plugin error
    #[error("plugin error: {0}")]
    Plugin(String),

    /// Internal error
    #[error("{0}")]
    Internal(String),
}

/// Result type alias using QNTX Error.
pub type Result<T> = std::result::Result<T, Error>;
