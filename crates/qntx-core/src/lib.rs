//! QNTX Core Engine
//!
//! This crate provides the computational kernel for QNTX, designed to run
//! identically in browser (WASM) and server (native) environments.
//!
//! # Features
//!
//! - `wasm` - WASM-compatible build
//!
//! # Example
//!
//! ```rust
//! use qntx_core::parser::Parser;
//!
//! // Parse an AX query
//! let query = Parser::parse("ALICE is author_of of GitHub").unwrap();
//! assert_eq!(query.subjects, vec!["ALICE"]);
//! ```

pub mod attestation;
pub mod classify;
pub mod expand;
pub mod parser;
pub mod similarity;
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
pub use parser::{AxQuery, Lexer, ParseError, Parser, TemporalClause, Token, TokenKind};
pub use storage::{AttestationStore, MemoryStore, QueryStore, StoreError};
