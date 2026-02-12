//! QNTX Core Engine
//!
//! This crate provides the computational kernel for QNTX, designed to run
//! identically in browser (WASM) and server (native) environments.
//!
//! # Features
//!
//! - `native` - Enable all native optimizations (SIMD, parallel, phonetic)
//! - `simd` - SIMD-accelerated substring search via memchr
//! - `parallel` - Parallel matching via rayon for large vocabularies
//! - `phonetic` - Phonetic matching via Double Metaphone algorithm
//! - `wasm` - WASM-compatible build (excludes native-only features)
//!
//! # Example
//!
//! ```rust
//! use qntx_core::parser::Parser;
//! use qntx_core::fuzzy::FuzzyEngine;
//!
//! // Parse an AX query
//! let query = Parser::parse("ALICE is author_of of GitHub").unwrap();
//! assert_eq!(query.subjects, vec!["ALICE"]);
//!
//! // Fuzzy search
//! let mut engine = FuzzyEngine::new();
//! engine.rebuild_index(vec!["author_of".into(), "maintainer_of".into()], vec![]);
//! let matches = engine.search_predicates("author", 10, 0.6);
//! ```

pub mod attestation;
pub mod classify;
pub mod expand;
pub mod fuzzy;
pub mod parser;
pub mod storage;

// Re-export main types at crate root
pub use attestation::{Attestation, AttestationBuilder, AxFilter, AxResult, Conflict};
pub use classify::{
    classify_claims, ActorCredibility, ClaimGroup, ClaimInput, ClaimTiming, ClaimWithTiming,
    ClassificationResult, ClassifyInput, ClassifyOutput, ConfidenceCalculator, ConflictType,
    SmartClassifier, TemporalAnalyzer, TemporalConfig, TemporalPattern,
};
pub use expand::{
    dedup_source_ids, dedup_source_ids_json, expand_cartesian, expand_claims_json, group_by_key,
    group_claims_json, DedupInput, DedupOutput, ExpandAttestation, ExpandInput, ExpandOutput,
    GroupInput, GroupOutput, IndividualClaim,
};
pub use fuzzy::{FuzzyEngine, FuzzyMatch};
pub use parser::{AxQuery, ParseError, Parser, TemporalClause};
pub use storage::{AttestationStore, MemoryStore, QueryStore, StoreError};
