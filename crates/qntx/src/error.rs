//! Common error types for QNTX Rust components.
//!
//! Provides consistent error handling with context wrapping across all QNTX Rust crates.
//! Error messages accumulate context as they propagate up the call stack, similar to
//! cockroachdb/errors used in Go components.
//!
//! # Examples
//!
//! ```ignore
//! use qntx::error::{Error, Result};
//!
//! fn process_data(path: &str) -> Result<String> {
//!     let data = std::fs::read_to_string(path)
//!         .map_err(|e| Error::Io(e).wrap("failed to read file"))?;
//!     Ok(data)
//! }
//! ```

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

    /// Internal error with optional context chain
    #[error("{message}")]
    Internal { message: String },
}

impl Error {
    /// Create a new internal error with context.
    pub fn internal(msg: impl Into<String>) -> Self {
        Error::Internal {
            message: msg.into(),
        }
    }

    /// Wrap this error with additional context.
    ///
    /// This creates a new error message that chains the context with the original error,
    /// similar to cockroachdb/errors.Wrap() in Go. The context appears first,
    /// providing more specific information about what failed.
    ///
    /// # Examples
    ///
    /// ```ignore
    /// result.map_err(|e| e.wrap("failed to initialize engine"))
    /// ```
    pub fn wrap(self, context: impl Into<String>) -> Self {
        let msg = format!("{}: {}", context.into(), self);
        Error::Internal { message: msg }
    }

    /// Wrap any error type with context, automatically converting to Error::Internal.
    ///
    /// Convenience helper for wrapping foreign error types.
    pub fn context(context: impl Into<String>, err: impl std::fmt::Display) -> Self {
        Error::Internal {
            message: format!("{}: {}", context.into(), err),
        }
    }
}

/// Result type alias using QNTX Error.
pub type Result<T> = std::result::Result<T, Error>;

/// Extension trait for wrapping errors in Result.
pub trait ErrorContext<T> {
    /// Wrap an error with context message.
    fn wrap_err(self, context: impl Into<String>) -> Result<T>;
}

impl<T, E> ErrorContext<T> for std::result::Result<T, E>
where
    E: Into<Error>,
{
    fn wrap_err(self, context: impl Into<String>) -> Result<T> {
        self.map_err(|e| e.into().wrap(context))
    }
}
