//! Helper for the atproto OAuth client key. Two modes:
//!
//! - `--generate`: mint a fresh ES256 (P-256) keypair, print base64
//!   PKCS8 DER of the private (paste into `TF_VAR_relaye_atproto_
//!   client_key_b64`) and the corresponding JWKS JSON (paste into
//!   `broker/jwks.json`).
//! - `--jwks`: read the private key from env
//!   `RELAYE_ATPROTO_CLIENT_KEY_BYTES` (base64 PKCS8 DER) and print
//!   only the JWKS. Used to regenerate `broker/jwks.json` after key
//!   rotation without minting a new key.
//!
//! Both modes take an optional `--kid <str>` (default `laye-relaye-1`).

use base64::Engine;
use p256::ecdsa::SigningKey;
use p256::pkcs8::{DecodePrivateKey, EncodePrivateKey};

fn main() {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let mut mode: Option<&str> = None;
    let mut kid: String = "laye-relaye-1".to_string();
    let mut i = 0;
    while i < args.len() {
        match args[i].as_str() {
            "--generate" => mode = Some("generate"),
            "--jwks" => mode = Some("jwks"),
            "--kid" => {
                i += 1;
                if let Some(k) = args.get(i) {
                    kid = k.clone();
                }
            }
            other => {
                eprintln!("unknown arg: {other}");
                std::process::exit(2);
            }
        }
        i += 1;
    }
    match mode {
        Some("generate") => cmd_generate(&kid),
        Some("jwks") => cmd_jwks(&kid),
        _ => {
            eprintln!("usage: relaye-jwk (--generate | --jwks) [--kid <str>]");
            std::process::exit(2);
        }
    }
}

fn cmd_generate(kid: &str) {
    let key = SigningKey::random(&mut rand::thread_rng());
    let pkcs8 = key
        .to_pkcs8_der()
        .unwrap_or_else(|e| die(&format!("pkcs8 encode: {e}")));
    let b64 = base64::engine::general_purpose::STANDARD.encode(pkcs8.as_bytes());
    println!("# TF_VAR_relaye_atproto_client_key_b64 — paste into shell env before `tofu apply`");
    println!("export TF_VAR_relaye_atproto_client_key_b64={b64}");
    println!();
    println!("# broker/jwks.json — commit this alongside the deploy");
    println!("{}", jwks_json(&key, kid));
}

fn cmd_jwks(kid: &str) {
    let b64 = std::env::var("RELAYE_ATPROTO_CLIENT_KEY_BYTES").unwrap_or_else(|_| {
        die("RELAYE_ATPROTO_CLIENT_KEY_BYTES not set");
    });
    let bytes = base64::engine::general_purpose::STANDARD
        .decode(b64.trim())
        .unwrap_or_else(|e| die(&format!("base64 decode: {e}")));
    let key = SigningKey::from_pkcs8_der(&bytes)
        .unwrap_or_else(|e| die(&format!("pkcs8 decode: {e}")));
    println!("{}", jwks_json(&key, kid));
}

fn jwks_json(key: &SigningKey, kid: &str) -> String {
    let verifying = *key.verifying_key();
    let point = verifying.to_encoded_point(false);
    let Some(x) = point.x() else {
        die("public key encoding missing x coordinate")
    };
    let Some(y) = point.y() else {
        die("public key encoding missing y coordinate")
    };
    let x_b64 = base64::engine::general_purpose::URL_SAFE_NO_PAD.encode(x);
    let y_b64 = base64::engine::general_purpose::URL_SAFE_NO_PAD.encode(y);
    let jwks = serde_json::json!({
        "keys": [{
            "kty": "EC",
            "crv": "P-256",
            "x": x_b64,
            "y": y_b64,
            "kid": kid,
            "use": "sig",
            "alg": "ES256",
        }]
    });
    serde_json::to_string_pretty(&jwks).unwrap_or_else(|e| die(&format!("jwks json: {e}")))
}

fn die(msg: &str) -> ! {
    eprintln!("relaye-jwk: {msg}");
    std::process::exit(1);
}
