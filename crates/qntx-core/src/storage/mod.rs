//! Storage abstraction for attestations
//!
//! This module defines the `AttestationStore` trait that abstracts over different
//! storage backends. Implementations exist for:
//!
//! - **Memory**: In-memory storage for testing (`MemoryStore`)
//! - **SQLite**: Native SQLite via rusqlite (separate crate, native only)
//! - **IndexedDB**: Browser storage via idb (separate crate, WASM only)
//!
//! # Example
//!
//! ```rust
//! use qntx_core::storage::{AttestationStore, MemoryStore};
//! use qntx_core::attestation::AttestationBuilder;
//!
//! let mut store = MemoryStore::new();
//!
//! let attestation = AttestationBuilder::new()
//!     .id("AS-test-123")
//!     .subject("ALICE")
//!     .predicate("knows")
//!     .context("work")
//!     .actor("human:bob")
//!     .build();
//!
//! store.put(attestation).unwrap();
//! let retrieved = store.get("AS-test-123").unwrap();
//! assert!(retrieved.is_some());
//! ```
//!
//! # Future Backends
//!
//! TODO: Implement SQLite backend for native platforms (Tauri, server)
//! - Use rusqlite crate
//! - Port schema from Go's db/sqlite/migrations/
//! - Implement AttestationStore + QueryStore traits
//! - Add connection pooling for server use
//! - Feature-gate behind `sqlite` feature
//!
//! TODO: Implement IndexedDB backend for browser WASM
//! - Use idb or indexed_db_futures crate
//! - Implement AttestationStore + QueryStore traits
//! - Handle async nature of IndexedDB (may need async trait variant)
//! - Feature-gate behind `indexeddb` feature
//!
//! TODO: Add CGO wrapper for Go server to use Rust storage
//! - Similar pattern to fuzzy-ax CGO wrapper
//! - Expose AttestationStore operations via C FFI
//! - Go server can then use qntx-core storage instead of Go sql_store.go

mod error;
mod memory;
mod traits;

pub use error::StoreError;
pub use memory::MemoryStore;
pub use traits::{AttestationStore, QueryStore, StorageStats};
