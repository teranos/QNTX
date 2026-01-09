//! # QNTX Shared Rust Library
//!
//! This crate provides shared infrastructure for all QNTX Rust components:
//! - **types**: Generated type definitions from Go source (run `make types` to regenerate)
//! - **plugin**: gRPC plugin infrastructure for building QNTX plugins
//! - **error**: Common error types with context
//! - **tracing**: Logging utilities with QNTX segment prefixes
//!
//! ## Usage
//!
//! ```rust,ignore
//! use qntx::types::{Job, JobStatus, sym};
//! use qntx::plugin::PluginServer;
//! use qntx::error::Error;
//! ```

#[cfg(feature = "types")]
pub mod types;

#[cfg(feature = "plugin")]
pub mod plugin;

pub mod error;
pub mod tracing;

// Re-export commonly used items at crate root
#[cfg(feature = "types")]
pub use types::{sym, Job, JobStatus, Progress};

pub use error::{Error, Result};
