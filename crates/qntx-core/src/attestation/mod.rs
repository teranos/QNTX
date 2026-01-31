//! Attestation types - the core data model of QNTX
//!
//! An attestation is a verifiable claim about subjects, predicates, and contexts
//! with actor attribution and timestamps.
//!
//! # Example
//!
//! ```rust
//! use qntx_core::attestation::{Attestation, AttestationBuilder};
//!
//! let attestation = AttestationBuilder::new()
//!     .subject("ALICE")
//!     .predicate("is_author_of")
//!     .context("GitHub")
//!     .actor("human:bob")
//!     .build();
//!
//! assert_eq!(attestation.subjects, vec!["ALICE"]);
//! ```

mod types;

pub use types::{
    Attestation, AttestationBuilder, AxFilter, AxResult, AxSummary, Conflict, OverFilter,
};
