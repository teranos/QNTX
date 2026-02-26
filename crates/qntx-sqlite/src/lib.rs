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
//! # Example: Bounded Storage with Eviction
//!
//! ```rust,no_run
//! use qntx_sqlite::{BoundedStore, BoundedConfig};
//! use qntx_core::{AttestationBuilder, storage::AttestationStore};
//!
//! # fn main() -> Result<(), Box<dyn std::error::Error>> {
//! // Create store with custom limits (attestations per actor-context, contexts per actor, actors per entity)
//! let config = BoundedConfig { actor_context_limit: 32, ..Default::default() };
//! let mut store = BoundedStore::in_memory_with_config(config)?;
//!
//! // Inserts succeed; oldest attestations are evicted when limits are exceeded
//! # Ok(())
//! # }
//! ```

pub mod bounded;
pub mod error;
pub mod json;
pub mod migrate;
pub mod store;
pub mod vec;

// Re-export proto conversion utilities from qntx-proto
pub use qntx_proto::proto_convert;

// FFI module for CGO integration
#[cfg(feature = "ffi")]
pub mod ffi;

// Re-export main types
pub use bounded::{BoundedConfig, BoundedStore, EvictionResult};
pub use error::{Result, SqliteError};
pub use store::SqliteStore;
