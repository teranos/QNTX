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
//! laye-p2p: the key a browser holds. Mints it, persists it, signs with it,
//! and keeps the bindings a node signed over it. The host owns every pixel.

//! JS surface: init, did, sign, owner_did, owner_sign, bindings,
//! self_peer_id, accept_binding, errors.

pub mod didkey;
pub mod error;
pub mod store;

#[cfg(target_arch = "wasm32")]
pub mod wasm;
