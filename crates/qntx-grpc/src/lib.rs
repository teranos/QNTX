#![cfg_attr(
    test,
    allow(
        clippy::unwrap_used,
        clippy::expect_used,
        clippy::panic,
        clippy::indexing_slicing,
        clippy::string_slice
    )
)]
//! # QNTX Shared Rust Library
//!
//! This crate provides shared infrastructure for all QNTX Rust components:
//! - **plugin**: gRPC plugin infrastructure for building QNTX plugins
//! - **error**: Common error types with context
//! - **tracing**: Logging utilities with QNTX segment prefixes
//!
//! ## Usage
//!
//! ```rust,ignore
//! use qntx::plugin::PluginServer;
//! use qntx::error::Error;
//! ```

#[cfg(feature = "plugin")]
pub mod plugin;

pub mod error;

// Re-export commonly used items at crate root

pub use error::{Error, Result};
