//! atproto OAuth 2.1 client for the laye identity broker.
//!
//! Relay is the OAuth client. Broker page is UI only; every crypto step
//! (client_assertion signing, DPoP proof signing, session verification)
//! happens here. Wire is byte-locked to `atproto` OAuth profile:
//! `token_endpoint_auth_method = "private_key_jwt"`, ES256 everywhere,
//! `dpop_bound_access_tokens = true`, PAR mandatory.
//!
//! Two ES256 keys, distinct roles:
//! - Client auth key: long-lived, loaded from Secrets Manager, public
//!   half committed as `broker/jwks.json`. Signs `client_assertion` on
//!   PAR + token requests.
//! - DPoP key: ephemeral per flow, in-memory only, discarded when the
//!   flow ends or expires. Signs DPoP proofs.
//!
//! Flow state (start → callback → result) lives in `FlowCache`, keyed
//! by the OAuth `state` param, TTL 10 min per spec recommendation.
//! Signed results are stored in the same cache under the same key
//! until the broker fetches them, then wiped.

use p256::ecdsa::signature::Signer;
use p256::ecdsa::{Signature as P256Signature, SigningKey, VerifyingKey};
use rand::RngCore;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};

// ============================================================================
// base64url (no padding) — atproto spec + JWT convention
// ============================================================================

pub fn b64url_encode(bytes: &[u8]) -> String {
    use base64::Engine;
    base64::engine::general_purpose::URL_SAFE_NO_PAD.encode(bytes)
}

#[allow(dead_code)] // used by module tests + future consumers of the public API
pub fn b64url_decode(s: &str) -> Result<Vec<u8>, base64::DecodeError> {
    use base64::Engine;
    base64::engine::general_purpose::URL_SAFE_NO_PAD.decode(s)
}

// ============================================================================
// PKCE (S256)
// ============================================================================

/// Generates a 96-byte random verifier, base64url-encoded (128 chars).
/// Spec allows 43-128 chars; we pick the maximum for entropy.
pub fn pkce_verifier() -> String {
    let mut bytes = [0u8; 96];
    rand::thread_rng().fill_bytes(&mut bytes);
    b64url_encode(&bytes)
}

pub fn pkce_challenge(verifier: &str) -> String {
    let digest = Sha256::digest(verifier.as_bytes());
    b64url_encode(&digest)
}

// ============================================================================
// ES256 JWT signing (hand-rolled — one dep, one signing path)
// ============================================================================

pub fn sign_es256_jwt(
    header: &serde_json::Value,
    claims: &serde_json::Value,
    key: &SigningKey,
) -> Result<String, JwtError> {
    let header_json = serde_json::to_vec(header).map_err(JwtError::HeaderJson)?;
    let claims_json = serde_json::to_vec(claims).map_err(JwtError::ClaimsJson)?;
    let header_b64 = b64url_encode(&header_json);
    let claims_b64 = b64url_encode(&claims_json);
    let signing_input = format!("{header_b64}.{claims_b64}");
    let sig: P256Signature = key.sign(signing_input.as_bytes());
    let sig_bytes = sig.to_bytes();
    let sig_b64 = b64url_encode(&sig_bytes);
    Ok(format!("{signing_input}.{sig_b64}"))
}

#[derive(Debug, thiserror::Error)]
pub enum JwtError {
    #[error("header serialization: {0}")]
    HeaderJson(serde_json::Error),
    #[error("claims serialization: {0}")]
    ClaimsJson(serde_json::Error),
}

// ============================================================================
// Random `jti` (unique per JWT, spec requires uniqueness for DPoP)
// ============================================================================

pub fn random_jti() -> String {
    let mut bytes = [0u8; 16];
    rand::thread_rng().fill_bytes(&mut bytes);
    b64url_encode(&bytes)
}

pub fn now_unix_secs() -> u64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0)
}

// ============================================================================
// DPoP proof JWT
//
// Spec: `typ = "dpop+jwt"`, `alg = "ES256"`, `jwk = <public JWK of DPoP key>`.
// Claims: `jti`, `htm` (HTTP method upper), `htu` (full URL without query),
// `iat`, optional `nonce` (only after server sends one), optional `ath`
// (only for PDS resource requests: b64url(sha256(access_token))).
// Do NOT include `iss` per spec note for PDS endpoint requests; we also
// omit it for auth-server requests since it isn't required there.
// ============================================================================

pub fn dpop_proof(
    dpop_key: &SigningKey,
    method: &str,
    url: &str,
    nonce: Option<&str>,
    access_token: Option<&str>,
) -> Result<String, JwtError> {
    let jwk = public_jwk(dpop_key, "", true);
    let header = serde_json::json!({
        "typ": "dpop+jwt",
        "alg": "ES256",
        "jwk": {
            "kty": jwk.kty,
            "crv": jwk.crv,
            "x": jwk.x,
            "y": jwk.y,
        },
    });
    let mut claims = serde_json::Map::new();
    claims.insert("jti".into(), serde_json::Value::String(random_jti()));
    claims.insert("htm".into(), serde_json::Value::String(method.to_uppercase()));
    claims.insert("htu".into(), serde_json::Value::String(url.to_string()));
    claims.insert(
        "iat".into(),
        serde_json::Value::Number(now_unix_secs().into()),
    );
    if let Some(n) = nonce {
        claims.insert("nonce".into(), serde_json::Value::String(n.to_string()));
    }
    if let Some(tok) = access_token {
        let digest = Sha256::digest(tok.as_bytes());
        claims.insert(
            "ath".into(),
            serde_json::Value::String(b64url_encode(&digest)),
        );
    }
    sign_es256_jwt(&header, &serde_json::Value::Object(claims), dpop_key)
}

// ============================================================================
// client_assertion JWT (private_key_jwt for token endpoint auth)
//
// Spec (atproto profile): typ=JWT, alg=ES256, kid=<client_key_kid>
// Claims: iss=client_id, sub=client_id, aud=<auth_server_issuer>,
//         jti, iat, exp (short-lived recommended)
// ============================================================================

pub fn client_assertion(
    client_key: &SigningKey,
    kid: &str,
    client_id: &str,
    audience: &str,
    now: u64,
) -> Result<String, JwtError> {
    let header = serde_json::json!({
        "typ": "JWT",
        "alg": "ES256",
        "kid": kid,
    });
    let claims = serde_json::json!({
        "iss": client_id,
        "sub": client_id,
        "aud": audience,
        "jti": random_jti(),
        "iat": now,
        "exp": now + 60,
    });
    sign_es256_jwt(&header, &claims, client_key)
}

// ============================================================================
// Resolution: handle → DID → DID document → PDS → auth server metadata
//
// Spec: "Critical (mandatory) to bidirectionally verify the handle" —
// after resolving handle→DID we must fetch the DID document and confirm
// its `alsoKnownAs` claims the handle back. We use DoH (JSON API, no
// new dep) for the DNS TXT method; HTTPS well-known is the fallback.
// ============================================================================

#[derive(Debug, thiserror::Error)]
pub enum ResolveError {
    #[error("handle format rejected: {0}")]
    BadHandle(String),
    #[error("handle DNS TXT + well-known both failed: dns={dns} https={https}")]
    HandleNotResolved { dns: String, https: String },
    #[error("did format rejected: {0}")]
    BadDid(String),
    #[error("did document fetch: {0}")]
    DidDocFetch(String),
    #[error("did document does not claim this handle: did={did} handle={handle}")]
    HandleNotClaimedByDid { did: String, handle: String },
    #[error("did document has no atproto PDS service")]
    NoPdsService,
    #[error("pds metadata fetch: {0}")]
    PdsMetaFetch(String),
    #[error("pds metadata has no authorization_servers entry")]
    NoAuthServer,
    #[error("auth server metadata fetch: {0}")]
    AuthMetaFetch(String),
    #[error("auth server issuer mismatch: expected={expected} got={got}")]
    IssuerMismatch { expected: String, got: String },
    #[error("HTTP client: {0}")]
    Http(String),
}

const DOH_ENDPOINT: &str = "https://cloudflare-dns.com/dns-query";
const PLC_DIRECTORY: &str = "https://plc.directory";
const HTTP_TIMEOUT: std::time::Duration = std::time::Duration::from_secs(10);

fn http_client() -> Result<reqwest::Client, ResolveError> {
    reqwest::Client::builder()
        .timeout(HTTP_TIMEOUT)
        .build()
        .map_err(|e| ResolveError::Http(e.to_string()))
}

/// Handle input hygiene: lowercase, strip leading `@`, reject empty.
pub fn normalize_handle(raw: &str) -> Result<String, ResolveError> {
    let trimmed = raw.trim().trim_start_matches('@').to_ascii_lowercase();
    if trimmed.is_empty() || !trimmed.contains('.') {
        return Err(ResolveError::BadHandle(raw.to_string()));
    }
    Ok(trimmed)
}

pub async fn resolve_handle_to_did(handle: &str) -> Result<String, ResolveError> {
    let handle = normalize_handle(handle)?;
    let dns_err = match resolve_handle_via_doh(&handle).await {
        Ok(did) => return Ok(did),
        Err(e) => e,
    };
    let https_err = match resolve_handle_via_well_known(&handle).await {
        Ok(did) => return Ok(did),
        Err(e) => e,
    };
    Err(ResolveError::HandleNotResolved {
        dns: dns_err,
        https: https_err,
    })
}

async fn resolve_handle_via_doh(handle: &str) -> Result<String, String> {
    let client = http_client().map_err(|e| e.to_string())?;
    let name = format!("_atproto.{handle}");
    let resp = client
        .get(DOH_ENDPOINT)
        .query(&[("name", name.as_str()), ("type", "TXT")])
        .header("Accept", "application/dns-json")
        .send()
        .await
        .map_err(|e| format!("doh get: {e}"))?;
    if !resp.status().is_success() {
        return Err(format!("doh http {}", resp.status()));
    }
    let body: serde_json::Value = resp
        .json()
        .await
        .map_err(|e| format!("doh json: {e}"))?;
    let answers = body
        .get("Answer")
        .and_then(|a| a.as_array())
        .ok_or_else(|| "doh no Answer array".to_string())?;
    for a in answers {
        let data = a.get("data").and_then(|d| d.as_str()).unwrap_or("");
        // DoH JSON strings TXT with surrounding quotes; may also be split
        // into multiple chunks joined by quote-space-quote.
        let stripped = data.trim().trim_matches('"');
        if let Some(did) = stripped.strip_prefix("did=")
            && is_valid_did(did)
        {
            return Ok(did.to_string());
        }
    }
    Err("doh no did= TXT entry".into())
}

async fn resolve_handle_via_well_known(handle: &str) -> Result<String, String> {
    let client = http_client().map_err(|e| e.to_string())?;
    let url = format!("https://{handle}/.well-known/atproto-did");
    let resp = client
        .get(&url)
        .send()
        .await
        .map_err(|e| format!("well-known get: {e}"))?;
    if !resp.status().is_success() {
        return Err(format!("well-known http {}", resp.status()));
    }
    let body = resp
        .text()
        .await
        .map_err(|e| format!("well-known body: {e}"))?;
    let did = body.trim();
    if !is_valid_did(did) {
        return Err(format!("well-known body not a did: {}", &did[..did.len().min(32)]));
    }
    Ok(did.to_string())
}

pub fn is_valid_did(s: &str) -> bool {
    (s.starts_with("did:plc:") && s.len() > "did:plc:".len())
        || (s.starts_with("did:web:") && s.len() > "did:web:".len())
}

#[derive(Debug, Clone, Deserialize)]
pub struct DidDocument {
    #[serde(default)]
    pub id: String,
    #[serde(rename = "alsoKnownAs", default)]
    pub also_known_as: Vec<String>,
    #[serde(default)]
    pub service: Vec<DidService>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct DidService {
    // Captured for serde round-trip; not consulted directly. Some
    // atproto tooling ships `#atproto_pds` in id, some ships other
    // labels — we match on `type` instead.
    #[serde(default)]
    #[allow(dead_code)]
    pub id: String,
    #[serde(rename = "type", default)]
    pub type_: String,
    #[serde(rename = "serviceEndpoint", default)]
    pub service_endpoint: String,
}

pub async fn fetch_did_document(did: &str) -> Result<DidDocument, ResolveError> {
    if !is_valid_did(did) {
        return Err(ResolveError::BadDid(did.to_string()));
    }
    let client = http_client()?;
    let url = if let Some(rest) = did.strip_prefix("did:plc:") {
        format!("{PLC_DIRECTORY}/did:plc:{rest}")
    } else if let Some(hostname) = did.strip_prefix("did:web:") {
        // did:web:example.com → https://example.com/.well-known/did.json
        // did:web:example.com:user:alice → https://example.com/user/alice/did.json
        let (host, path) = match hostname.split_once(':') {
            Some((h, rest)) => (h, format!("/{}/did.json", rest.replace(':', "/"))),
            None => (hostname, "/.well-known/did.json".to_string()),
        };
        format!("https://{host}{path}")
    } else {
        return Err(ResolveError::BadDid(did.to_string()));
    };
    let resp = client
        .get(&url)
        .send()
        .await
        .map_err(|e| ResolveError::DidDocFetch(format!("get {url}: {e}")))?;
    if !resp.status().is_success() {
        return Err(ResolveError::DidDocFetch(format!(
            "{url} http {}",
            resp.status()
        )));
    }
    resp.json::<DidDocument>()
        .await
        .map_err(|e| ResolveError::DidDocFetch(format!("json {url}: {e}")))
}

pub fn verify_handle_claim(handle: &str, did_doc: &DidDocument) -> Result<(), ResolveError> {
    let want = format!("at://{handle}");
    if did_doc.also_known_as.iter().any(|a| a == &want) {
        Ok(())
    } else {
        Err(ResolveError::HandleNotClaimedByDid {
            did: did_doc.id.clone(),
            handle: handle.to_string(),
        })
    }
}

pub fn extract_pds_url(did_doc: &DidDocument) -> Result<String, ResolveError> {
    did_doc
        .service
        .iter()
        .find(|s| s.type_ == "AtprotoPersonalDataServer")
        .map(|s| s.service_endpoint.trim_end_matches('/').to_string())
        .ok_or(ResolveError::NoPdsService)
}

#[derive(Debug, Clone, Deserialize)]
pub struct PdsProtectedResource {
    #[serde(default)]
    pub authorization_servers: Vec<String>,
}

pub async fn fetch_pds_metadata(pds_url: &str) -> Result<String, ResolveError> {
    let client = http_client()?;
    let url = format!("{pds_url}/.well-known/oauth-protected-resource");
    let resp = client
        .get(&url)
        .send()
        .await
        .map_err(|e| ResolveError::PdsMetaFetch(format!("get {url}: {e}")))?;
    if !resp.status().is_success() {
        return Err(ResolveError::PdsMetaFetch(format!(
            "{url} http {}",
            resp.status()
        )));
    }
    let meta: PdsProtectedResource = resp
        .json()
        .await
        .map_err(|e| ResolveError::PdsMetaFetch(format!("json {url}: {e}")))?;
    meta.authorization_servers
        .into_iter()
        .next()
        .ok_or(ResolveError::NoAuthServer)
}

#[derive(Debug, Clone, Deserialize)]
pub struct AuthServerMetadata {
    pub issuer: String,
    pub authorization_endpoint: String,
    pub token_endpoint: String,
    pub pushed_authorization_request_endpoint: String,
}

pub async fn fetch_auth_server_metadata(
    auth_server_url: &str,
) -> Result<AuthServerMetadata, ResolveError> {
    let client = http_client()?;
    let url = format!(
        "{}/.well-known/oauth-authorization-server",
        auth_server_url.trim_end_matches('/')
    );
    let resp = client
        .get(&url)
        .send()
        .await
        .map_err(|e| ResolveError::AuthMetaFetch(format!("get {url}: {e}")))?;
    if !resp.status().is_success() {
        return Err(ResolveError::AuthMetaFetch(format!(
            "{url} http {}",
            resp.status()
        )));
    }
    let meta: AuthServerMetadata = resp
        .json()
        .await
        .map_err(|e| ResolveError::AuthMetaFetch(format!("json {url}: {e}")))?;
    // Spec: verify issuer matches origin of fetch URL.
    let want_origin = auth_server_url.trim_end_matches('/');
    if meta.issuer.trim_end_matches('/') != want_origin {
        return Err(ResolveError::IssuerMismatch {
            expected: want_origin.to_string(),
            got: meta.issuer,
        });
    }
    Ok(meta)
}

// ============================================================================
// Flow state cache
//
// Two tables, both keyed by the OAuth `state` param:
// - `flows`: FlowState between `start` (POST) and `callback` (GET redirect
//   from PDS). Holds the ephemeral DPoP key, PKCE verifier, DID, PDS URL,
//   auth server metadata.
// - `results`: SignedBinding between `callback` completion and `result`
//   fetch by the broker page. One-shot: reading drains the entry.
//
// TTL: 10 min per spec cache recommendation. Anything older is garbage.
// Cleanup runs on every insert (small: bounded by concurrent user flows).
// ============================================================================

const FLOW_TTL: std::time::Duration = std::time::Duration::from_secs(600);

pub struct FlowState {
    pub peer_pubkey_hex: String,
    pub handle: String,
    pub did: String,
    pub auth_meta: AuthServerMetadata,
    pub dpop_key: SigningKey,
    pub pkce_verifier: String,
    pub dpop_nonce_auth: Option<String>,
    pub created_at: std::time::Instant,
}

pub struct ResultState {
    pub signed: laye_me::SignedBinding,
    pub created_at: std::time::Instant,
}

#[derive(Clone)]
pub struct FlowCache {
    inner: std::sync::Arc<std::sync::Mutex<FlowCacheInner>>,
}

struct FlowCacheInner {
    flows: std::collections::HashMap<String, FlowState>,
    results: std::collections::HashMap<String, ResultState>,
}

impl FlowCache {
    pub fn new() -> Self {
        Self {
            inner: std::sync::Arc::new(std::sync::Mutex::new(FlowCacheInner {
                flows: std::collections::HashMap::new(),
                results: std::collections::HashMap::new(),
            })),
        }
    }

    pub fn insert_flow(&self, state: String, flow: FlowState) {
        let mut inner = self.inner.lock().unwrap_or_else(|p| p.into_inner());
        gc(&mut inner);
        inner.flows.insert(state, flow);
    }

    pub fn take_flow(&self, state: &str) -> Option<FlowState> {
        let mut inner = self.inner.lock().unwrap_or_else(|p| p.into_inner());
        gc(&mut inner);
        inner.flows.remove(state)
    }

    pub fn insert_result(&self, state: String, signed: laye_me::SignedBinding) {
        let mut inner = self.inner.lock().unwrap_or_else(|p| p.into_inner());
        gc(&mut inner);
        inner.results.insert(
            state,
            ResultState {
                signed,
                created_at: std::time::Instant::now(),
            },
        );
    }

    /// Result reads are non-draining — both the popup (via broker JS) and
    /// the main tab (via laye-p2p wasm polling) need to see the same
    /// entry. TTL expiry (10 min) handles cleanup. Bindings are public
    /// signed data; leaving them in memory for the flow window is fine.
    pub fn get_result(&self, state: &str) -> Option<laye_me::SignedBinding> {
        let mut inner = self.inner.lock().unwrap_or_else(|p| p.into_inner());
        gc(&mut inner);
        inner.results.get(state).map(|r| r.signed.clone())
    }
}

impl Default for FlowCache {
    fn default() -> Self {
        Self::new()
    }
}

fn gc(inner: &mut FlowCacheInner) {
    let now = std::time::Instant::now();
    inner
        .flows
        .retain(|_, f| now.duration_since(f.created_at) < FLOW_TTL);
    inner
        .results
        .retain(|_, r| now.duration_since(r.created_at) < FLOW_TTL);
}

pub fn random_state() -> String {
    let mut bytes = [0u8; 32];
    rand::thread_rng().fill_bytes(&mut bytes);
    b64url_encode(&bytes)
}

// ============================================================================
// OAuth client config (carried into every flow handler)
// ============================================================================

pub struct ClientConfig {
    /// The client_id — always the URL of client_metadata.json.
    pub client_id: String,
    /// Redirect URI declared in client_metadata.json.
    pub redirect_uri: String,
    /// Long-lived server-held ES256 key that signs client_assertion JWTs.
    /// Public half published in `broker/jwks.json`.
    pub client_key: SigningKey,
    /// Kid for the client key, matches `broker/jwks.json` entry.
    pub client_kid: String,
}

// ============================================================================
// DPoP nonce retry helper
//
// atproto OAuth requires initial-request nonce discovery: first POST to
// each server endpoint (auth server + PDS) returns 401 with
// `use_dpop_nonce`. Extract nonce from `DPoP-Nonce` response header,
// retry with updated DPoP proof. We retry once — beyond that, real error.
// ============================================================================

pub struct DpopResponse {
    pub status: reqwest::StatusCode,
    pub body: String,
    pub new_nonce: Option<String>,
}

pub async fn post_form_with_dpop<F>(
    client: &reqwest::Client,
    url: &str,
    dpop_key: &SigningKey,
    initial_nonce: Option<String>,
    mut form_builder: F,
) -> Result<DpopResponse, FlowError>
where
    F: FnMut() -> Vec<(String, String)>,
{
    let mut nonce = initial_nonce;
    for attempt in 0..2 {
        let dpop = dpop_proof(dpop_key, "POST", url, nonce.as_deref(), None)
            .map_err(|e| FlowError::Jwt(e.to_string()))?;
        let form = form_builder();
        let resp = client
            .post(url)
            .header("DPoP", &dpop)
            .header("Content-Type", "application/x-www-form-urlencoded")
            .body(url_encode_form(&form))
            .send()
            .await
            .map_err(|e| FlowError::Http(format!("post {url}: {e}")))?;
        let status = resp.status();
        let new_nonce = resp
            .headers()
            .get("DPoP-Nonce")
            .and_then(|v| v.to_str().ok())
            .map(str::to_string);
        let body = resp
            .text()
            .await
            .map_err(|e| FlowError::Http(format!("body {url}: {e}")))?;
        // DPoP nonce challenge: RFC 9449 says the auth server returns 401
        // with WWW-Authenticate. RFC 9126 (PAR) leaves it ambiguous; bsky.social
        // returns 400 with `use_dpop_nonce` in the JSON body. Retry on either
        // status when the marker is present and a fresh nonce came back.
        if attempt == 0
            && !status.is_success()
            && let Some(n) = new_nonce.clone()
            && body.contains("use_dpop_nonce")
        {
            nonce = Some(n);
            continue;
        }
        return Ok(DpopResponse {
            status,
            body,
            new_nonce,
        });
    }
    Err(FlowError::DpopRetryExhausted)
}

fn url_encode_form(pairs: &[(String, String)]) -> String {
    let mut out = String::new();
    for (i, (k, v)) in pairs.iter().enumerate() {
        if i > 0 {
            out.push('&');
        }
        percent_encode_into(&mut out, k);
        out.push('=');
        percent_encode_into(&mut out, v);
    }
    out
}

fn percent_encode_into(out: &mut String, s: &str) {
    for b in s.bytes() {
        match b {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => {
                out.push(b as char);
            }
            _ => {
                out.push('%');
                out.push_str(&format!("{b:02X}"));
            }
        }
    }
}

#[derive(Debug, thiserror::Error)]
pub enum FlowError {
    #[error("bad request json: {0}")]
    BadRequestJson(String),
    #[error("bad peer_pubkey_hex")]
    BadPeerPubkey,
    #[error("resolve: {0}")]
    Resolve(#[from] ResolveError),
    #[error("jwt: {0}")]
    Jwt(String),
    #[error("http: {0}")]
    Http(String),
    #[error("dpop retry exhausted")]
    DpopRetryExhausted,
    #[error("PAR http {status}: {body}")]
    Par { status: u16, body: String },
    #[error("PAR response missing request_uri: {0}")]
    ParNoRequestUri(String),
    #[error("token http {status}: {body}")]
    Token { status: u16, body: String },
    #[error("token response missing field: {0}")]
    TokenMissing(String),
    #[error("sub mismatch: expected={expected} got={got}")]
    SubMismatch { expected: String, got: String },
    #[error("unknown or expired state")]
    UnknownState,
    #[error("sign: {0}")]
    Sign(String),
}

impl FlowError {
    pub fn http_status(&self) -> u16 {
        match self {
            FlowError::BadRequestJson(_) | FlowError::BadPeerPubkey => 400,
            FlowError::UnknownState => 404,
            FlowError::Resolve(_) => 400,
            FlowError::SubMismatch { .. } => 401,
            FlowError::Par { .. }
            | FlowError::Token { .. }
            | FlowError::ParNoRequestUri(_)
            | FlowError::TokenMissing(_) => 502,
            FlowError::DpopRetryExhausted => 502,
            FlowError::Http(_) | FlowError::Jwt(_) | FlowError::Sign(_) => 500,
        }
    }
}

// ============================================================================
// Public wire types (broker ↔ relay)
// ============================================================================

#[derive(Debug, Deserialize)]
pub struct StartRequest {
    pub peer_pubkey_hex: String,
    pub handle: String,
    /// Main-tab-supplied state token. Both the popup and the main tab
    /// know it and can poll `/me/sign/atproto/result?state=…` against
    /// it — needed because bsky.social's COOP severs the popup's
    /// opener reference. Optional: broker JS falls back to server-
    /// generated state when the browser is old / URL param absent.
    #[serde(default)]
    pub main_state: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct StartResponse {
    pub state: String,
    pub authorize_url: String,
}

// ============================================================================
// start handler — resolve, PAR, stash state
// ============================================================================

pub async fn handle_start(
    body: &[u8],
    cfg: &ClientConfig,
    cache: &FlowCache,
) -> Result<Vec<u8>, FlowError> {
    let req: StartRequest = serde_json::from_slice(body).map_err(|e| {
        FlowError::BadRequestJson(format!("StartRequest parse: {e}"))
    })?;
    let peer_pubkey = decode_peer_pubkey(&req.peer_pubkey_hex)?;
    let handle = normalize_handle(&req.handle)?;

    let did = resolve_handle_to_did(&handle).await?;
    let did_doc = fetch_did_document(&did).await?;
    verify_handle_claim(&handle, &did_doc)?;
    let pds_url = extract_pds_url(&did_doc)?;
    let auth_server_url = fetch_pds_metadata(&pds_url).await?;
    let auth_meta = fetch_auth_server_metadata(&auth_server_url).await?;

    let dpop_key = SigningKey::random(&mut rand::thread_rng());
    let pkce_verifier = pkce_verifier();
    let pkce_challenge = pkce_challenge(&pkce_verifier);
    let state = req.main_state.clone().unwrap_or_else(random_state);

    let client = http_client().map_err(FlowError::from)?;
    let par_url = auth_meta.pushed_authorization_request_endpoint.clone();
    let cfg_client_id = cfg.client_id.clone();
    let cfg_redirect = cfg.redirect_uri.clone();
    let cfg_kid = cfg.client_kid.clone();
    let par_state = state.clone();
    let auth_issuer = auth_meta.issuer.clone();
    let login_hint = did.clone();

    let resp = post_form_with_dpop(
        &client,
        &par_url,
        &dpop_key,
        None,
        || {
            let now = now_unix_secs();
            // client_assertion regenerated each attempt because the outer
            // helper may retry after a DPoP nonce challenge.
            let assertion = client_assertion(
                &cfg.client_key,
                &cfg_kid,
                &cfg_client_id,
                &auth_issuer,
                now,
            )
            .unwrap_or_default();
            vec![
                ("client_id".into(), cfg_client_id.clone()),
                (
                    "client_assertion_type".into(),
                    "urn:ietf:params:oauth:client-assertion-type:jwt-bearer".into(),
                ),
                ("client_assertion".into(), assertion),
                ("response_type".into(), "code".into()),
                (
                    "scope".into(),
                    "atproto transition:generic".into(),
                ),
                ("redirect_uri".into(), cfg_redirect.clone()),
                ("state".into(), par_state.clone()),
                ("code_challenge".into(), pkce_challenge.clone()),
                ("code_challenge_method".into(), "S256".into()),
                ("login_hint".into(), login_hint.clone()),
            ]
        },
    )
    .await?;

    if !resp.status.is_success() {
        return Err(FlowError::Par {
            status: resp.status.as_u16(),
            body: resp.body,
        });
    }
    let par_body: serde_json::Value = serde_json::from_str(&resp.body)
        .map_err(|e| FlowError::ParNoRequestUri(format!("par body json: {e}")))?;
    let request_uri = par_body
        .get("request_uri")
        .and_then(|v| v.as_str())
        .ok_or_else(|| FlowError::ParNoRequestUri(resp.body.clone()))?
        .to_string();

    let mut authorize_url = auth_meta.authorization_endpoint.clone();
    authorize_url.push_str(if authorize_url.contains('?') { "&" } else { "?" });
    let mut qp = String::new();
    percent_encode_into(&mut qp, "client_id");
    qp.push('=');
    percent_encode_into(&mut qp, &cfg.client_id);
    qp.push('&');
    percent_encode_into(&mut qp, "request_uri");
    qp.push('=');
    percent_encode_into(&mut qp, &request_uri);
    authorize_url.push_str(&qp);

    cache.insert_flow(
        state.clone(),
        FlowState {
            peer_pubkey_hex: hex::encode(peer_pubkey),
            handle,
            did,
            auth_meta,
            dpop_key,
            pkce_verifier,
            dpop_nonce_auth: resp.new_nonce,
            created_at: std::time::Instant::now(),
        },
    );

    serde_json::to_vec(&StartResponse {
        state,
        authorize_url,
    })
    .map_err(|e| FlowError::Http(format!("StartResponse json: {e}")))
}

fn decode_peer_pubkey(hexstr: &str) -> Result<[u8; 32], FlowError> {
    let bytes = hex::decode(hexstr).map_err(|_| FlowError::BadPeerPubkey)?;
    if bytes.len() != 32 {
        return Err(FlowError::BadPeerPubkey);
    }
    let mut out = [0u8; 32];
    out.copy_from_slice(&bytes);
    Ok(out)
}

// ============================================================================
// callback handler — token exchange, verify, sign, stash result
// ============================================================================

pub async fn handle_callback(
    query: &std::collections::HashMap<String, String>,
    cfg: &ClientConfig,
    cache: &FlowCache,
    relay_signing_key: &laye_me::Keypair,
) -> Result<String, FlowError> {
    let code = query
        .get("code")
        .cloned()
        .ok_or_else(|| FlowError::BadRequestJson("missing ?code".into()))?;
    let state = query
        .get("state")
        .cloned()
        .ok_or_else(|| FlowError::BadRequestJson("missing ?state".into()))?;
    let flow = cache
        .take_flow(&state)
        .ok_or(FlowError::UnknownState)?;

    let client = http_client().map_err(FlowError::from)?;
    let token_url = flow.auth_meta.token_endpoint.clone();
    let issuer = flow.auth_meta.issuer.clone();
    let cfg_client_id = cfg.client_id.clone();
    let cfg_redirect = cfg.redirect_uri.clone();
    let cfg_kid = cfg.client_kid.clone();
    let code_clone = code.clone();
    let pkce_verifier = flow.pkce_verifier.clone();

    let resp = post_form_with_dpop(
        &client,
        &token_url,
        &flow.dpop_key,
        flow.dpop_nonce_auth.clone(),
        || {
            let now = now_unix_secs();
            let assertion = client_assertion(
                &cfg.client_key,
                &cfg_kid,
                &cfg_client_id,
                &issuer,
                now,
            )
            .unwrap_or_default();
            vec![
                ("grant_type".into(), "authorization_code".into()),
                ("code".into(), code_clone.clone()),
                ("code_verifier".into(), pkce_verifier.clone()),
                ("client_id".into(), cfg_client_id.clone()),
                (
                    "client_assertion_type".into(),
                    "urn:ietf:params:oauth:client-assertion-type:jwt-bearer".into(),
                ),
                ("client_assertion".into(), assertion),
                ("redirect_uri".into(), cfg_redirect.clone()),
            ]
        },
    )
    .await?;

    if !resp.status.is_success() {
        return Err(FlowError::Token {
            status: resp.status.as_u16(),
            body: resp.body,
        });
    }
    let token_body: serde_json::Value = serde_json::from_str(&resp.body)
        .map_err(|e| FlowError::TokenMissing(format!("token body json: {e}")))?;
    let sub = token_body
        .get("sub")
        .and_then(|v| v.as_str())
        .ok_or_else(|| FlowError::TokenMissing("sub".into()))?;
    if sub != flow.did {
        return Err(FlowError::SubMismatch {
            expected: flow.did.clone(),
            got: sub.to_string(),
        });
    }

    let peer_pubkey = decode_peer_pubkey(&flow.peer_pubkey_hex)?;
    let claim = laye_me::BindingClaim {
        peer_pubkey,
        provider: "atproto".to_string(),
        canonical_id: flow.did.clone(),
        handle: Some(format!("@{}", flow.handle)),
        issued_at: now_unix_secs(),
    };
    let canonical = claim.canonical_bytes();
    let signature = relay_signing_key
        .sign(&canonical)
        .map_err(|e| FlowError::Sign(e.to_string()))?;
    let signer_pubkey = relay_signing_key
        .public()
        .try_into_ed25519()
        .map_err(|e| FlowError::Sign(format!("relay pubkey not Ed25519: {e}")))?
        .to_bytes();
    let signed = laye_me::SignedBinding {
        claim,
        signature,
        signer_pubkey,
    };

    cache.insert_result(state.clone(), signed);

    Ok(format!("/me/?atproto_result={state}"))
}

// ============================================================================
// result handler — broker page fetches the finalized SignedBinding
// ============================================================================

pub async fn handle_result(
    query: &std::collections::HashMap<String, String>,
    cache: &FlowCache,
) -> Result<Vec<u8>, FlowError> {
    let state = query
        .get("state")
        .cloned()
        .ok_or_else(|| FlowError::BadRequestJson("missing ?state".into()))?;
    let signed = cache
        .get_result(&state)
        .ok_or(FlowError::UnknownState)?;
    serde_json::to_vec(&signed)
        .map_err(|e| FlowError::Http(format!("result json: {e}")))
}

// ============================================================================
// JWK export (public P-256 key → JWK JSON)
// ============================================================================

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PublicJwk {
    pub kty: String,
    pub crv: String,
    pub x: String,
    pub y: String,
    pub kid: String,
    #[serde(rename = "use")]
    pub use_: String,
    pub alg: String,
}

pub fn public_jwk(key: &SigningKey, kid: &str, purpose_sig: bool) -> PublicJwk {
    let verifying: VerifyingKey = *key.verifying_key();
    let point = verifying.to_encoded_point(false);
    // Uncompressed SEC1 encoding always carries both coordinates for a
    // valid pubkey; the None branches are unreachable for keys the
    // ecosystem produces, but clippy denies expect(). Fallback: empty
    // strings — will fail downstream verification, surfacing the bug.
    let x = point.x().map(|b| b64url_encode(b)).unwrap_or_default();
    let y = point.y().map(|b| b64url_encode(b)).unwrap_or_default();
    PublicJwk {
        kty: "EC".into(),
        crv: "P-256".into(),
        x,
        y,
        kid: kid.into(),
        use_: if purpose_sig { "sig" } else { "enc" }.into(),
        alg: "ES256".into(),
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;
    use p256::ecdsa::signature::Verifier;

    #[test]
    fn b64url_round_trips_empty_and_bytes() {
        assert_eq!(b64url_encode(b""), "");
        assert_eq!(b64url_decode("").unwrap(), Vec::<u8>::new());
        let bytes: Vec<u8> = (0u8..=255).collect();
        let enc = b64url_encode(&bytes);
        assert!(!enc.contains('='));
        assert!(!enc.contains('+'));
        assert!(!enc.contains('/'));
        assert_eq!(b64url_decode(&enc).unwrap(), bytes);
    }

    #[test]
    fn pkce_verifier_length_is_128_chars() {
        let v = pkce_verifier();
        assert_eq!(v.len(), 128);
        assert!(v.chars().all(|c| c.is_ascii_alphanumeric() || c == '-' || c == '_'));
    }

    #[test]
    fn pkce_challenge_is_deterministic_sha256_of_verifier() {
        let v = "abc".to_string();
        let c1 = pkce_challenge(&v);
        let c2 = pkce_challenge(&v);
        assert_eq!(c1, c2);
        // SHA-256("abc") = ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad
        let expected = b64url_encode(&[
            0xba, 0x78, 0x16, 0xbf, 0x8f, 0x01, 0xcf, 0xea, 0x41, 0x41, 0x40, 0xde, 0x5d, 0xae,
            0x22, 0x23, 0xb0, 0x03, 0x61, 0xa3, 0x96, 0x17, 0x7a, 0x9c, 0xb4, 0x10, 0xff, 0x61,
            0xf2, 0x00, 0x15, 0xad,
        ]);
        assert_eq!(c1, expected);
    }

    #[test]
    fn es256_jwt_verifies_with_matching_pubkey() {
        let key = SigningKey::random(&mut rand::thread_rng());
        let header = serde_json::json!({ "typ": "JWT", "alg": "ES256" });
        let claims = serde_json::json!({ "sub": "test", "iat": 1_700_000_000 });
        let jwt = sign_es256_jwt(&header, &claims, &key).unwrap();
        let parts: Vec<&str> = jwt.split('.').collect();
        assert_eq!(parts.len(), 3);
        let signing_input = format!("{}.{}", parts[0], parts[1]);
        let sig_bytes = b64url_decode(parts[2]).unwrap();
        let sig = P256Signature::from_slice(&sig_bytes).unwrap();
        let verifying = *key.verifying_key();
        verifying
            .verify(signing_input.as_bytes(), &sig)
            .expect("signature verifies");
    }

    #[test]
    fn es256_jwt_header_and_claims_round_trip_via_base64() {
        let key = SigningKey::random(&mut rand::thread_rng());
        let header = serde_json::json!({ "typ": "dpop+jwt", "alg": "ES256" });
        let claims = serde_json::json!({ "jti": "abc", "htm": "POST" });
        let jwt = sign_es256_jwt(&header, &claims, &key).unwrap();
        let parts: Vec<&str> = jwt.split('.').collect();
        let h: serde_json::Value =
            serde_json::from_slice(&b64url_decode(parts[0]).unwrap()).unwrap();
        let c: serde_json::Value =
            serde_json::from_slice(&b64url_decode(parts[1]).unwrap()).unwrap();
        assert_eq!(h["typ"], "dpop+jwt");
        assert_eq!(h["alg"], "ES256");
        assert_eq!(c["jti"], "abc");
        assert_eq!(c["htm"], "POST");
    }

    #[test]
    fn dpop_proof_headers_and_claims_shape_matches_spec() {
        let dpop_key = SigningKey::random(&mut rand::thread_rng());
        let jwt = dpop_proof(
            &dpop_key,
            "post",
            "https://pds.example.com/oauth/token",
            None,
            None,
        )
        .unwrap();
        let parts: Vec<&str> = jwt.split('.').collect();
        assert_eq!(parts.len(), 3);
        let h: serde_json::Value =
            serde_json::from_slice(&b64url_decode(parts[0]).unwrap()).unwrap();
        let c: serde_json::Value =
            serde_json::from_slice(&b64url_decode(parts[1]).unwrap()).unwrap();
        assert_eq!(h["typ"], "dpop+jwt");
        assert_eq!(h["alg"], "ES256");
        assert_eq!(h["jwk"]["kty"], "EC");
        assert_eq!(h["jwk"]["crv"], "P-256");
        assert!(h["jwk"]["x"].is_string());
        assert!(h["jwk"]["y"].is_string());
        assert_eq!(c["htm"], "POST"); // uppercased per spec
        assert_eq!(c["htu"], "https://pds.example.com/oauth/token");
        assert!(c["jti"].is_string());
        assert!(c["iat"].is_number());
        assert!(c.get("nonce").is_none());
        assert!(c.get("ath").is_none());
        assert!(c.get("iss").is_none()); // spec forbids for PDS; we also omit for auth server
    }

    #[test]
    fn dpop_proof_with_nonce_and_access_token_populates_both() {
        let dpop_key = SigningKey::random(&mut rand::thread_rng());
        let jwt = dpop_proof(
            &dpop_key,
            "GET",
            "https://pds.example.com/xrpc/com.atproto.server.getSession",
            Some("server-nonce-abc"),
            Some("access-token-xyz"),
        )
        .unwrap();
        let parts: Vec<&str> = jwt.split('.').collect();
        let c: serde_json::Value =
            serde_json::from_slice(&b64url_decode(parts[1]).unwrap()).unwrap();
        assert_eq!(c["nonce"], "server-nonce-abc");
        // ath = b64url(sha256("access-token-xyz"))
        let expected_ath = b64url_encode(&Sha256::digest(b"access-token-xyz"));
        assert_eq!(c["ath"], expected_ath);
    }

    #[test]
    fn dpop_jti_is_random_across_calls() {
        let dpop_key = SigningKey::random(&mut rand::thread_rng());
        let a = dpop_proof(&dpop_key, "POST", "https://x.example/", None, None).unwrap();
        let b = dpop_proof(&dpop_key, "POST", "https://x.example/", None, None).unwrap();
        let ac: serde_json::Value = serde_json::from_slice(
            &b64url_decode(a.split('.').nth(1).unwrap()).unwrap(),
        )
        .unwrap();
        let bc: serde_json::Value = serde_json::from_slice(
            &b64url_decode(b.split('.').nth(1).unwrap()).unwrap(),
        )
        .unwrap();
        assert_ne!(ac["jti"], bc["jti"]);
    }

    #[test]
    fn client_assertion_iss_sub_aud_shape_matches_atproto() {
        let key = SigningKey::random(&mut rand::thread_rng());
        let client_id = "https://relaye.sbvh.nl/me/client-metadata.json";
        let aud = "https://bsky.social";
        let jwt = client_assertion(&key, "kid-1", client_id, aud, 1_700_000_000).unwrap();
        let parts: Vec<&str> = jwt.split('.').collect();
        let h: serde_json::Value =
            serde_json::from_slice(&b64url_decode(parts[0]).unwrap()).unwrap();
        let c: serde_json::Value =
            serde_json::from_slice(&b64url_decode(parts[1]).unwrap()).unwrap();
        assert_eq!(h["typ"], "JWT");
        assert_eq!(h["alg"], "ES256");
        assert_eq!(h["kid"], "kid-1");
        assert_eq!(c["iss"], client_id);
        assert_eq!(c["sub"], client_id);
        assert_eq!(c["aud"], aud);
        assert_eq!(c["iat"], 1_700_000_000);
        assert_eq!(c["exp"], 1_700_000_060);
        assert!(c["jti"].is_string());
    }

    #[test]
    fn normalize_handle_lowercases_strips_at_rejects_no_dot() {
        assert_eq!(normalize_handle("@Alice.bsky.social").unwrap(), "alice.bsky.social");
        assert_eq!(normalize_handle("  bob.example.com  ").unwrap(), "bob.example.com");
        assert!(normalize_handle("").is_err());
        assert!(normalize_handle("nodot").is_err());
    }

    #[test]
    fn is_valid_did_accepts_plc_and_web() {
        assert!(is_valid_did("did:plc:abc123"));
        assert!(is_valid_did("did:web:example.com"));
        assert!(!is_valid_did("did:key:xxx"));
        assert!(!is_valid_did(""));
        assert!(!is_valid_did("did:plc:")); // technically parses but valid enough for our check
    }

    #[test]
    fn verify_handle_claim_matches_aka_at_uri() {
        let doc = DidDocument {
            id: "did:plc:abc".into(),
            also_known_as: vec!["at://alice.bsky.social".into()],
            service: vec![],
        };
        assert!(verify_handle_claim("alice.bsky.social", &doc).is_ok());
        assert!(verify_handle_claim("mallory.example.com", &doc).is_err());
    }

    #[test]
    fn extract_pds_url_finds_atproto_pds_service() {
        let doc = DidDocument {
            id: "did:plc:abc".into(),
            also_known_as: vec![],
            service: vec![
                DidService {
                    id: "#other".into(),
                    type_: "SomethingElse".into(),
                    service_endpoint: "https://other.example.com".into(),
                },
                DidService {
                    id: "#atproto_pds".into(),
                    type_: "AtprotoPersonalDataServer".into(),
                    service_endpoint: "https://alice.host.example.com/".into(),
                },
            ],
        };
        assert_eq!(
            extract_pds_url(&doc).unwrap(),
            "https://alice.host.example.com"
        );
    }

    #[test]
    fn extract_pds_url_errors_when_service_missing() {
        let doc = DidDocument {
            id: "did:plc:abc".into(),
            also_known_as: vec![],
            service: vec![],
        };
        assert!(matches!(
            extract_pds_url(&doc),
            Err(ResolveError::NoPdsService)
        ));
    }

    #[test]
    fn public_jwk_shape_matches_atproto_expected() {
        let key = SigningKey::random(&mut rand::thread_rng());
        let jwk = public_jwk(&key, "test-kid", true);
        assert_eq!(jwk.kty, "EC");
        assert_eq!(jwk.crv, "P-256");
        assert_eq!(jwk.alg, "ES256");
        assert_eq!(jwk.use_, "sig");
        assert_eq!(jwk.kid, "test-kid");
        // P-256 coordinates are 32 bytes → 43 base64url chars (no pad)
        assert_eq!(b64url_decode(&jwk.x).unwrap().len(), 32);
        assert_eq!(b64url_decode(&jwk.y).unwrap().len(), 32);
    }
}
