//! ats-dev's own signing identity: an ed25519 key it mints on first use and
//! keeps in a file beside the database. The bytes signed are `ats-sign`'s, the
//! ones Go's `ats/signing` signs and verifies; the signer is named by its
//! `did:key`, as the node names itself. This is not the node's key and its DID
//! is not in the node's DID document.

use std::path::{Path, PathBuf};

use ats::Attestation;
use ats_sign::{didkey, Signable};
use laye_me::Keypair;
use libp2p_identity::ed25519;
use sacred_error::Error;

use crate::fail;

pub struct Signer {
    key: Keypair,
    pub did: String,
}

/// Where the key lives: `--key`, else `$ATS_DEV_KEY`, else `<db>.key`.
pub fn key_path(flag: Option<&str>, db: &str) -> PathBuf {
    match (flag, std::env::var("ATS_DEV_KEY")) {
        (Some(f), _) => PathBuf::from(f),
        (None, Ok(e)) => PathBuf::from(e),
        (None, Err(_)) => PathBuf::from(format!("{db}.key")),
    }
}

impl Signer {
    /// The key at `path`, or a fresh one written there (0600) if there is none.
    /// A file that exists and is not a key is an error, never replaced.
    pub fn load_or_mint(path: &Path) -> Result<Self, Box<Error>> {
        let key = match std::fs::read(path) {
            Ok(bytes) => laye_me::load(&bytes)
                .map_err(|e| fail("the key file is not a key", format!("{}: {e}", path.display())))?,
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => {
                let k = laye_me::fresh();
                let bytes = laye_me::to_bytes(&k).map_err(|e| fail("could not encode the key", e))?;
                write_private(path, &bytes)?;
                k
            }
            Err(e) => return Err(fail("could not read the key file", format!("{}: {e}", path.display()))),
        };
        Self::from_keypair(key)
    }

    pub fn from_keypair(key: Keypair) -> Result<Self, Box<Error>> {
        let ed = key
            .clone()
            .try_into_ed25519()
            .map_err(|e| fail("the key is not ed25519", e))?;
        let did = didkey::encode(&ed.public().to_bytes());
        Ok(Self { key, did })
    }

    /// The attestation, signed over `ats-sign`'s canonical bytes. The actors
    /// are under the signature, so they must be settled before this is called.
    pub fn sign(&self, mut a: Attestation) -> Result<Attestation, Box<Error>> {
        let bytes = canonical(&a)?;
        a.signature = Some(self.key.sign(bytes.as_bytes()).map_err(|e| fail("could not sign", e))?);
        a.signer_did = Some(self.did.clone());
        Ok(a)
    }
}

/// What a signature on an attestation amounts to.
#[derive(Debug, PartialEq, Eq)]
pub enum Verdict {
    Unsigned,
    Valid(String),
    Invalid(String),
}

pub fn verify(a: &Attestation) -> Verdict {
    match (&a.signature, &a.signer_did) {
        (None, None) => Verdict::Unsigned,
        (Some(_), None) | (None, Some(_)) => Verdict::Invalid("a signature and a signer are given together or not at all".into()),
        (Some(sig), Some(did)) => {
            let check = || -> Result<bool, String> {
                let pk = didkey::decode(did).map_err(|e| e.to_string())?;
                let ed = ed25519::PublicKey::try_from_bytes(&pk).map_err(|e| e.to_string())?;
                let bytes = canonical(a).map_err(|e| e.why)?;
                Ok(libp2p_identity::PublicKey::from(ed).verify(bytes.as_bytes(), sig))
            };
            match check() {
                Ok(true) => Verdict::Valid(did.clone()),
                Ok(false) => Verdict::Invalid(format!("signature does not stand for these bytes (signer {did})")),
                Err(why) => Verdict::Invalid(why),
            }
        }
    }
}

/// `ats-sign`'s canonical JSON of an attestation as ats-dev holds it.
pub fn canonical(a: &Attestation) -> Result<String, Box<Error>> {
    let wire = serde_json::to_string(a).map_err(|e| fail("could not encode an attestation", e))?;
    Signable::from_wire_json(&wire)
        .and_then(|s| s.canonical_json())
        .map_err(|e| fail("could not produce the bytes to sign", e))
}

#[cfg(unix)]
fn write_private(path: &Path, bytes: &[u8]) -> Result<(), Box<Error>> {
    use std::io::Write;
    use std::os::unix::fs::OpenOptionsExt;
    std::fs::OpenOptions::new()
        .write(true)
        .create_new(true)
        .mode(0o600)
        .open(path)
        .and_then(|mut f| f.write_all(bytes))
        .map_err(|e| fail("could not write the key file", format!("{}: {e}", path.display())))
}

#[cfg(not(unix))]
fn write_private(path: &Path, bytes: &[u8]) -> Result<(), Box<Error>> {
    std::fs::write(path, bytes)
        .map_err(|e| fail("could not write the key file", format!("{}: {e}", path.display())))
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used, clippy::indexing_slicing, clippy::string_slice)]
mod tests {
    use super::*;

    fn hex(s: &str) -> Vec<u8> {
        (0..s.len()).step_by(2).map(|i| u8::from_str_radix(&s[i..i + 2], 16).unwrap()).collect()
    }

    /// Go's vectors: the same seed, the same attestations, the signatures
    /// `ats/signing` made. ed25519 is deterministic, so ats-dev's must be equal.
    #[test]
    fn it_signs_what_go_signed_byte_for_byte() {
        let sigs: serde_json::Value = serde_json::from_str(include_str!("../../../ats/signing/testdata/signatures.json")).unwrap();
        let cases: serde_json::Value = serde_json::from_str(include_str!("../../../ats/signing/testdata/canonical.json")).unwrap();
        let seed = hex(sigs["seed_hex"].as_str().unwrap());
        let secret = ed25519::SecretKey::try_from_bytes(seed).unwrap();
        let signer = Signer::from_keypair(Keypair::from(ed25519::Keypair::from(secret))).unwrap();

        let expected = sigs["signatures"].as_array().unwrap();
        assert!(!expected.is_empty());
        for e in expected {
            let name = e["name"].as_str().unwrap();
            let case = cases.as_array().unwrap().iter().find(|c| c["name"] == name).unwrap();
            let wire = case["wire"].to_string();
            let a: Attestation = serde_json::from_str(&wire).unwrap();
            assert_eq!(canonical(&a).unwrap(), case["canonical"].as_str().unwrap(), "{name}: bytes");
            let signed = signer.sign(a).unwrap();
            assert_eq!(signed.signature.clone().unwrap(), hex(e["signature_hex"].as_str().unwrap()), "{name}: signature");
            assert_eq!(verify(&signed), Verdict::Valid(signer.did.clone()), "{name}: verifies");
        }
    }

    #[test]
    fn a_changed_field_no_longer_verifies() {
        let signer = Signer::from_keypair(laye_me::fresh()).unwrap();
        let mut a = Attestation { id: "AS-1".into(), subjects: vec!["a".into()], actors: vec!["x".into()], ..Attestation::default() };
        a = signer.sign(a).unwrap();
        assert!(matches!(verify(&a), Verdict::Valid(_)));
        a.subjects = vec!["b".into()];
        assert!(matches!(verify(&a), Verdict::Invalid(_)));
    }
}
