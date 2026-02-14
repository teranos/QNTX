//! Content-addressed attestation sync primitives.
//!
//! Provides deterministic content hashing and a Merkle tree state digest
//! for efficient peer-to-peer attestation reconciliation.
//!
//! Content hashing produces the same SHA-256 digest for semantically identical
//! attestations regardless of ASID, attributes, or creation time. The Merkle
//! tree groups attestations by (actor, context) pairs — mirroring bounded
//! storage — for O(log n) state comparison between peers.

mod content;
mod merkle;

pub use content::content_hash;
pub use content::content_hash_hex;
pub use merkle::{GroupKey, MerkleDiff, MerkleTree};

// JSON-based entry points for WASM bridge
pub use content::content_hash_json;
pub use merkle::{
    merkle_contains_json, merkle_diff_json, merkle_find_group_key_json, merkle_group_hashes_json,
    merkle_insert_json, merkle_remove_json, merkle_root_json,
};
