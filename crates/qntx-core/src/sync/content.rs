//! Deterministic content hashing for attestations.
//!
//! Produces a SHA-256 digest from an attestation's semantic fields:
//! subjects, predicates, contexts, actors, timestamp, and source.
//! Field order is canonicalized (sorted) so two nodes creating the
//! same claim independently get the same hash.

use sha2::{Digest, Sha256};

use crate::Attestation;

/// Compute a deterministic SHA-256 content hash from an attestation's semantic fields.
///
/// Excluded fields:
/// - `id` (ASID) — storage identity, not content identity
/// - `attributes` — mutable metadata
/// - `created_at` — local database artifact
///
/// Two attestations with identical semantic content produce the same hash
/// regardless of ASID, attributes, or creation time.
pub fn content_hash(attestation: &Attestation) -> [u8; 32] {
    let mut h = Sha256::new();

    // Domain separators prevent field-boundary collisions
    h.update(b"s:");
    h.update(canonical(&attestation.subjects).as_bytes());
    h.update(b"\np:");
    h.update(canonical(&attestation.predicates).as_bytes());
    h.update(b"\nc:");
    h.update(canonical(&attestation.contexts).as_bytes());
    h.update(b"\na:");
    h.update(canonical(&attestation.actors).as_bytes());
    h.update(b"\nt:");
    h.update(&attestation.timestamp.to_be_bytes());
    h.update(b"\nrc:");
    h.update(attestation.source.as_bytes());

    h.finalize().into()
}

/// Compute content hash and return as hex string.
pub fn content_hash_hex(attestation: &Attestation) -> String {
    hex::encode(content_hash(attestation))
}

/// JSON entry point for WASM bridge: takes JSON attestation, returns hex hash.
///
/// Input: JSON-serialized Attestation
/// Output: `{"hash":"<64-char hex>"}` or `{"error":"..."}`
pub fn content_hash_json(input: &str) -> String {
    match serde_json::from_str::<Attestation>(input) {
        Ok(att) => {
            let hash = content_hash_hex(&att);
            format!(r#"{{"hash":"{}"}}"#, hash)
        }
        Err(e) => format!(
            r#"{{"error":"invalid attestation JSON: {}"}}"#,
            e.to_string().replace('"', "\\\"")
        ),
    }
}

/// Sort a string slice and join with null bytes for deterministic hashing.
fn canonical(ss: &[String]) -> String {
    let mut sorted: Vec<&str> = ss.iter().map(|s| s.as_str()).collect();
    sorted.sort();
    sorted.join("\0")
}

// Inline hex encoding to avoid adding another dependency
mod hex {
    const HEX_CHARS: &[u8; 16] = b"0123456789abcdef";

    pub fn encode(bytes: [u8; 32]) -> String {
        let mut s = String::with_capacity(64);
        for b in bytes {
            s.push(HEX_CHARS[(b >> 4) as usize] as char);
            s.push(HEX_CHARS[(b & 0x0f) as usize] as char);
        }
        s
    }

    pub fn decode(s: &str) -> Option<[u8; 32]> {
        if s.len() != 64 {
            return None;
        }
        let mut out = [0u8; 32];
        for (i, chunk) in s.as_bytes().chunks(2).enumerate() {
            let hi = hex_val(chunk[0])?;
            let lo = hex_val(chunk[1])?;
            out[i] = (hi << 4) | lo;
        }
        Some(out)
    }

    fn hex_val(c: u8) -> Option<u8> {
        match c {
            b'0'..=b'9' => Some(c - b'0'),
            b'a'..=b'f' => Some(c - b'a' + 10),
            b'A'..=b'F' => Some(c - b'A' + 10),
            _ => None,
        }
    }
}

pub(crate) use hex::decode as hex_decode;
pub(crate) use hex::encode as hex_encode;

#[cfg(test)]
mod tests {
    use super::*;
    use crate::AttestationBuilder;

    fn test_attestation() -> Attestation {
        AttestationBuilder::new()
            .id("as-abc123")
            .subject("user-1")
            .predicate("member")
            .context("team-eng")
            .actor("hr-system")
            .timestamp(1718452800000)
            .source("cli")
            .build()
    }

    #[test]
    fn deterministic() {
        let att = test_attestation();
        assert_eq!(content_hash(&att), content_hash(&att));
    }

    #[test]
    fn order_independent() {
        let a = AttestationBuilder::new()
            .subjects(["b", "a"])
            .predicate("member")
            .context("team")
            .actor("sys")
            .timestamp(1000)
            .source("cli")
            .build();

        let b = AttestationBuilder::new()
            .subjects(["a", "b"])
            .predicate("member")
            .context("team")
            .actor("sys")
            .timestamp(1000)
            .source("cli")
            .build();

        assert_eq!(content_hash(&a), content_hash(&b));
    }

    #[test]
    fn different_content_different_hash() {
        let base = test_attestation();

        let mut diff_subject = base.clone();
        diff_subject.subjects = vec!["user-2".into()];
        assert_ne!(content_hash(&base), content_hash(&diff_subject));

        let mut diff_pred = base.clone();
        diff_pred.predicates = vec!["admin".into()];
        assert_ne!(content_hash(&base), content_hash(&diff_pred));

        let mut diff_ts = base.clone();
        diff_ts.timestamp = 9999;
        assert_ne!(content_hash(&base), content_hash(&diff_ts));
    }

    #[test]
    fn ignores_asid() {
        let a = test_attestation();
        let mut b = test_attestation();
        b.id = "as-different".into();
        assert_eq!(content_hash(&a), content_hash(&b));
    }

    #[test]
    fn ignores_attributes() {
        let a = test_attestation();
        let mut b = test_attestation();
        b.attributes
            .insert("color".into(), serde_json::json!("red"));
        assert_eq!(content_hash(&a), content_hash(&b));
    }

    #[test]
    fn json_roundtrip() {
        let att = test_attestation();
        let json = serde_json::to_string(&att).unwrap();
        let result = content_hash_json(&json);
        let parsed: serde_json::Value = serde_json::from_str(&result).unwrap();
        assert!(parsed["hash"].is_string());
        assert_eq!(parsed["hash"].as_str().unwrap().len(), 64);
    }

    #[test]
    fn json_invalid_input() {
        let result = content_hash_json("not json");
        let parsed: serde_json::Value = serde_json::from_str(&result).unwrap();
        assert!(parsed["error"].as_str().unwrap().contains("invalid attestation JSON"));
    }

    #[test]
    fn hex_roundtrip() {
        let hash = content_hash(&test_attestation());
        let encoded = hex_encode(hash);
        let decoded = hex_decode(&encoded).unwrap();
        assert_eq!(hash, decoded);
    }
}
