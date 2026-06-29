//! Identity primitives for laye — Ed25519 keypair load/generate via
//! libp2p protobuf bytes. Home for identity now; auth later.

pub use libp2p_identity::Keypair;
use libp2p_identity::DecodingError;

#[derive(Debug)]
pub enum MeError {
    Decode(DecodingError),
    Encode(String),
}

impl std::fmt::Display for MeError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            MeError::Decode(e) => write!(f, "keypair protobuf decode: {e}"),
            MeError::Encode(e) => write!(f, "keypair protobuf encode: {e}"),
        }
    }
}

impl std::error::Error for MeError {
    fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
        match self {
            MeError::Decode(e) => Some(e),
            MeError::Encode(_) => None,
        }
    }
}

pub fn fresh() -> Keypair {
    Keypair::generate_ed25519()
}

pub fn load(bytes: &[u8]) -> Result<Keypair, MeError> {
    Keypair::from_protobuf_encoding(bytes).map_err(MeError::Decode)
}

pub fn to_bytes(keypair: &Keypair) -> Result<Vec<u8>, MeError> {
    keypair
        .to_protobuf_encoding()
        .map_err(|e| MeError::Encode(format!("{e}")))
}

pub fn load_or_fresh(bytes: Option<&[u8]>) -> Result<Keypair, MeError> {
    match bytes {
        Some(b) => load(b),
        None => Ok(fresh()),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use libp2p_identity::PeerId;

    #[test]
    fn fresh_keypair_has_ed25519_public_of_32_bytes() {
        let kp = fresh();
        let pk_bytes = kp
            .public()
            .try_into_ed25519()
            .expect("ed25519 public")
            .to_bytes();
        assert_eq!(pk_bytes.len(), 32);
    }

    #[test]
    fn fresh_round_trips_via_bytes() {
        let kp = fresh();
        let bytes = to_bytes(&kp).expect("encode");
        let restored = load(&bytes).expect("decode");
        assert_eq!(PeerId::from(kp.public()), PeerId::from(restored.public()));
    }

    #[test]
    fn corrupt_bytes_surface_as_decode_error() {
        let result = load(&[0xFF; 8]);
        assert!(matches!(result, Err(MeError::Decode(_))));
    }

    #[test]
    fn load_or_fresh_none_mints_fresh() {
        let kp = load_or_fresh(None).expect("fresh path");
        let _ = PeerId::from(kp.public());
    }

    #[test]
    fn load_or_fresh_some_restores_same_peer_id() {
        let kp = fresh();
        let bytes = to_bytes(&kp).expect("encode");
        let restored = load_or_fresh(Some(&bytes)).expect("restore");
        assert_eq!(PeerId::from(kp.public()), PeerId::from(restored.public()));
    }
}
