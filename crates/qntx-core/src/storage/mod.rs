//! Storage abstraction for attestations
//!
//! This module defines the `AttestationStore` trait that abstracts over different
//! storage backends. Implementations exist for:
//!
//! - **Memory**: In-memory storage for testing (`MemoryStore`)
//! - **SQLite**: Native SQLite via rusqlite (`qntx-sqlite` crate, native only)
//! - **IndexedDB**: Browser storage via web-sys (`qntx-indexeddb` crate, WASM only)
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
//! # Backend Crates
//!
//! - `qntx-sqlite`: SQLite backend for native platforms (Tauri, server)
//! - `qntx-indexeddb`: IndexedDB backend for browser WASM (async API matching
//!   the same trait contract)

pub mod enforcement;
mod error;
mod memory;
mod traits;

pub use enforcement::{EnforcementConfig, EnforcementEvent, EnforcementInput, EvictionDetails};
pub use error::StoreError;
pub use memory::MemoryStore;
pub use traits::{AttestationStore, QueryStore, StorageStats};
