//! wasm-bindgen exports for the host contract: identity, signing, bindings.
//! laye renders no DOM — the host reads typed errors through `errors()`.

//! Identity is IDB-backed: DB `laye-p2p-identity`, store `identity`, keys
//! `self` (keypair) and `bindings`. The ceremony lives in the host.

// LayeError carries ~184 bytes of context; unboxed is what lets a boundary
// emit straight through.
#![allow(clippy::result_large_err)]

use crate::error::{self as errpipe, build, build_panic};
use crate::store::{IdentityStore, InMemoryStore, load_or_generate};
use futures::channel::oneshot;
use laye_error::{Error as LayeError, Severity};
use laye_me::{Keypair, SignedBinding};
use laye_net::{Net, NetConfig, NetDrive};
use serde::Deserialize;
use std::cell::RefCell;
use std::rc::Rc;
use wasm_bindgen::JsCast;
use wasm_bindgen::closure::Closure;
use wasm_bindgen::prelude::*;
use wasm_bindgen_futures::spawn_local;

const SURFACE: &str = "laye-p2p";

const IDB_NAME: &str = "laye-p2p-identity";
const IDB_STORE: &str = "identity";
const IDB_KEY: &str = "self";
const IDB_KEY_BINDINGS: &str = "bindings";

#[derive(Deserialize)]
struct BootConfig {
    bootstrap_addrs: Vec<String>,
    #[serde(default = "default_identify")]
    identify_protocol: String,
    /// Hex ed25519 pubkeys whose signature on a binding counts. Empty trusts
    /// nobody, so a host that names none resolves no handles.
    #[serde(default)]
    binding_signers: Vec<String>,
}

/// Parse the configured signers, dropping anything that is not 32 hex bytes.
/// A malformed entry is a host misconfiguration, not a reason to refuse init.
fn parse_binding_signers(hexes: &[String]) -> Vec<[u8; 32]> {
    hexes.iter().filter_map(|entry| hex_to_key(entry)).collect()
}

fn default_identify() -> String {
    "/laye/1.0.0".to_string()
}

#[derive(Deserialize)]
struct BrokerMessageWire {
    #[serde(rename = "type")]
    type_: String,
    signed: Option<SignedBinding>,
}

struct AppState {
    net: Option<Net>,
    peer_id_hex: String,
    self_pubkey: Option<[u8; 32]>,
    self_bindings: Vec<SignedBinding>,
    /// Whose signature on a binding counts. Empty trusts nobody.
    binding_signers: Vec<[u8; 32]>,
}

thread_local! {
    static STATE: RefCell<AppState> = const { RefCell::new(AppState {
        net: None,
        peer_id_hex: String::new(),
        self_pubkey: None,
        self_bindings: Vec::new(),
        binding_signers: Vec::new(),
    }) };
}

/// A panic is a typed Panic error in the same buffer as everything else.
#[wasm_bindgen(start)]
pub fn __start() {
    std::panic::set_hook(Box::new(|info| {
        let location = info
            .location()
            .map(|l| format!("{}:{}:{}", l.file(), l.line(), l.column()));
        let payload = info.payload();
        let msg = payload
            .downcast_ref::<&str>()
            .copied()
            .map(|s| s.to_string())
            .or_else(|| payload.downcast_ref::<String>().cloned())
            .unwrap_or_else(|| "<non-string panic payload>".to_string());
        let err = build_panic(location, format!("panic: {msg}"), None);
        errpipe::emit(err);
    }));
}

// Exports are fire-and-forget: they emit typed on failure, never throw.

#[wasm_bindgen]
pub async fn init(config_json: String) {
    if let Err(err) = init_inner(config_json).await {
        errpipe::emit(err);
    }
}

#[wasm_bindgen]
pub fn self_peer_id() -> String {
    STATE.with(|s| s.borrow().peer_id_hex.clone())
}

/// Empty until init has minted or loaded the keypair.
#[wasm_bindgen]
pub fn did() -> String {
    STATE.with(|s| {
        s.borrow()
            .self_pubkey
            .as_ref()
            .map(crate::didkey::encode)
            .unwrap_or_default()
    })
}

/// did:key for a PRF seed. Empty when the seed is not 32 bytes, which is an
/// authenticator answering with something that is not a PRF output.
#[wasm_bindgen]
pub fn owner_did(seed: Vec<u8>) -> String {
    match crate::didkey::keypair_from_seed(&seed) {
        Some(kp) => crate::didkey::encode(&kp.public().to_bytes()),
        None => String::new(),
    }
}

/// Signs with the PRF-derived key. Empty on a seed that derives nothing, so a
/// caller cannot mistake a failure for a signature over nothing.
#[wasm_bindgen]
pub fn owner_sign(seed: Vec<u8>, message: Vec<u8>) -> Vec<u8> {
    match crate::didkey::keypair_from_seed(&seed) {
        Some(kp) => kp.sign(&message),
        None => Vec::new(),
    }
}

/// Proof of possession. The seed stays here; only the signature crosses.
#[wasm_bindgen]
pub fn sign(bytes: Vec<u8>) -> Vec<u8> {
    match sign_inner(&bytes) {
        Ok(sig) => sig,
        Err(err) => {
            errpipe::emit(err);
            Vec::new()
        }
    }
}

/// JSON array of SignedBinding — the external identities bound to this key.
#[wasm_bindgen]
pub fn bindings() -> String {
    STATE.with(|s| {
        serde_json::to_string(&s.borrow().self_bindings).unwrap_or_else(|_| "[]".to_string())
    })
}

/// Take a binding the node signed and keep it: same verification and same
/// IndexedDB write as if laye had collected the ceremony's result itself.
#[wasm_bindgen]
pub fn accept_binding(binding_json: String) {
    let envelope = format!("{{\"type\":\"laye/identity/link\",\"signed\":{binding_json}}}");
    if let Err(err) = handle_login_message(&envelope) {
        errpipe::emit(err);
    }
}

/// The buffered typed errors, for a host that renders its own surface.
#[wasm_bindgen]
pub fn errors() -> String {
    serde_json::to_string(&errpipe::peek_all()).unwrap_or_else(|_| "[]".to_string())
}

fn sign_inner(bytes: &[u8]) -> Result<Vec<u8>, LayeError> {
    STATE.with(|s| {
        let st = s.borrow();
        let net = st.net.as_ref().ok_or_else(|| {
            build(
                Severity::Error,
                SURFACE,
                "sign-preinit",
                "sign called before init",
                "STATE.net is None",
            )
        })?;
        net.keypair().sign(bytes).map_err(|e| {
            build(
                Severity::Error,
                SURFACE,
                "sign-failed",
                "signing the caller's bytes failed",
                format!("{e}"),
            )
        })
    })
}

async fn init_inner(config_json: String) -> Result<(), LayeError> {
    let config: BootConfig = serde_json::from_str(&config_json).map_err(|e| {
        build(
            Severity::Error,
            SURFACE,
            "init-config-parse",
            "init config parse failed",
            format!("{e}"),
        )
    })?;

    let db = idb_open().await.map_err(|why| {
        build(
            Severity::Error,
            SURFACE,
            "idb-open",
            "IndexedDB open failed",
            why,
        )
    })?;

    let existing_bytes = idb_load_bytes(&db).await.map_err(|why| {
        build(
            Severity::Error,
            SURFACE,
            "idb-load-identity",
            "IndexedDB load identity failed",
            why,
        )
    })?;

    let store = InMemoryStore::from_option(existing_bytes.clone());
    let keypair = load_or_generate(&store).map_err(|e| {
        build(
            Severity::Error,
            SURFACE,
            "identity-mint",
            "identity load-or-generate failed",
            format!("{e}"),
        )
    })?;
    if existing_bytes.is_none() {
        let fresh_bytes = store
            .load()
            .map_err(|e| {
                build(
                    Severity::Error,
                    SURFACE,
                    "identity-store",
                    "in-memory store did not accept a mint",
                    format!("{e}"),
                )
            })?
            .ok_or_else(|| {
                build(
                    Severity::Error,
                    SURFACE,
                    "identity-store",
                    "mint did not populate store",
                    "load returned None after fresh()",
                )
            })?;
        idb_save_bytes(&db, &fresh_bytes).await.map_err(|why| {
            build(
                Severity::Error,
                SURFACE,
                "idb-save-identity",
                "IndexedDB save identity failed",
                why,
            )
        })?;
    }

    let self_pubkey = self_ed25519_pubkey(&keypair)?;
    let peer_id_hex = hex_lower(&self_pubkey);

    // A stored binding is recoverable by asking the node again, so an
    // unreadable blob is a warning and an empty set — never the reason
    // nothing works until IndexedDB is deleted.
    let self_bindings = match idb_load_bindings(&db).await {
        Ok(loaded) => loaded,
        Err(why) => {
            errpipe::emit(build(
                Severity::Warn,
                SURFACE,
                "idb-load-bindings",
                "IndexedDB load bindings failed, continuing with none",
                why,
            ));
            Vec::new()
        }
    };

    let (net, drive) = laye_net::new(NetConfig {
        bootstrap_addrs: config.bootstrap_addrs,
        keypair,
        topics: Vec::new(),
        identify_protocol: config.identify_protocol,
    })
    .map_err(|e| {
        build(
            Severity::Error,
            SURFACE,
            "net-build",
            "laye_net::new failed",
            format!("{e}"),
        )
    })?;

    // The signer list can change under what IndexedDB already holds, so
    // applying it on load is what makes striking a signer out of am.toml
    // reach the bindings already on disk.
    let binding_signers = parse_binding_signers(&config.binding_signers);
    let dropped = self_bindings.len();
    let self_bindings: Vec<SignedBinding> = self_bindings
        .into_iter()
        .filter(|b| binding_signers.contains(&b.signer_pubkey) && b.verify().is_ok())
        .collect();
    if dropped > self_bindings.len() {
        errpipe::emit(build(
            Severity::Warn,
            SURFACE,
            "idb-untrusted-binding",
            "stored bindings dropped: signer is not in auth.binding_signers",
            format!("{} of {dropped} kept", self_bindings.len()),
        ));
    }

    STATE.with(|s| {
        let mut st = s.borrow_mut();
        st.net = Some(net);
        st.peer_id_hex = peer_id_hex;
        st.self_pubkey = Some(self_pubkey);
        st.self_bindings = self_bindings;
        st.binding_signers = binding_signers;
    });

    spawn_local(drive_forever(drive));

    Ok(())
}

async fn drive_forever(drive: NetDrive) {
    drive.await;
}

fn handle_login_message(json_str: &str) -> Result<(), LayeError> {
    let msg: BrokerMessageWire = serde_json::from_str(json_str).map_err(|e| {
        build(
            Severity::Warn,
            SURFACE,
            "login-parse",
            "binding envelope parse failed",
            format!("{e}"),
        )
    })?;
    if msg.type_ != "laye/identity/link" {
        return Ok(()); // ignore non-link messages silently — not a failure
    }
    let binding = msg.signed.ok_or_else(|| {
        build(
            Severity::Warn,
            SURFACE,
            "login-empty",
            "envelope carried no signed binding",
            "msg.type_ == laye/identity/link but msg.signed == null",
        )
    })?;
    binding.verify().map_err(|e| {
        build(
            Severity::Error,
            SURFACE,
            "login-verify",
            "signed binding signature verification failed",
            format!("{e:?}"),
        )
    })?;
    // verify() reads the signing key out of the message, so alone it proves
    // only self-consistency. The signer list decides what that is worth.
    let trusted = STATE.with(|s| s.borrow().binding_signers.contains(&binding.signer_pubkey));
    if !trusted {
        return Err(build(
            Severity::Error,
            SURFACE,
            "login-untrusted-signer",
            "binding is signed by a key that is not in auth.binding_signers",
            format!("signer {}", hex_lower(&binding.signer_pubkey)),
        ));
    }
    let self_pubkey = STATE.with(|s| s.borrow().self_pubkey).ok_or_else(|| {
        build(
            Severity::Error,
            SURFACE,
            "login-preinit",
            "binding accepted before self_pubkey is known",
            "state.self_pubkey is None",
        )
    })?;
    if binding.claim.peer_pubkey != self_pubkey {
        return Err(build(
            Severity::Warn,
            SURFACE,
            "login-wrong-peer",
            "the signed binding names a different peer",
            "claim.peer_pubkey != self_pubkey — dropping",
        ));
    }
    let provider = binding.claim.provider.clone();
    STATE.with(|s| {
        let mut st = s.borrow_mut();
        st.self_bindings.retain(|b| b.claim.provider != provider);
        st.self_bindings.insert(0, binding);
    });
    // Success emits nothing: a confirmation in the failure surface is noise.

    let bindings_snap = STATE.with(|s| s.borrow().self_bindings.clone());
    spawn_local(async move {
        match idb_open().await {
            Ok(db) => {
                if let Err(why) = idb_save_bindings(&db, &bindings_snap).await {
                    errpipe::emit(build(
                        Severity::Warn,
                        SURFACE,
                        "idb-save-bindings",
                        "IndexedDB save bindings failed",
                        why,
                    ));
                }
            }
            Err(why) => {
                errpipe::emit(build(
                    Severity::Warn,
                    SURFACE,
                    "idb-open",
                    "IndexedDB open failed in login persist",
                    why,
                ));
            }
        }
    });
    Ok(())
}

/// The 32 raw bytes behind this keypair's public half. did:key, the peer hex
/// and a binding claim are all this value in another encoding.
fn self_ed25519_pubkey(kp: &Keypair) -> Result<[u8; 32], LayeError> {
    let ed = kp.public().try_into_ed25519().map_err(|e| {
        build(
            Severity::Error,
            SURFACE,
            "identity-nonEd25519",
            "public key is not Ed25519",
            format!("{e}"),
        )
    })?;
    Ok(ed.to_bytes())
}

/// The inverse of hex_lower for a 32-byte key. None for anything that is not
/// exactly 64 hex characters, so a truncated key is refused rather than padded.
fn hex_to_key(s: &str) -> Option<[u8; 32]> {
    let chars = s.as_bytes();
    if chars.len() != 64 {
        return None;
    }
    let mut out = [0u8; 32];
    for (i, slot) in out.iter_mut().enumerate() {
        let hi = nibble(chars[i * 2])?;
        let lo = nibble(chars[i * 2 + 1])?;
        *slot = (hi << 4) | lo;
    }
    Some(out)
}

fn nibble(c: u8) -> Option<u8> {
    match c {
        b'0'..=b'9' => Some(c - b'0'),
        b'a'..=b'f' => Some(c - b'a' + 10),
        b'A'..=b'F' => Some(c - b'A' + 10),
        _ => None,
    }
}

fn hex_lower(bytes: &[u8]) -> String {
    const HEX: &[u8; 16] = b"0123456789abcdef";
    let mut s = String::with_capacity(bytes.len() * 2);
    for b in bytes {
        s.push(HEX[(b >> 4) as usize] as char);
        s.push(HEX[(b & 0x0f) as usize] as char);
    }
    s
}

async fn idb_open() -> Result<web_sys::IdbDatabase, String> {
    let factory = web_sys::window()
        .ok_or_else(|| "no window".to_string())?
        .indexed_db()
        .map_err(|e| format!("indexed_db(): {e:?}"))?
        .ok_or_else(|| "indexedDB unavailable".to_string())?;
    let req = factory
        .open_with_u32(IDB_NAME, 1)
        .map_err(|e| format!("open(): {e:?}"))?;

    let upgrade_req = req.clone();
    let onupgrade = Closure::<dyn FnMut(web_sys::IdbVersionChangeEvent)>::new(
        move |_ev: web_sys::IdbVersionChangeEvent| {
            let Ok(val) = upgrade_req.result() else {
                return;
            };
            let Ok(db) = val.dyn_into::<web_sys::IdbDatabase>() else {
                return;
            };
            let names = db.object_store_names();
            let mut has_store = false;
            for i in 0..names.length() {
                if names.item(i).as_deref() == Some(IDB_STORE) {
                    has_store = true;
                    break;
                }
            }
            if !has_store {
                let _ = db.create_object_store(IDB_STORE);
            }
        },
    );
    req.set_onupgradeneeded(Some(onupgrade.as_ref().unchecked_ref()));
    onupgrade.forget();

    let val = idb_request_promise(req.unchecked_ref::<web_sys::IdbRequest>()).await?;
    val.dyn_into::<web_sys::IdbDatabase>()
        .map_err(|_| "IdbOpenDbRequest result was not an IdbDatabase".to_string())
}

async fn idb_load_bytes(db: &web_sys::IdbDatabase) -> Result<Option<Vec<u8>>, String> {
    let tx = db
        .transaction_with_str(IDB_STORE)
        .map_err(|e| format!("transaction(readonly): {e:?}"))?;
    let store = tx
        .object_store(IDB_STORE)
        .map_err(|e| format!("object_store: {e:?}"))?;
    let req = store
        .get(&JsValue::from_str(IDB_KEY))
        .map_err(|e| format!("get: {e:?}"))?;
    let val = idb_request_promise(&req).await?;
    if val.is_null() || val.is_undefined() {
        return Ok(None);
    }
    let arr = val
        .dyn_into::<js_sys::Uint8Array>()
        .map_err(|_| "stored identity is not a Uint8Array".to_string())?;
    let mut bytes = vec![0u8; arr.length() as usize];
    arr.copy_to(&mut bytes);
    Ok(Some(bytes))
}

async fn idb_save_bytes(db: &web_sys::IdbDatabase, bytes: &[u8]) -> Result<(), String> {
    let tx = db
        .transaction_with_str_and_mode(IDB_STORE, web_sys::IdbTransactionMode::Readwrite)
        .map_err(|e| format!("transaction(readwrite): {e:?}"))?;
    let store = tx
        .object_store(IDB_STORE)
        .map_err(|e| format!("object_store: {e:?}"))?;
    let arr = js_sys::Uint8Array::from(bytes);
    let req = store
        .put_with_key(&arr.into(), &JsValue::from_str(IDB_KEY))
        .map_err(|e| format!("put_with_key: {e:?}"))?;
    idb_request_promise(&req).await?;
    Ok(())
}

async fn idb_load_bindings(db: &web_sys::IdbDatabase) -> Result<Vec<SignedBinding>, String> {
    let tx = db
        .transaction_with_str(IDB_STORE)
        .map_err(|e| format!("transaction(readonly): {e:?}"))?;
    let store = tx
        .object_store(IDB_STORE)
        .map_err(|e| format!("object_store: {e:?}"))?;
    let req = store
        .get(&JsValue::from_str(IDB_KEY_BINDINGS))
        .map_err(|e| format!("get: {e:?}"))?;
    let val = idb_request_promise(&req).await?;
    if val.is_null() || val.is_undefined() {
        return Ok(Vec::new());
    }
    let arr = val
        .dyn_into::<js_sys::Uint8Array>()
        .map_err(|_| "stored bindings not a Uint8Array".to_string())?;
    let mut bytes = vec![0u8; arr.length() as usize];
    arr.copy_to(&mut bytes);
    serde_json::from_slice(&bytes).map_err(|e| format!("bindings decode: {e}"))
}

async fn idb_save_bindings(
    db: &web_sys::IdbDatabase,
    bindings: &[SignedBinding],
) -> Result<(), String> {
    let tx = db
        .transaction_with_str_and_mode(IDB_STORE, web_sys::IdbTransactionMode::Readwrite)
        .map_err(|e| format!("transaction(readwrite): {e:?}"))?;
    let store = tx
        .object_store(IDB_STORE)
        .map_err(|e| format!("object_store: {e:?}"))?;
    let json = serde_json::to_vec(bindings).map_err(|e| format!("bindings encode: {e}"))?;
    let arr = js_sys::Uint8Array::from(json.as_slice());
    let req = store
        .put_with_key(&arr.into(), &JsValue::from_str(IDB_KEY_BINDINGS))
        .map_err(|e| format!("put_with_key: {e:?}"))?;
    idb_request_promise(&req).await?;
    Ok(())
}

async fn idb_request_promise(req: &web_sys::IdbRequest) -> Result<JsValue, String> {
    let (tx, rx) = oneshot::channel::<Result<JsValue, String>>();
    let sender = Rc::new(RefCell::new(Some(tx)));

    let success_req = req.clone();
    let sender_success = sender.clone();
    let onsuccess = Closure::<dyn FnMut(web_sys::Event)>::new(move |_ev: web_sys::Event| {
        let Some(tx) = sender_success.borrow_mut().take() else {
            return;
        };
        let result = success_req
            .result()
            .map_err(|e| format!("IdbRequest.result: {e:?}"));
        let _ = tx.send(result);
    });
    req.set_onsuccess(Some(onsuccess.as_ref().unchecked_ref()));
    onsuccess.forget();

    let error_req = req.clone();
    let sender_error = sender;
    let onerror = Closure::<dyn FnMut(web_sys::Event)>::new(move |_ev: web_sys::Event| {
        let Some(tx) = sender_error.borrow_mut().take() else {
            return;
        };
        let msg = error_req
            .error()
            .ok()
            .flatten()
            .map(|e| e.message())
            .unwrap_or_else(|| "unknown IDB error".to_string());
        let _ = tx.send(Err(msg));
    });
    req.set_onerror(Some(onerror.as_ref().unchecked_ref()));
    onerror.forget();

    rx.await.map_err(|_| "IDB oneshot canceled".to_string())?
}
