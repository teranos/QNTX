//! Fuzzy Matching Engine
//!
//! Multi-strategy fuzzy matching with platform-specific optimizations:
//!
//! - **Native**: SIMD substring search, parallel matching, phonetic matching
//! - **WASM**: Sequential matching with pure-Rust algorithms
//!
//! # Strategies (in order of specificity)
//!
//! 1. Exact match (score: 1.0)
//! 2. Prefix match (score: 0.9)
//! 3. Word boundary match (score: 0.85)
//! 4. Substring match (score: 0.65-0.75)
//! 5. Phonetic match (score: 0.70-0.75) - native only
//! 6. Jaro-Winkler similarity (score: 0.6-0.825)
//! 7. Levenshtein edit distance (score: 0.6-0.8)
//!
//! # Example
//!
//! ```rust
//! use qntx_core::fuzzy::FuzzyEngine;
//!
//! let mut engine = FuzzyEngine::new();
//! engine.rebuild_index(
//!     vec!["is_author_of".into(), "is_maintainer_of".into()],
//!     vec!["GitHub".into(), "GitLab".into()],
//! );
//!
//! let matches = engine.search_predicates("author", 10, 0.6);
//! assert!(!matches.is_empty());
//! ```

mod engine;
mod strategies;

pub use engine::{EngineConfig, FuzzyEngine, FuzzyMatch, VocabularyType};
