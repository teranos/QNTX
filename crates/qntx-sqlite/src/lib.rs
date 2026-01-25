//! SQLite storage backend for QNTX attestations
//!
//! This crate provides a persistent SQLite implementation of the qntx-core storage traits,
//! enabling native platforms (server, Tauri desktop) to store attestations on disk.
//!
//! # Features
//!
//! - Implements `AttestationStore` trait for basic CRUD operations
//! - Uses the same SQLite schema as the Go implementation for compatibility
//! - Supports in-memory databases for testing
//! - Thread-safe with proper connection handling
//!
//! # Example
//!
//! ```rust,no_run
//! use qntx_sqlite::SqliteStore;
//! use qntx_core::{AttestationBuilder, storage::AttestationStore};
//!
//! # fn main() -> Result<(), Box<dyn std::error::Error>> {
//! // Create an in-memory store
//! let mut store = SqliteStore::in_memory()?;
//!
//! // Create an attestation
//! let attestation = AttestationBuilder::new()
//!     .id("AS-test-1")
//!     .subject("ALICE")
//!     .predicate("knows")
//!     .context("work")
//!     .build();
//!
//! // Store it
//! store.put(attestation)?;
//!
//! // Retrieve it
//! let retrieved = store.get("AS-test-1")?;
//! assert!(retrieved.is_some());
//! # Ok(())
//! # }
//! ```

pub mod error;
pub mod json;
pub mod migrate;
pub mod store;

// Re-export main types
pub use error::{Result, SqliteError};
pub use store::SqliteStore;
