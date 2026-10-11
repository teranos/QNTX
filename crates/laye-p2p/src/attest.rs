//! An attestation signed with the key this browser holds, over the bytes a
//! node verifies it by (crates/ats-sign). The browser signs what it attests,
//! so no node signs it on the browser's behalf.

use ats_sign::{CanonicalError, Signable};
use laye_me::Keypair;

#[derive(Debug, thiserror::Error)]
pub enum AttestError {
    #[error(transparent)]
    Canonical(#[from] CanonicalError),
    #[error("signing {id} failed: {why}")]
    Sign { id: String, why: String },
}

/// The signature over an attestation in proto JSON, as the browser holds it.
pub fn sign_attestation(keypair: &Keypair, attestation_json: &str) -> Result<Vec<u8>, AttestError> {
    sign_signable(keypair, &Signable::from_proto_json(attestation_json)?)
}

pub fn sign_signable(keypair: &Keypair, signable: &Signable) -> Result<Vec<u8>, AttestError> {
    let canonical = signable.canonical_json()?;
    keypair
        .sign(canonical.as_bytes())
        .map_err(|e| AttestError::Sign {
            id: signable.id.clone(),
            why: e.to_string(),
        })
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde::Deserialize;

    #[derive(Deserialize)]
    struct Vector {
        name: String,
        wire: serde_json::Value,
    }

    #[derive(Deserialize)]
    struct Signatures {
        seed_hex: String,
        signatures: Vec<Signature>,
    }

    #[derive(Deserialize)]
    struct Signature {
        name: String,
        signature_hex: String,
    }

    fn bytes(hex: &str) -> Vec<u8> {
        (0..hex.len())
            .step_by(2)
            .map(|i| u8::from_str_radix(&hex[i..i + 2], 16).unwrap())
            .collect()
    }

    /// A browser's key signs each vector as a Go node's key signs it: same
    /// seed, same bytes, same signature (ats/signing/testdata).
    #[test]
    fn signs_as_the_node_signs() {
        let vectors: Vec<Vector> = serde_json::from_str(include_str!(
            "../../../ats/signing/testdata/canonical.json"
        ))
        .unwrap();
        let fixture: Signatures = serde_json::from_str(include_str!(
            "../../../ats/signing/testdata/signatures.json"
        ))
        .unwrap();
        let keypair = Keypair::from(crate::didkey::keypair_from_seed(&bytes(&fixture.seed_hex)).unwrap());

        for v in vectors {
            let want = fixture
                .signatures
                .iter()
                .find(|s| s.name == v.name)
                .unwrap();
            let signable = Signable::from_wire_json(&v.wire.to_string()).unwrap();
            let got = sign_signable(&keypair, &signable).unwrap();
            assert_eq!(got, bytes(&want.signature_hex), "{}", v.name);
        }
    }

    #[test]
    fn an_attestation_that_does_not_read_is_not_signed() {
        let keypair = laye_me::fresh();
        assert!(matches!(
            sign_attestation(&keypair, r#"{"id":"AS-1"}"#),
            Err(AttestError::Canonical(_))
        ));
    }
}
