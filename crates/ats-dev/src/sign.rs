//! ats-dev's own signing identity: an ed25519 key it mints on first use and
//! keeps in a file beside the database. The bytes signed are `ats-sign`'s, the
//! ones Go's `ats/signing` signs and verifies; the signer is named by its
//! `did:key`, as the node names itself. This is not the node's key and its DID
//! is not in the node's DID document. Unix only, as QNTX is (ADR-029).

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
    /// A file that exists and is not a key is an error, never replaced. Two
    /// processes minting at once end with one key: the loser loads the winner's.
    pub fn load_or_mint(path: &Path) -> Result<Self, Box<Error>> {
        match read_key(path) {
            Ok(Some(bytes)) => Self::from_file(path, &bytes),
            Ok(None) => {
                let k = laye_me::fresh();
                let bytes = laye_me::to_bytes(&k).map_err(|e| fail("could not encode the key", e))?;
                if publish_new(path, &bytes)? {
                    return Self::from_keypair(k);
                }
                let bytes = read_key(path)?
                    .ok_or_else(|| fail("the key file vanished", path.display()))?;
                Self::from_file(path, &bytes)
            }
            Err(e) => Err(e),
        }
    }

    /// The key at `path` if there is one; none is not minted.
    pub fn load_existing(path: &Path) -> Result<Option<Self>, Box<Error>> {
        read_key(path)?.map(|bytes| Self::from_file(path, &bytes)).transpose()
    }

    fn from_file(path: &Path, bytes: &[u8]) -> Result<Self, Box<Error>> {
        let key = laye_me::load(bytes)
            .map_err(|e| fail("the key file is not a key", format!("{}: {e}", path.display())))?;
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

/// The bytes of the key file, `None` if there is none. A key that group or
/// other can reach is refused, as ssh refuses one: whoever can read it can sign
/// as this identity.
fn read_key(path: &Path) -> Result<Option<Vec<u8>>, Box<Error>> {
    use std::os::unix::fs::PermissionsExt;
    let meta = match std::fs::metadata(path) {
        Ok(m) => m,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(None),
        Err(e) => return Err(fail("could not read the key file", format!("{}: {e}", path.display()))),
    };
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
    std::fs::read(path)
        .map(Some)
        .map_err(|e| fail("could not read the key file", format!("{}: {e}", path.display())))
}

/// The key, written whole to a private temporary file beside `path` and then
/// linked into place, which fails if `path` exists: a reader never sees half a
/// key, and nothing is replaced. `false` means someone else's key is there.
/// The temporary name is this attempt's alone, and only a file this attempt
/// created is removed.
fn publish_new(path: &Path, bytes: &[u8]) -> Result<bool, Box<Error>> {
    use std::io::Write;
    use std::os::unix::fs::OpenOptionsExt;
    let nanos = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_nanos())
        .unwrap_or(0);
    let tmp = PathBuf::from(format!("{}.{}.{nanos}.tmp", path.display(), std::process::id()));
    let mut f = std::fs::OpenOptions::new()
        .write(true)
        .create_new(true)
        .mode(0o600)
        .open(&tmp)
        .map_err(|e| fail("could not create the key's temporary file", format!("{}: {e}", tmp.display())))?;
    let wrote = f.write_all(bytes).and_then(|()| f.sync_all());
    drop(f);
    let linked = match wrote {
        Ok(()) => std::fs::hard_link(&tmp, path),
        Err(e) => Err(e),
    };
    let cleaned = std::fs::remove_file(&tmp);
    match linked {
        Ok(()) => {
            cleaned.map_err(|e| fail("the key is written but its temporary copy is left", format!("{}: {e}", tmp.display())))?;
            sync_dir_of(path)?;
            Ok(true)
        }
        Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists => Ok(false),
        Err(e) => Err(fail("could not write the key file", format!("{}: {e}", path.display()))),
    }
}

/// Make the key's directory entry durable before anything is signed with it:
/// rows signed by a key that a power loss un-links could never be re-signed.
fn sync_dir_of(path: &Path) -> Result<(), Box<Error>> {
    let dir = match path.parent() {
        Some(d) if !d.as_os_str().is_empty() => d,
        _ => Path::new("."),
    };
    std::fs::File::open(dir)
        .and_then(|d| d.sync_all())
        .map_err(|e| fail("could not make the key durable", format!("{}: {e}", dir.display())))
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
