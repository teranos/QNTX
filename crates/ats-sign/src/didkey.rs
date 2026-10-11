//! did:key for an ed25519 public key: `did:key:z` + base58btc(0xed 0x01 + key).

const PREFIX: &str = "did:key:z";
const ED25519_PUB: [u8; 2] = [0xed, 0x01];

#[derive(Debug, thiserror::Error, PartialEq)]
pub enum DidKeyError {
    #[error("{did:?} is not a did:key")]
    NotDidKey { did: String },
    #[error("did:key {did} is not base58: {why}")]
    NotBase58 { did: String, why: String },
    #[error("did:key {did} decodes to {len} bytes, expected 34")]
    Length { did: String, len: usize },
    #[error("did:key {did} is not ed25519 (multicodec {first:x} {second:x})")]
    NotEd25519 { did: String, first: u8, second: u8 },
}

/// The did:key naming this public key.
pub fn encode(public_key: &[u8; 32]) -> String {
    let mut buf = Vec::with_capacity(ED25519_PUB.len() + public_key.len());
    buf.extend_from_slice(&ED25519_PUB);
    buf.extend_from_slice(public_key);
    format!("{PREFIX}{}", bs58::encode(buf).into_string())
}

/// The ed25519 public key a did:key names.
pub fn decode(did: &str) -> Result<[u8; 32], DidKeyError> {
    let body = did.strip_prefix(PREFIX).ok_or_else(|| DidKeyError::NotDidKey {
        did: did.to_string(),
    })?;
    let bytes = bs58::decode(body)
        .into_vec()
        .map_err(|e| DidKeyError::NotBase58 {
            did: did.to_string(),
            why: e.to_string(),
        })?;
    let Some((codec, key)) = bytes.split_first_chunk::<2>() else {
        return Err(DidKeyError::Length {
            did: did.to_string(),
            len: bytes.len(),
        });
    };
    if *codec != ED25519_PUB {
        let [first, second] = *codec;
        return Err(DidKeyError::NotEd25519 {
            did: did.to_string(),
            first,
            second,
        });
    }
    if key.len() != 32 {
        return Err(DidKeyError::Length {
            did: did.to_string(),
            len: bytes.len(),
        });
    }
    let mut public_key = [0u8; 32];
    public_key.copy_from_slice(key);
    Ok(public_key)
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde::Deserialize;

    #[derive(Deserialize)]
    struct Vector {
        public_key_hex: String,
        did: String,
    }

    fn key(hex: &str) -> [u8; 32] {
        let bytes: Vec<u8> = (0..hex.len())
            .step_by(2)
            .map(|i| u8::from_str_radix(&hex[i..i + 2], 16).unwrap())
            .collect();
        bytes.try_into().unwrap()
    }

    fn vectors() -> Vec<Vector> {
        serde_json::from_str(include_str!("../../../ats/signing/testdata/didkey.json")).unwrap()
    }

    #[test]
    fn encodes_what_go_encodes() {
        for v in vectors() {
            assert_eq!(encode(&key(&v.public_key_hex)), v.did);
        }
    }

    #[test]
    fn decodes_what_go_encodes() {
        for v in vectors() {
            assert_eq!(decode(&v.did).unwrap(), key(&v.public_key_hex));
        }
    }

    #[test]
    fn refuses_what_is_not_an_ed25519_did_key() {
        assert!(matches!(
            decode("did:web:example.com"),
            Err(DidKeyError::NotDidKey { .. })
        ));
        assert!(matches!(
            decode("did:key:z0OIl"),
            Err(DidKeyError::NotBase58 { .. })
        ));
        assert!(matches!(
            decode("did:key:z1"),
            Err(DidKeyError::Length { .. })
        ));
        let secp = format!("did:key:z{}", bs58::encode([0xe7, 0x01, 1, 2]).into_string());
        assert!(matches!(
            decode(&secp),
            Err(DidKeyError::NotEd25519 { .. })
        ));
    }
}
