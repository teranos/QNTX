use laye_me::{BindingClaim, Keypair};
use serde::{Deserialize, Serialize};

#[derive(Deserialize)]
struct SignRequest {
    peer_pubkey_hex: String,
    provider: String,
    canonical_id: String,
    handle: Option<String>,
    mastodon_token: String,
    mastodon_instance: String,
}

#[derive(Serialize)]
struct SignResponse {
    claim: ClaimJson,
    signature_hex: String,
    signer_pubkey_hex: String,
}

#[derive(Serialize)]
struct ClaimJson {
    peer_pubkey_hex: String,
    provider: String,
    canonical_id: String,
    handle: Option<String>,
    issued_at: u64,
}

pub async fn handle_sign(body: &[u8], keypair: &Keypair) -> Result<Vec<u8>, SignError> {
    let req: SignRequest = serde_json::from_slice(body).map_err(SignError::BadRequestJson)?;

    if req.provider != "mastodon" {
        return Err(SignError::UnsupportedProvider(req.provider));
    }

    let peer_pubkey = decode_peer_pubkey(&req.peer_pubkey_hex)?;

    let actor_url = fetch_mastodon_actor_url(&req.mastodon_instance, &req.mastodon_token).await?;
    if actor_url != req.canonical_id {
        return Err(SignError::CanonicalIdMismatch {
            token_actor: actor_url,
            claim_actor: req.canonical_id,
        });
    }

    let issued_at = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0);

    let claim = BindingClaim {
        peer_pubkey,
        provider: req.provider.clone(),
        canonical_id: req.canonical_id.clone(),
        handle: req.handle.clone(),
        issued_at,
    };

    let canonical = claim.canonical_bytes();
    let signature = keypair
        .sign(&canonical)
        .map_err(|e| SignError::Sign(e.to_string()))?;

    let signer_pubkey = keypair
        .public()
        .try_into_ed25519()
        .map_err(|e| SignError::Sign(format!("signer pubkey not Ed25519: {e}")))?
        .to_bytes();

    let response = SignResponse {
        claim: ClaimJson {
            peer_pubkey_hex: hex::encode(peer_pubkey),
            provider: req.provider,
            canonical_id: req.canonical_id,
            handle: req.handle,
            issued_at,
        },
        signature_hex: hex::encode(&signature),
        signer_pubkey_hex: hex::encode(signer_pubkey),
    };

    serde_json::to_vec(&response).map_err(SignError::ResponseJson)
}

async fn fetch_mastodon_actor_url(instance: &str, token: &str) -> Result<String, SignError> {
    let url = format!("https://{instance}/api/v1/accounts/verify_credentials");
    let client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(10))
        .build()
        .map_err(|e| SignError::HttpClient(e.to_string()))?;
    let resp = client
        .get(&url)
        .bearer_auth(token)
        .send()
        .await
        .map_err(|e| SignError::MastodonRequest(e.to_string()))?;
    if !resp.status().is_success() {
        return Err(SignError::MastodonStatus(resp.status().as_u16()));
    }
    let actor: serde_json::Value = resp
        .json()
        .await
        .map_err(|e| SignError::MastodonJson(e.to_string()))?;
    actor
        .get("url")
        .and_then(|v| v.as_str())
        .map(|s| s.to_string())
        .ok_or(SignError::MastodonActorMissingUrl)
}

fn decode_peer_pubkey(hexstr: &str) -> Result<[u8; 32], SignError> {
    let bytes = hex::decode(hexstr).map_err(|_| SignError::BadPeerPubkeyHex)?;
    if bytes.len() != 32 {
        return Err(SignError::BadPeerPubkeyLen(bytes.len()));
    }
    let mut out = [0u8; 32];
    out.copy_from_slice(&bytes);
    Ok(out)
}

pub enum SignError {
    BadRequestJson(serde_json::Error),
    UnsupportedProvider(String),
    BadPeerPubkeyHex,
    BadPeerPubkeyLen(usize),
    CanonicalIdMismatch {
        token_actor: String,
        claim_actor: String,
    },
    HttpClient(String),
    MastodonRequest(String),
    MastodonStatus(u16),
    MastodonJson(String),
    MastodonActorMissingUrl,
    Sign(String),
    ResponseJson(serde_json::Error),
}

impl SignError {
    pub fn http_status(&self) -> u16 {
        match self {
            SignError::BadRequestJson(_)
            | SignError::UnsupportedProvider(_)
            | SignError::BadPeerPubkeyHex
            | SignError::BadPeerPubkeyLen(_) => 400,
            SignError::CanonicalIdMismatch { .. } | SignError::MastodonStatus(_) => 401,
            SignError::MastodonRequest(_)
            | SignError::MastodonJson(_)
            | SignError::MastodonActorMissingUrl => 502,
            SignError::HttpClient(_) | SignError::Sign(_) | SignError::ResponseJson(_) => 500,
        }
    }

    pub fn message(&self) -> String {
        match self {
            SignError::BadRequestJson(e) => format!("bad request JSON: {e}"),
            SignError::UnsupportedProvider(p) => format!("unsupported provider: {p}"),
            SignError::BadPeerPubkeyHex => "peer_pubkey_hex not valid hex".to_string(),
            SignError::BadPeerPubkeyLen(n) => {
                format!("peer_pubkey_hex must decode to 32 bytes, got {n}")
            }
            SignError::CanonicalIdMismatch {
                token_actor,
                claim_actor,
            } => format!(
                "canonical_id mismatch: token belongs to {token_actor}, claim says {claim_actor}"
            ),
            SignError::HttpClient(e) => format!("http client init: {e}"),
            SignError::MastodonRequest(e) => format!("mastodon verify_credentials: {e}"),
            SignError::MastodonStatus(s) => format!("mastodon verify_credentials: HTTP {s}"),
            SignError::MastodonJson(e) => format!("mastodon verify_credentials JSON: {e}"),
            SignError::MastodonActorMissingUrl => "mastodon actor missing url field".to_string(),
            SignError::Sign(e) => format!("sign: {e}"),
            SignError::ResponseJson(e) => format!("response JSON: {e}"),
        }
    }
}
