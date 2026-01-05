//! QNTX Fuzzy Matching Library
//!
//! High-performance fuzzy matching for QNTX attestation queries.
//! Provides multi-strategy matching (exact, prefix, substring, edit distance).
//!
//! ## Usage
//!
//! ```rust
//! use qntx_fuzzy::{FuzzyEngine, VocabularyType};
//!
//! let engine = FuzzyEngine::new();
//! engine.rebuild_index(
//!     vec!["is_author_of".to_string(), "works_at".to_string()],
//!     vec!["GitHub".to_string(), "Microsoft".to_string()],
//! );
//!
//! let (matches, time_us) = engine.find_matches(
//!     "author",
//!     VocabularyType::Predicates,
//!     Some(10),
//!     Some(0.6),
//! );
//! ```

pub mod engine;

// FFI module for C/CGO integration
pub mod ffi;

#[cfg(feature = "grpc")]
pub mod proto {
    tonic::include_proto!("qntx.fuzzy");
}

#[cfg(feature = "grpc")]
pub mod service;

// Re-export main types
pub use engine::{EngineConfig, FuzzyEngine, RankedMatch, VocabularyType};

// Re-export FFI types for C consumers
pub use ffi::{RustMatchC, RustMatchResultC, RustRebuildResultC};
