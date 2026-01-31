//! SQLite storage backend for QNTX attestations
//!
//! This crate provides a persistent SQLite implementation of the qntx-core storage traits,
//! enabling native platforms (server, Tauri desktop) to store attestations on disk.
//!
//! # Features
//!
//! - Implements `AttestationStore` and `QueryStore` traits
//! - Uses the same SQLite schema as the Go implementation for compatibility
//! - Supports in-memory databases for testing
//! - Thread-safe with proper connection handling
//! - Optional quota enforcement via `BoundedStore`
//!
//! # Example: Basic Usage
//!
//! ```rust,no_run
//! use qntx_sqlite::SqliteStore;
//! use qntx_core::{AttestationBuilder, storage::{AttestationStore, QueryStore}, AxFilter};
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
//!
//! // Query with filters
//! let filter = AxFilter {
//!     subjects: vec!["ALICE".to_string()],
//!     ..Default::default()
//! };
//! let results = store.query(&filter)?;
//! # Ok(())
//! # }
//! ```
//!
//! # Example: Bounded Storage with Quotas
//!
//! ```rust,no_run
//! use qntx_sqlite::{BoundedStore, StorageQuotas};
//! use qntx_core::{AttestationBuilder, storage::AttestationStore};
//!
//! # fn main() -> Result<(), Box<dyn std::error::Error>> {
//! // Create store with custom quotas
//! let quotas = StorageQuotas::new(100, 256, 256); // 100 attestations, 256 predicates, 256 contexts
//! let mut store = BoundedStore::in_memory_with_quotas(quotas)?;
//!
//! // Attempts to exceed quotas will fail with QuotaExceeded error
//! # Ok(())
//! # }
//! ```

pub mod bounded;
pub mod error;
pub mod json;
pub mod migrate;
pub mod store;

// FFI module for CGO integration
#[cfg(feature = "ffi")]
pub mod ffi;

// Re-export main types
pub use bounded::{BoundedStore, StorageQuotas};
pub use error::{Result, SqliteError};
pub use store::SqliteStore;
