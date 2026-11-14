#![allow(
    clippy::unwrap_used,
    clippy::expect_used,
    clippy::indexing_slicing,
    clippy::string_slice
)]
//! What Rust makes is what Go makes: the did:key laye encodes, and the
//! signature laye's key makes over the bytes made here (ats/signing/testdata).

use ats_sign::{didkey, Signable};
use serde::Deserialize;

fn bytes(hex: &str) -> Vec<u8> {
    (0..hex.len())
        .step_by(2)
        .map(|i| u8::from_str_radix(&hex[i..i + 2], 16).unwrap())
        .collect()
}

#[derive(Deserialize)]
struct DidVector {
    public_key_hex: String,
    did: String,
}

fn did_vectors() -> Vec<DidVector> {
    serde_json::from_str(include_str!("../../../ats/signing/testdata/didkey.json")).unwrap()
}

#[test]
fn encodes_what_go_encodes() {
    for v in did_vectors() {
        let key: [u8; 32] = bytes(&v.public_key_hex).try_into().unwrap();
        assert_eq!(didkey::encode(&key), v.did);
    }
}

#[test]
fn decodes_what_go_encodes() {
    for v in did_vectors() {
        assert_eq!(
            didkey::decode(&v.did).unwrap().to_vec(),
            bytes(&v.public_key_hex)
        );
    }
}

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

/// QNTX makes the canonical bytes and laye's key signs them plainly: same
/// seed, same bytes, same signature as a Go node's key.
#[test]
fn signs_as_the_node_signs() {
    let vectors: Vec<Vector> =
        serde_json::from_str(include_str!("../../../ats/signing/testdata/canonical.json")).unwrap();
    let fixture: Signatures = serde_json::from_str(include_str!(
        "../../../ats/signing/testdata/signatures.json"
    ))
    .unwrap();
    let mut seed = bytes(&fixture.seed_hex);
    let secret = libp2p_identity::ed25519::SecretKey::try_from_bytes(&mut seed).unwrap();
    let keypair = laye_me::Keypair::from(libp2p_identity::ed25519::Keypair::from(secret));

    for v in vectors {
        let want = fixture
            .signatures
            .iter()
            .find(|s| s.name == v.name)
            .unwrap();
        let canonical = Signable::from_wire_json(&v.wire.to_string())
            .unwrap()
            .canonical_json()
            .unwrap();
        let got = keypair.sign(canonical.as_bytes()).unwrap();
        assert_eq!(got, bytes(&want.signature_hex), "{}", v.name);
    }
}
