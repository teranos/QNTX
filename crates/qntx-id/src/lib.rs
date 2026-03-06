//! QNTX identity primitives (ADR-010)
//!
//! Two-layer identity model:
//! - **Vanity IDs**: Human-readable subject handles (`SARAH`, `SBVH`)
//! - **ASUIDs**: Content-addressed attestation identifiers
//!
//! This crate provides the foundational alphabet, normalization, and seed
//! cleaning that both layers build on.

pub mod alphabet;

pub use alphabet::{clean_seed, normalize_for_lookup, normalize_to_ascii, to_custom_alphabet};
