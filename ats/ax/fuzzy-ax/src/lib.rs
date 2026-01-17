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

// Import logging macros
#[macro_use]
extern crate log;

pub mod engine;

// FFI module for C/CGO integration
pub mod ffi;

/// Initialize the logger for the fuzzy matching library.
/// This should be called once at startup, typically from FFI.
///
/// The log level can be controlled via the RUST_LOG environment variable:
/// - RUST_LOG=qntx_fuzzy=debug
/// - RUST_LOG=qntx_fuzzy=trace
pub fn init_logger() {
    use std::sync::Once;
    static INIT: Once = Once::new();

    INIT.call_once(|| {
        env_logger::init();
        info!("QNTX Fuzzy matching library initialized");
        debug!("Logging is enabled at debug level");
    });
}

// Re-export main types
pub use engine::{AttributeMatch, EngineConfig, FuzzyEngine, RankedMatch, VocabularyType};

// Re-export FFI types for C consumers
pub use ffi::{RustMatchC, RustMatchResultC, RustRebuildResultC};
