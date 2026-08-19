pub use libp2p_identity::Keypair;
use libp2p_identity::{DecodingError, PublicKey};
use serde::{Deserialize, Serialize};

#[derive(Debug, thiserror::Error)]
pub enum MeError {
    #[error("keypair protobuf decode: {0}")]
    Decode(#[from] DecodingError),
    #[error("keypair protobuf encode: {0}")]
    Encode(String),
}

#[derive(Debug, thiserror::Error)]
pub enum VerifyError {
    #[error("signer_pubkey not a valid Ed25519 key: {0}")]
    BadSignerKey(String),
    #[error("signature does not match canonical bytes")]
    BadSignature,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct BindingClaim {
    #[serde(with = "hex_bytes_32", rename = "peer_pubkey_hex")]
    pub peer_pubkey: [u8; 32],
    pub provider: String,
    pub canonical_id: String,
    pub handle: Option<String>,
    pub issued_at: u64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SignedBinding {
    pub claim: BindingClaim,
    #[serde(with = "hex_bytes_vec", rename = "signature_hex")]
    pub signature: Vec<u8>,
    #[serde(with = "hex_bytes_32", rename = "signer_pubkey_hex")]
    pub signer_pubkey: [u8; 32],
}

impl BindingClaim {
    pub fn canonical_bytes(&self) -> Vec<u8> {
        let peer_hex = hex_lower(&self.peer_pubkey);
        let handle = self.handle.as_deref().unwrap_or("");
        format!(
            "laye-binding/v1|{}|{}|{}|{}|{}",
            peer_hex, self.provider, self.canonical_id, handle, self.issued_at,
        )
        .into_bytes()
    }
}

impl SignedBinding {
    pub fn verify(&self) -> Result<(), VerifyError> {
        let ed = libp2p_identity::ed25519::PublicKey::try_from_bytes(&self.signer_pubkey)
            .map_err(|e| VerifyError::BadSignerKey(format!("{e}")))?;
        let public: PublicKey = ed.into();
        if public.verify(&self.claim.canonical_bytes(), &self.signature) {
            Ok(())
        } else {
            Err(VerifyError::BadSignature)
        }
    }
}

fn hex_lower(bytes: &[u8]) -> String {
    const HEX: &[u8; 16] = b"0123456789abcdef";
    let mut s = String::with_capacity(bytes.len() * 2);
    for b in bytes {
        s.push(HEX[(b >> 4) as usize] as char);
        s.push(HEX[(b & 0x0f) as usize] as char);
    }
    s
}

fn hex_decode(s: &str) -> Result<Vec<u8>, String> {
    if !s.len().is_multiple_of(2) {
        return Err(format!("hex length not even: {}", s.len()));
    }
    let mut out = Vec::with_capacity(s.len() / 2);
    for chunk in s.as_bytes().chunks(2) {
        let hi = hex_nibble(chunk[0])?;
        let lo = hex_nibble(chunk[1])?;
        out.push((hi << 4) | lo);
    }
    Ok(out)
}

fn hex_nibble(c: u8) -> Result<u8, String> {
    match c {
        b'0'..=b'9' => Ok(c - b'0'),
        b'a'..=b'f' => Ok(10 + c - b'a'),
        b'A'..=b'F' => Ok(10 + c - b'A'),
        _ => Err(format!("non-hex byte: 0x{c:02x}")),
    }
}

mod hex_bytes_32 {
    use serde::{Deserialize, Deserializer, Serializer};

    pub fn serialize<S: Serializer>(bytes: &[u8; 32], s: S) -> Result<S::Ok, S::Error> {
        s.serialize_str(&super::hex_lower(bytes))
    }

    pub fn deserialize<'de, D: Deserializer<'de>>(d: D) -> Result<[u8; 32], D::Error> {
        let s = String::deserialize(d)?;
        let bytes = super::hex_decode(&s).map_err(serde::de::Error::custom)?;
        if bytes.len() != 32 {
            return Err(serde::de::Error::custom(format!(
                "expected 32 bytes, got {}",
                bytes.len()
            )));
        }
        let mut out = [0u8; 32];
        out.copy_from_slice(&bytes);
        Ok(out)
    }
}

mod hex_bytes_vec {
    use serde::{Deserialize, Deserializer, Serializer};

    #[allow(clippy::ptr_arg)]
    pub fn serialize<S: Serializer>(bytes: &Vec<u8>, s: S) -> Result<S::Ok, S::Error> {
        s.serialize_str(&super::hex_lower(bytes))
    }

    pub fn deserialize<'de, D: Deserializer<'de>>(d: D) -> Result<Vec<u8>, D::Error> {
        let s = String::deserialize(d)?;
        super::hex_decode(&s).map_err(serde::de::Error::custom)
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
#[allow(clippy::unwrap_used, clippy::expect_used)]
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

    #[test]
    fn canonical_bytes_is_pipe_delimited_v1_format() {
        let claim = BindingClaim {
            peer_pubkey: [0xab; 32],
            provider: "mastodon".to_string(),
            canonical_id: "https://mastodon.example/@tim".to_string(),
            handle: Some("@tim@mastodon.example".to_string()),
            issued_at: 1_735_000_000,
        };
        assert_eq!(
            claim.canonical_bytes(),
            b"laye-binding/v1|abababababababababababababababababababababababababababababababab|mastodon|https://mastodon.example/@tim|@tim@mastodon.example|1735000000",
        );
    }

    #[test]
    fn canonical_bytes_empty_handle_renders_as_empty_string() {
        let claim = BindingClaim {
            peer_pubkey: [0u8; 32],
            provider: "atproto".to_string(),
            canonical_id: "did:plc:xyz".to_string(),
            handle: None,
            issued_at: 0,
        };
        let bytes = claim.canonical_bytes();
        let s = std::str::from_utf8(&bytes).expect("utf8");
        assert!(s.contains("|atproto|did:plc:xyz||0"));
    }

    #[test]
    fn ed25519_sign_then_verify_round_trips_the_claim() {
        let signer = fresh();
        let claim = BindingClaim {
            peer_pubkey: [0x11; 32],
            provider: "mastodon".to_string(),
            canonical_id: "https://mastodon.example/@tim".to_string(),
            handle: Some("@tim@mastodon.example".to_string()),
            issued_at: 1_735_000_000,
        };
        let canonical = claim.canonical_bytes();
        let sig = signer.sign(&canonical).expect("sign");
        assert!(signer.public().verify(&canonical, &sig));
    }

    #[test]
    fn tampered_canonical_bytes_fail_verification() {
        let signer = fresh();
        let claim = BindingClaim {
            peer_pubkey: [0x11; 32],
            provider: "mastodon".to_string(),
            canonical_id: "https://mastodon.example/@tim".to_string(),
            handle: None,
            issued_at: 1_735_000_000,
        };
        let canonical = claim.canonical_bytes();
        let sig = signer.sign(&canonical).expect("sign");
        let mut tampered = canonical.clone();
        tampered[0] ^= 0xff;
        assert!(!signer.public().verify(&tampered, &sig));
    }

    fn sample_signed_binding() -> SignedBinding {
        let signer = fresh();
        let claim = BindingClaim {
            peer_pubkey: [0xab; 32],
            provider: "mastodon".to_string(),
            canonical_id: "https://mastodon.example/@tim".to_string(),
            handle: Some("@tim@mastodon.example".to_string()),
            issued_at: 1_735_000_000,
        };
        let signature = signer.sign(&claim.canonical_bytes()).expect("sign");
        let signer_pubkey = signer
            .public()
            .try_into_ed25519()
            .expect("ed25519")
            .to_bytes();
        SignedBinding {
            claim,
            signature,
            signer_pubkey,
        }
    }

    #[test]
    fn signed_binding_wire_json_uses_hex_suffix_field_names() {
        let sb = sample_signed_binding();
        let json = serde_json::to_string(&sb).expect("serialize");
        // Same field names the broker page emits via postMessage
        // (hex-encoded byte fields, JSON-plain string/number for the
        // rest) — one wire shape for broker→app and app↔app.
        assert!(json.contains("\"peer_pubkey_hex\":"));
        assert!(json.contains("\"signature_hex\":"));
        assert!(json.contains("\"signer_pubkey_hex\":"));
        assert!(json.contains("\"provider\":\"mastodon\""));
        assert!(json.contains("\"canonical_id\":\"https://mastodon.example/@tim\""));
        assert!(json.contains("\"handle\":\"@tim@mastodon.example\""));
        assert!(json.contains("\"issued_at\":1735000000"));
    }

    #[test]
    fn signed_binding_json_round_trips() {
        let sb = sample_signed_binding();
        let json = serde_json::to_string(&sb).expect("serialize");
        let back: SignedBinding = serde_json::from_str(&json).expect("deserialize");
        assert_eq!(back.claim.peer_pubkey, sb.claim.peer_pubkey);
        assert_eq!(back.claim.provider, sb.claim.provider);
        assert_eq!(back.claim.canonical_id, sb.claim.canonical_id);
        assert_eq!(back.claim.handle, sb.claim.handle);
        assert_eq!(back.claim.issued_at, sb.claim.issued_at);
        assert_eq!(back.signature, sb.signature);
        assert_eq!(back.signer_pubkey, sb.signer_pubkey);
    }

    #[test]
    fn verify_ok_on_matching_signer_and_signature() {
        let sb = sample_signed_binding();
        sb.verify().expect("verify");
    }

    #[test]
    fn verify_fails_on_tampered_signature() {
        let mut sb = sample_signed_binding();
        sb.signature[0] ^= 0xff;
        assert!(matches!(sb.verify(), Err(VerifyError::BadSignature)));
    }

    #[test]
    fn verify_fails_on_tampered_claim() {
        let mut sb = sample_signed_binding();
        sb.claim.canonical_id = "https://mastodon.example/@spike".to_string();
        assert!(matches!(sb.verify(), Err(VerifyError::BadSignature)));
    }

    #[test]
    fn verify_fails_on_wrong_signer_pubkey() {
        let mut sb = sample_signed_binding();
        let other = fresh();
        sb.signer_pubkey = other
            .public()
            .try_into_ed25519()
            .expect("ed25519")
            .to_bytes();
        assert!(matches!(sb.verify(), Err(VerifyError::BadSignature)));
    }
}
