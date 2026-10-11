#![cfg_attr(
    test,
    allow(
        clippy::unwrap_used,
        clippy::expect_used,
        clippy::panic,
        clippy::indexing_slicing,
        clippy::string_slice
    )
)]
//! What an attestation's signature is over, and who it names as signer.
//!
//! A signature made in the browser is verified by a Go node, and one made by a
//! Go node is verified by whoever syncs it. Both must reach the same bytes, so
//! the bytes are made here, once: [`Signable::canonical_json`] writes what
//! `ats/signing.CanonicalJSON` wrote, byte for byte, and
//! `ats/signing/testdata` holds the vectors both sides are tested against.
//!
//! No key lives here. The signer holds its own: the node in Go, the browser in
//! laye-p2p.

pub mod canonical;
pub mod didkey;

pub use canonical::{CanonicalError, Signable};
pub use didkey::DidKeyError;
