//! Identity persistence abstraction. Native tests use `InMemoryStore`;
//! the wasm module wires up `IndexedDbStore` under `crates/laye-p2p/src/wasm.rs`.
//!
//! `load_or_generate` is where the LK flow lives: bytes → keypair,
//! or fresh keypair → bytes → store.

use laye_me::{Keypair, MeError};
use std::cell::RefCell;

#[derive(Debug, thiserror::Error)]
pub enum StoreError {
    #[error("identity encode/decode: {0}")]
    Codec(#[from] MeError),
    #[error("backing store: {0}")]
    Backend(String),
}

pub trait IdentityStore {
    fn load(&self) -> Result<Option<Vec<u8>>, StoreError>;
    fn save(&self, bytes: &[u8]) -> Result<(), StoreError>;
}

#[derive(Default)]
pub struct InMemoryStore {
    bytes: RefCell<Option<Vec<u8>>>,
}

impl InMemoryStore {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn with(bytes: Vec<u8>) -> Self {
        Self {
            bytes: RefCell::new(Some(bytes)),
        }
    }

    pub fn from_option(bytes: Option<Vec<u8>>) -> Self {
        Self {
            bytes: RefCell::new(bytes),
        }
    }
}

impl IdentityStore for InMemoryStore {
    fn load(&self) -> Result<Option<Vec<u8>>, StoreError> {
        Ok(self.bytes.borrow().clone())
    }

    fn save(&self, bytes: &[u8]) -> Result<(), StoreError> {
        *self.bytes.borrow_mut() = Some(bytes.to_vec());
        Ok(())
    }
}

/// LK core: if the store has bytes, decode them into a keypair;
/// otherwise mint a fresh one and persist. A fresh keypair reappears
/// on the next call — reload → same peer_id is the LK milestone.
pub fn load_or_generate<S: IdentityStore>(store: &S) -> Result<Keypair, StoreError> {
    match store.load()? {
        Some(bytes) => Ok(laye_me::load(&bytes)?),
        None => {
            let kp = laye_me::fresh();
            let bytes = laye_me::to_bytes(&kp)?;
            store.save(&bytes)?;
            Ok(kp)
        }
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;

    #[test]
    fn empty_store_mints_and_persists() {
        let store = InMemoryStore::new();
        let kp = load_or_generate(&store).expect("mint");
        let bytes = store.load().expect("load").expect("saved");
        assert_eq!(bytes, laye_me::to_bytes(&kp).unwrap());
    }

    #[test]
    fn populated_store_returns_same_peer_id() {
        let first = laye_me::fresh();
        let first_peer_id = first.public().to_peer_id();
        let bytes = laye_me::to_bytes(&first).unwrap();
        let store = InMemoryStore::with(bytes);
        let second = load_or_generate(&store).expect("restore");
        assert_eq!(first_peer_id, second.public().to_peer_id());
    }

    #[test]
    fn two_calls_on_fresh_store_return_same_peer_id() {
        let store = InMemoryStore::new();
        let first = load_or_generate(&store).expect("mint");
        let second = load_or_generate(&store).expect("restore");
        assert_eq!(first.public().to_peer_id(), second.public().to_peer_id());
    }

    #[test]
    fn corrupt_bytes_surface_as_codec_error() {
        let store = InMemoryStore::with(vec![0xff; 8]);
        let result = load_or_generate(&store);
        assert!(matches!(result, Err(StoreError::Codec(_))));
    }
}
