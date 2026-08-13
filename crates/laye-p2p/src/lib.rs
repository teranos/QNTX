//! laye-p2p: browser social plugin. Bevy-free. Owns identity. Owns its DOM.
//!
//! JS surface: init, subscribe_opaque, pending_bytes, recv_bytes, publish, self_peer_id.
//! Framing: [u32 LE len][bytes]... into the host's rx buffer.
//! Consumers (e.g. game.wasm) read peer_id from laye; laye is the identity owner.

pub mod binding;
pub mod chat;
pub mod didkey;
pub mod error;
pub mod identity;
pub mod state;
pub mod store;

#[cfg(target_arch = "wasm32")]
pub mod wasm;
