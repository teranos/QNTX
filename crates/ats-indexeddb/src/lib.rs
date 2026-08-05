//! IndexedDB storage backend for QNTX attestations (browser WASM)
//!
//! This crate provides a persistent IndexedDB implementation matching the ats
//! storage trait contract, enabling browser WASM to store attestations using IndexedDB.
//!
//! Because IndexedDB is inherently asynchronous, the `IndexedDbStore` provides async
//! methods that mirror the synchronous `AttestationStore` and `QueryStore` traits from
//! ats. Same method names, same inputs, same outputs, same error semantics.
//!
//! # Schema
//!
//! Attestations are stored in an `"attestations"` object store with `id` as keyPath.
//! Array fields (subjects, predicates, contexts, actors) use native JS arrays with
//! multiEntry indexes for efficient lookups. Timestamps are stored as numbers
//! (milliseconds since epoch).
//!
//! # Example
//!
//! ```rust,ignore
//! use ats_indexeddb::IndexedDbStore;
//! use ats::AttestationBuilder;
//!
//! // Open (or create) the database
//! let store = IndexedDbStore::open("qntx").await?;
//!
//! // Create an attestation
//! let attestation = AttestationBuilder::new()
//!     .id("AS-test-1")
//!     .subject("ALICE")
//!     .predicate("knows")
//!     .context("work")
//!     .build();
//!
//! // Store and retrieve
//! store.put(attestation).await?;
//! let retrieved = store.get("AS-test-1").await?;
//! assert!(retrieved.is_some());
//! ```

pub mod error;
pub mod idb;
pub mod store;

pub use error::{IndexedDbError, Result};
pub use store::IndexedDbStore;

// Re-export proto conversion utilities from qntx-proto
pub use qntx_proto::proto_convert;
