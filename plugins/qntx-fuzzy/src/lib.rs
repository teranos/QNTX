//! QNTX Fuzzy Matching Library (CGO)
//!
//! High-performance fuzzy matching for QNTX attestation queries.
//! Provides multi-strategy matching (exact, prefix, substring, edit distance).
//!
//! This library is designed to be used from Go via CGO for maximum performance.
//!
//! ## Usage from Go via CGO
//!
//! ```go
//! engine := cgo.NewFuzzyEngine()
//! defer engine.Free()
//!
//! engine.RebuildIndex(predicates, contexts)
//! result := engine.FindMatches("author", 0, 10, 0.6)
//! ```

pub mod engine;

// FFI module for C/CGO integration
pub mod ffi;

// Re-export main types
pub use engine::{EngineConfig, FuzzyEngine, RankedMatch, VocabularyType};

// Re-export FFI types for C consumers
pub use ffi::{RustMatchC, RustMatchResultC, RustRebuildResultC};
