//! ats-dev signs as the key kept in `~/.qntx/key`: the one `qntx` signs a
//! request to a node with, a 32-byte ed25519 seed in hex that `ground code
//! login` keeps. ats-dev never mints one. The bytes an attestation is signed
//! over are `ats-sign`'s, the ones the node verifies (through `ats-wasm`); a
//! request is signed as `qntx sign` signs one (QNTX `server/auth/signed.go`).
//!
//! "A signature proves the key, and nothing reusable leaves the node."
//!
//! Unix only, as QNTX is (ADR-029).

use std::path::{Path, PathBuf};

use ats::Attestation;
use ats_sign::{didkey, Signable};
use libp2p_identity::ed25519;
use sacred_error::Error;

use crate::fail;

pub struct Signer {
    key: ed25519::Keypair,
    pub did: String,
}

/// Where the key is kept: `--key`, else `$ATS_DEV_KEY`, else `~/.qntx/key`.
pub fn key_path(flag: Option<&str>) -> Result<PathBuf, Box<Error>> {
    if let Some(f) = flag {
        return Ok(PathBuf::from(f));
    }
    if let Ok(e) = std::env::var("ATS_DEV_KEY") {
        return Ok(PathBuf::from(e));
    }
    let home = std::env::var("HOME")
        .map_err(|e| fail("the home directory is unknown, so ~/.qntx/key is not found", e))?;
    Ok(PathBuf::from(home).join(".qntx").join("key"))
}

impl Signer {
    /// The key at `path`. None kept there is an error: ats-dev does not mint one.
    pub fn load(path: &Path) -> Result<Self, Box<Error>> {
        Self::load_existing(path)?.ok_or_else(|| {
            fail(
                &format!("no key is kept at {} to sign with", path.display()),
                "ground code login keeps one; or --key FILE, or --unsigned",
            )
        })
    }

    /// The key at `path` if one is kept there.
    pub fn load_existing(path: &Path) -> Result<Option<Self>, Box<Error>> {
        read_key(path)?.map(|bytes| Self::from_file(path, &bytes)).transpose()
    }

    fn from_file(path: &Path, bytes: &[u8]) -> Result<Self, Box<Error>> {
        let not_seed = || fail("the key file holds no 32-byte ed25519 seed in hex", path.display());
        let text = std::str::from_utf8(bytes).map_err(|_e| not_seed())?.trim();
        let seed = hex_decode(text).ok_or_else(not_seed)?;
        if seed.len() != 32 {
            return Err(not_seed());
        }
        Self::from_seed(seed)
    }

    pub fn from_seed(seed: Vec<u8>) -> Result<Self, Box<Error>> {
        let secret = ed25519::SecretKey::try_from_bytes(seed).map_err(|e| fail("the seed is not an ed25519 key", e))?;
        let key = ed25519::Keypair::from(secret);
        let did = didkey::encode(&key.public().to_bytes());
        Ok(Self { key, did })
    }

    /// The attestation, signed over `ats-sign`'s canonical bytes. The actors
    /// and the timestamp are under the signature, so they must be settled
    /// before this is called (see `ready_to_sign` in main.rs).
    pub fn sign(&self, mut a: Attestation) -> Result<Attestation, Box<Error>> {
        let bytes = canonical(&a)?;
        a.signature = Some(self.key.sign(bytes.as_bytes()));
        a.signer_did = Some(self.did.clone());
        Ok(a)
    }

    /// The `Authorization` one request to the node at `origin` goes with: a
    /// JWT this key signs as its did:key, made out to that origin, lasting
    /// `lasts` seconds and spent once there (`qntx sign` makes the same).
    pub fn authorization(&self, origin: &str, lasts: i64) -> Result<String, Box<Error>> {
        use base64::Engine;
        let b64 = base64::engine::general_purpose::URL_SAFE_NO_PAD;
        let mut jti = [0u8; 16];
        std::fs::File::open("/dev/urandom")
            .and_then(|mut f| std::io::Read::read_exact(&mut f, &mut jti))
            .map_err(|e| fail("no id could be drawn for the signature", e))?;
        let now = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map_err(|e| fail("the clock is before 1970", e))?
            .as_secs();
        let exp = i64::try_from(now).map_err(|e| fail("the clock does not fit", e))? + lasts;
        let header = serde_json::json!({ "alg": "EdDSA", "typ": "JWT" });
        let claims = serde_json::json!({
            "iss": self.did, "sub": self.did, "aud": [origin], "exp": exp,
            "jti": jti.iter().map(|b| format!("{b:02x}")).collect::<String>(),
        });
        let signing_input = format!("{}.{}", b64.encode(header.to_string()), b64.encode(claims.to_string()));
        let sig = self.key.sign(signing_input.as_bytes());
        Ok(format!("Signed {signing_input}.{}", b64.encode(sig)))
    }
}

fn hex_decode(s: &str) -> Option<Vec<u8>> {
    if !s.len().is_multiple_of(2) || !s.is_ascii() {
        return None;
    }
    s.as_bytes()
        .chunks(2)
        .map(|p| std::str::from_utf8(p).ok().and_then(|h| u8::from_str_radix(h, 16).ok()))
        .collect()
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
                Ok(ed.verify(bytes.as_bytes(), sig))
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

/// The bytes of the key file, `None` if there is none. A key that group or
/// other can reach is refused, as ssh refuses one: whoever can read it can sign
/// as this identity.
fn read_key(path: &Path) -> Result<Option<Vec<u8>>, Box<Error>> {
    use std::io::Read;
    use std::os::unix::fs::PermissionsExt;
    let unreadable = |e: std::io::Error| fail("could not read the key file", format!("{}: {e}", path.display()));
    // One open, then the checks and the read through that handle: the file
    // that was checked is the file that is read, whatever happens to the path.
    let mut file = match std::fs::File::open(path) {
        Ok(f) => f,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(None),
        Err(e) => return Err(unreadable(e)),
    };
    let meta = file.metadata().map_err(unreadable)?;
    if !meta.is_file() {
        return Err(fail("the key file is not a regular file", path.display()));
    }
    let mode = meta.permissions().mode() & 0o777;
    if mode & 0o077 != 0 {
        return Err(fail(
            "the key file is open to other users",
            format!("{} is mode {mode:03o}; chmod 600 {}", path.display(), path.display()),
        ));
    }
    let mut bytes = Vec::new();
    file.read_to_end(&mut bytes).map_err(unreadable)?;
    Ok(Some(bytes))
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used, clippy::indexing_slicing, clippy::string_slice)]
mod tests {
    use super::*;
    use base64::Engine;

    fn hex(s: &str) -> Vec<u8> {
        hex_decode(s).unwrap()
    }

    /// Go's vectors: the same seed, the same attestations, the signatures
    /// `ats/signing` made. ed25519 is deterministic, so ats-dev's must be equal.
    #[test]
    fn it_signs_what_go_signed_byte_for_byte() {
        let sigs: serde_json::Value = serde_json::from_str(include_str!("../../../ats/signing/testdata/signatures.json")).unwrap();
        let cases: serde_json::Value = serde_json::from_str(include_str!("../../../ats/signing/testdata/canonical.json")).unwrap();
        let signer = Signer::from_seed(hex(sigs["seed_hex"].as_str().unwrap())).unwrap();

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
        let signer = Signer::from_seed(vec![7; 32]).unwrap();
        let mut a = Attestation { id: "AS-1".into(), subjects: vec!["a".into()], actors: vec!["x".into()], ..Attestation::default() };
        a = signer.sign(a).unwrap();
        assert_eq!(verify(&a), Verdict::Valid(signer.did.clone()));
        a.subjects = vec!["b".into()];
        assert!(matches!(verify(&a), Verdict::Invalid(_)));
    }

    /// The request JWT: EdDSA, issued by and about the key's did:key, made out
    /// to the origin, a 16-byte jti in hex, and signed by the key the DID names.
    #[test]
    fn a_request_is_signed_as_qntx_sign_signs_one() {
        let signer = Signer::from_seed(vec![9; 32]).unwrap();
        let auth = signer.authorization("https://node.example", 60).unwrap();
        let jwt = auth.strip_prefix("Signed ").unwrap();
        let parts: Vec<&str> = jwt.split('.').collect();
        assert_eq!(parts.len(), 3);
        let b64 = base64::engine::general_purpose::URL_SAFE_NO_PAD;
        let header: serde_json::Value = serde_json::from_slice(&b64.decode(parts[0]).unwrap()).unwrap();
        let claims: serde_json::Value = serde_json::from_slice(&b64.decode(parts[1]).unwrap()).unwrap();
        assert_eq!(header["alg"], "EdDSA");
        assert_eq!(claims["iss"], signer.did.as_str());
        assert_eq!(claims["sub"], signer.did.as_str());
        assert_eq!(claims["aud"], serde_json::json!(["https://node.example"]));
        assert_eq!(claims["jti"].as_str().unwrap().len(), 32);
        let pk = ed25519::PublicKey::try_from_bytes(&didkey::decode(&signer.did).unwrap()).unwrap();
        let input = format!("{}.{}", parts[0], parts[1]);
        assert!(pk.verify(input.as_bytes(), &b64.decode(parts[2]).unwrap()));
        // a second is a different jti: each request is its own
        assert_ne!(auth, signer.authorization("https://node.example", 60).unwrap());
    }
}
