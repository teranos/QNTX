//! wasm-bindgen exports for the host contract + laye-p2p's DOM overlays.
//!
//! Every boundary that can fail emits a typed `laye_error::Error` into
//! the pipeline. The error overlay renders each Error per the sacred
//! visual contract (dark-red terminal block, severity ribbon, title/why
//! /trace/dismiss). Nothing collapses to `String`, nothing swallows.
//!
//! Chat: laye-p2p subscribes internally to laye-chat/v1 (signed) and
//! rave-chat/v1 (legacy plaintext receive). It renders its own DOM
//! overlay for chat. subscribe_opaque refuses internal topics via a
//! typed Error. laye_is_focused() is the focus contract with the host.
//!
//! Identity: IDB-backed under DB `laye-p2p-identity`, store `identity`,
//! key `self` (keypair) + `bindings` (Vec<SignedBinding>). Login uses
//! the `relaye.sbvh.nl/me/` broker via popup + postMessage.
//!
//! # Adapter isolation (planned)
//!
//! Today this module still uses wasm-bindgen imports (JsCast, Closure,
//! JsValue) throughout. Slice 2 will move all such usage into
//! `wasm/bindings.rs`; this file will be pure typed Rust. For now, the
//! seam discipline is enforced by: (1) `Result<T, laye_error::Error>`
//! on every internal fn, (2) `#[wasm_bindgen]` exports are fire-and-
//! forget — they emit typed and render into the overlay on failure,
//! never return `JsValue::from_str`.

// LayeError carries full context (~184 bytes). Result<T, LayeError> is
// heavy but the perf cost at a boundary is negligible; the axiom prefers
// the typed value unboxed for straight-through emit. Slice 2 will move
// to Box<Error> if the seam benefits from it.
#![allow(clippy::result_large_err)]

use crate::binding::BindingTable;
use crate::chat::{
    self, CHAT_TOPIC, ChatEntry, ChatState, IncomingChat, LEGACY_CHAT_TOPIC, MAX_BODY_BYTES,
};
use crate::error::{self as errpipe, build, build_panic};
use crate::identity::{self, IDENTITY_TOPIC};
use crate::state::RxState;
use crate::store::{IdentityStore, InMemoryStore, load_or_generate};
use futures::channel::oneshot;
use laye_error::{Error as LayeError, Severity};
use laye_me::{Keypair, SignedBinding};
use laye_net::{Net, NetConfig, NetDrive, NetEvent};
use laye_protocol::Topic;
use serde::Deserialize;
use std::cell::RefCell;
use std::collections::{HashMap, HashSet, VecDeque};
use std::rc::Rc;
use wasm_bindgen::JsCast;
use wasm_bindgen::closure::Closure;
use wasm_bindgen::prelude::*;
use wasm_bindgen_futures::spawn_local;

/// Per-topic outbound queue cap. gossipsub's mesh may take ~1s (heartbeat)
/// to form after a peer subscribes; publishers that fire before then get
/// NoPeersSubscribedToTopic. We buffer up to this many sends and flush on
/// the first SubscriptionChange{joined:true} we observe for the topic.
/// Positions at 10Hz keep only the freshest few — deeper stale is worse
/// than dropped stale.
const PENDING_SENDS_CAP: usize = 8;

const SURFACE: &str = "laye-p2p";

const IDB_NAME: &str = "laye-p2p-identity";
const IDB_STORE: &str = "identity";
const IDB_KEY: &str = "self";
const IDB_KEY_BINDINGS: &str = "bindings";

const BROKER_ORIGIN: &str = "https://relaye.sbvh.nl";
const BROKER_LOGIN_PATH: &str = "/me/";
const POPUP_TARGET: &str = "laye-login";
const POPUP_FEATURES: &str = "width=520,height=720";

#[derive(Deserialize)]
struct BootConfig {
    bootstrap_addrs: Vec<String>,
    #[serde(default)]
    topics: Vec<String>,
    #[serde(default = "default_identify")]
    identify_protocol: String,
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
    rx: RxState,
    peer_id_hex: String,
    self_pubkey: Option<[u8; 32]>,
    chat: ChatState,
    bindings: BindingTable,
    self_bindings: Vec<SignedBinding>,
    is_focused: bool,
    messages_el: Option<web_sys::HtmlElement>,
    input_el: Option<web_sys::HtmlInputElement>,
    login_button_el: Option<web_sys::HtmlButtonElement>,
    error_list_el: Option<web_sys::HtmlElement>,
    /// For each topic, the set of peers we've observed as subscribed.
    /// Populated from `NetEvent::SubscriptionChange`. A topic with a
    /// non-empty set has a mesh forwarder — publish will succeed.
    topic_peers: HashMap<String, HashSet<String>>,
    /// Bytes queued to publish once a topic's mesh forms. FIFO capped at
    /// `PENDING_SENDS_CAP` per topic; oldest drops on overflow.
    pending_sends: HashMap<String, VecDeque<Vec<u8>>>,
    /// Coalescing map: `surface|region|title` → dom_id of the existing
    /// error node. Recurring failures bump a count on that node instead
    /// of appending a new one — 10 Hz publishers won't drown the overlay.
    error_dedup: HashMap<String, String>,
    /// dom_id → count. Bumped each time coalesce hits.
    error_counts: HashMap<String, u64>,
}

thread_local! {
    static STATE: RefCell<AppState> = RefCell::new(AppState {
        net: None,
        rx: RxState::new(),
        peer_id_hex: String::new(),
        self_pubkey: None,
        chat: ChatState::default(),
        bindings: BindingTable::default(),
        self_bindings: Vec::new(),
        is_focused: false,
        messages_el: None,
        input_el: None,
        login_button_el: None,
        error_list_el: None,
        topic_peers: HashMap::new(),
        pending_sends: HashMap::new(),
        error_dedup: HashMap::new(),
        error_counts: HashMap::new(),
    });
}

fn has_mesh_for(topic: &str) -> bool {
    STATE.with(|s| {
        s.borrow()
            .topic_peers
            .get(topic)
            .map(|set| !set.is_empty())
            .unwrap_or(false)
    })
}

fn enqueue_send(topic: String, bytes: Vec<u8>) {
    STATE.with(|s| {
        let mut st = s.borrow_mut();
        let queue = st.pending_sends.entry(topic).or_default();
        if queue.len() >= PENDING_SENDS_CAP {
            queue.pop_front();
        }
        queue.push_back(bytes);
    });
}

fn flush_pending_for(topic: &str) {
    let queued: Vec<Vec<u8>> = STATE.with(|s| {
        let mut st = s.borrow_mut();
        st.pending_sends
            .get_mut(topic)
            .map(|q| q.drain(..).collect())
            .unwrap_or_default()
    });
    if queued.is_empty() {
        return;
    }
    for bytes in queued {
        let result = STATE.with(|s| {
            s.borrow()
                .net
                .as_ref()
                .map(|net| net.publish(&Topic(topic.to_string()), &bytes))
        });
        match result {
            Some(Ok(())) => {}
            Some(Err(e)) => {
                errpipe::emit(build(
                    Severity::Warn,
                    SURFACE,
                    "publish-flush-cmd",
                    "post-mesh flush publish failed",
                    format!("topic={topic} err={e}"),
                ));
            }
            None => {
                errpipe::emit(build(
                    Severity::Error,
                    SURFACE,
                    "publish-flush-preinit",
                    "post-mesh flush ran before net init",
                    format!("topic={topic}"),
                ));
            }
        }
    }
}

// ============================================================================
// Panic hook (routes into typed Error pipeline)
// ============================================================================

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
        render_errors();
    }));
}

// ============================================================================
// wasm-bindgen exports (thin fire-and-forget shims)
// ============================================================================

#[wasm_bindgen]
pub async fn init(config_json: String) {
    if let Err(err) = init_inner(config_json).await {
        errpipe::emit(err);
    }
    render_errors();
}

#[wasm_bindgen]
pub fn subscribe_opaque(topic: String) {
    if let Err(err) = subscribe_opaque_inner(topic) {
        errpipe::emit(err);
        render_errors();
    }
}

#[wasm_bindgen]
pub fn publish(topic: String, bytes: Vec<u8>) {
    if let Err(err) = publish_inner(topic, bytes) {
        errpipe::emit(err);
        render_errors();
    }
}

#[wasm_bindgen]
pub fn pending_bytes(topic: String) -> u32 {
    drain_net_events();
    STATE.with(|s| s.borrow().rx.pending_bytes(&topic))
}

#[wasm_bindgen]
pub fn recv_bytes(topic: String) -> Vec<u8> {
    drain_net_events();
    STATE.with(|s| {
        let mut st = s.borrow_mut();
        let n = st.rx.pending_bytes(&topic) as usize;
        let mut buf = vec![0u8; n];
        st.rx.drain_into(&topic, &mut buf);
        buf
    })
}

#[wasm_bindgen]
pub fn self_peer_id() -> String {
    STATE.with(|s| s.borrow().peer_id_hex.clone())
}

#[wasm_bindgen]
pub fn laye_is_focused() -> bool {
    STATE.with(|s| s.borrow().is_focused)
}

/// External emit — hosts route their own typed Errors through laye's
/// overlay so one render path serves every layer per ERROR.md's axiom.
/// Payload is the JSON-serialized `laye_error::Error` wire shape.
#[wasm_bindgen]
pub fn emit_error(error_json: String) {
    match serde_json::from_str::<LayeError>(&error_json) {
        Ok(err) => errpipe::emit(err),
        Err(e) => errpipe::emit(build(
            Severity::Warn,
            SURFACE,
            "emit_error-parse",
            "external emit_error payload rejected",
            format!("{e}"),
        )),
    }
    render_errors();
}

#[wasm_bindgen]
pub fn dismiss_error(id: String) {
    // Errors are display:none, not destroyed — Slice 6 axiom preserves
    // Error.id → DOM node bijection so a subsequent re-render doesn't
    // rebuild the node. `id` addresses a specific overlay entry.
    if let Ok(doc) = document()
        && let Some(el) = doc.get_element_by_id(&id)
        && let Some(html) = el.dyn_ref::<web_sys::HtmlElement>()
    {
        let _ = html.style().set_property("display", "none");
    }
}

// ============================================================================
// Typed inner impls — all return Result<T, LayeError>. No JsValue in scope.
// ============================================================================

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
    if let Some(bad) = config.topics.iter().find(|t| is_internal_topic(t)) {
        return Err(build(
            Severity::Error,
            SURFACE,
            "init-topic-guard",
            "host attempted to subscribe to a laye-p2p-internal topic",
            format!(
                "{bad} is internal (laye-chat/v1, laye-identity/v1, rave-chat/v1) — subscribed automatically"
            ),
        ));
    }

    let db = idb_open()
        .await
        .map_err(|why| build(Severity::Error, SURFACE, "idb-open", "IndexedDB open failed", why))?;

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

    let peer_id_hex = pubkey_hex(&keypair)?;
    let self_pubkey = chat::self_peer_pubkey(&keypair).ok_or_else(|| {
        build(
            Severity::Error,
            SURFACE,
            "identity-nonEd25519",
            "keypair is not Ed25519",
            "chat::self_peer_pubkey returned None",
        )
    })?;

    let self_bindings = idb_load_bindings(&db).await.map_err(|why| {
        build(
            Severity::Warn,
            SURFACE,
            "idb-load-bindings",
            "IndexedDB load bindings failed",
            why,
        )
    })?;

    let mut topics: Vec<Topic> = config.topics.into_iter().map(Topic).collect();
    topics.push(Topic(CHAT_TOPIC.into()));
    topics.push(Topic(LEGACY_CHAT_TOPIC.into()));
    topics.push(Topic(IDENTITY_TOPIC.into()));

    let (net, drive) = laye_net::new(NetConfig {
        bootstrap_addrs: config.bootstrap_addrs,
        keypair,
        topics,
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

    STATE.with(|s| {
        let mut st = s.borrow_mut();
        st.net = Some(net);
        st.peer_id_hex = peer_id_hex;
        st.self_pubkey = Some(self_pubkey);
        if !self_bindings.is_empty() {
            st.bindings.0.insert(self_pubkey, self_bindings.clone());
        }
        st.self_bindings = self_bindings;
    });

    spawn_local(drive_forever(drive));

    install_error_overlay()?;
    install_chat_overlay()?;
    update_login_button();

    // TODO: FIX: republish when observe subscribe — this fires before
    // relaye's mesh for laye-identity/v1 has formed, producing
    // NoPeersSubscribedToTopic. Move to a SubscriptionChange handler
    // that fires once relaye joins the identity topic mesh.
    republish_self_bindings();
    Ok(())
}

async fn drive_forever(drive: NetDrive) {
    drive.await;
}

fn is_internal_topic(topic: &str) -> bool {
    topic == CHAT_TOPIC || topic == LEGACY_CHAT_TOPIC || topic == IDENTITY_TOPIC
}

fn subscribe_opaque_inner(topic: String) -> Result<(), LayeError> {
    if is_internal_topic(&topic) {
        return Err(build(
            Severity::Error,
            SURFACE,
            "subscribe-internal-topic",
            "host attempted subscribe_opaque on an internal topic",
            format!("{topic} is subscribed internally"),
        ));
    }
    let publish_result = STATE.with(|s| {
        let st = s.borrow();
        st.net
            .as_ref()
            .map(|net| net.subscribe(&Topic(topic.clone())))
    });
    match publish_result {
        Some(Ok(())) => Ok(()),
        Some(Err(e)) => Err(build(
            Severity::Warn,
            SURFACE,
            "subscribe-cmd",
            "subscribe cmd send failed",
            format!("{e}"),
        )),
        None => Err(build(
            Severity::Error,
            SURFACE,
            "subscribe-preinit",
            "subscribe called before init",
            "STATE.net is None",
        )),
    }
}

fn publish_inner(topic: String, bytes: Vec<u8>) -> Result<(), LayeError> {
    if is_internal_topic(&topic) {
        return Err(build(
            Severity::Error,
            SURFACE,
            "publish-internal-topic",
            "host attempted publish on an internal topic",
            format!("{topic} is owned by laye-p2p"),
        ));
    }
    if !has_mesh_for(&topic) {
        // Not a failure — buffer until the mesh forms. drain_net_events
        // flushes on SubscriptionChange{joined:true}.
        enqueue_send(topic, bytes);
        return Ok(());
    }
    let publish_result = STATE.with(|s| {
        let st = s.borrow();
        st.net
            .as_ref()
            .map(|net| net.publish(&Topic(topic.clone()), &bytes))
    });
    match publish_result {
        Some(Ok(())) => Ok(()),
        Some(Err(e)) => Err(build(
            Severity::Warn,
            SURFACE,
            "publish-cmd",
            "publish cmd send failed",
            format!("{e}"),
        )),
        None => Err(build(
            Severity::Error,
            SURFACE,
            "publish-preinit",
            "publish called before init",
            "STATE.net is None",
        )),
    }
}

// ============================================================================
// Net drive pump — routes NetEvents into chat/identity/rx + emits typed errors
// ============================================================================

fn drain_net_events() {
    let (events, self_pubkey, self_peer_id_str) = STATE.with(|s| {
        let st = s.borrow();
        let events = match st.net.as_ref() {
            Some(net) => net.poll_events(),
            None => return (Vec::new(), None, String::new()),
        };
        let self_pubkey = st
            .net
            .as_ref()
            .and_then(|net| chat::self_peer_pubkey(net.keypair()));
        let self_peer_id_str = st.peer_id_hex.clone();
        (events, self_pubkey, self_peer_id_str)
    });

    let mut chat_appended = 0usize;
    let mut bindings_changed = false;
    let mut collected_errors: Vec<LayeError> = Vec::new();
    let mut topics_to_flush: HashSet<String> = HashSet::new();

    STATE.with(|s| {
        let mut st = s.borrow_mut();
        let mut opaque = Vec::with_capacity(events.len());
        for e in events {
            match &e {
                NetEvent::SubscriptionChange { topic, peer, joined } => {
                    let entry = st.topic_peers.entry(topic.0.clone()).or_default();
                    if *joined {
                        let was_empty = entry.is_empty();
                        entry.insert(peer.0.clone());
                        if was_empty {
                            topics_to_flush.insert(topic.0.clone());
                        }
                    } else {
                        entry.remove(&peer.0);
                        if entry.is_empty() {
                            st.topic_peers.remove(&topic.0);
                        }
                    }
                    continue;
                }
                NetEvent::PeerDown { peer, .. } => {
                    // Peer gone → drop from every topic's peer set. If a
                    // topic empties, it's no longer meshed; future sends
                    // buffer.
                    let mut emptied: Vec<String> = Vec::new();
                    for (topic, set) in st.topic_peers.iter_mut() {
                        if set.remove(&peer.0) && set.is_empty() {
                            emptied.push(topic.clone());
                        }
                    }
                    for t in emptied {
                        st.topic_peers.remove(&t);
                    }
                }
                NetEvent::Message { topic, bytes, from, .. } => {
                    if topic.0 == CHAT_TOPIC || topic.0 == LEGACY_CHAT_TOPIC {
                        if let Some(incoming) = chat::ingest(
                            &topic.0,
                            bytes,
                            self_pubkey.as_ref(),
                            &self_peer_id_str,
                        ) {
                            let entry = match incoming {
                                IncomingChat::Signed(c) => ChatEntry {
                                    who: chat::attribute_author(
                                        Some(&st.bindings),
                                        &c.author_peer_pubkey,
                                    ),
                                    body: c.body,
                                },
                                IncomingChat::Plaintext(c) => ChatEntry {
                                    who: chat::short_peer_display(&c.peer),
                                    body: c.body,
                                },
                            };
                            st.chat.push(entry);
                            chat_appended += 1;
                        }
                        continue;
                    }
                    if topic.0 == IDENTITY_TOPIC
                        && let Some(publisher_pubkey) = peer_id_str_to_pubkey(&from.0)
                    {
                        let verified = identity::parse_and_verify(bytes, &publisher_pubkey);
                        identity::absorb(
                            &mut st.bindings,
                            publisher_pubkey,
                            verified.clone(),
                        );
                        bindings_changed = true;
                        continue;
                    }
                }
                NetEvent::Error(net_err) => {
                    collected_errors.push(build(
                        Severity::Warn,
                        SURFACE,
                        "net-event",
                        "libp2p transport surfaced an error",
                        format!("{net_err}"),
                    ));
                    continue;
                }
                _ => {}
            }
            opaque.push(e);
        }
        st.rx.ingest(opaque);
    });

    for err in collected_errors {
        errpipe::emit(err);
    }

    // Flush per-topic outbound queues for any topic whose mesh just
    // became non-empty. First-flush is also where LM's republish race
    // resolves — the initial `republish_self_bindings` at init enqueues
    // if no laye-identity/v1 peer is in the mesh; the first
    // SubscriptionChange{joined:true} on that topic flushes it.
    for topic in topics_to_flush {
        flush_pending_for(&topic);
    }

    if chat_appended > 0 || bindings_changed {
        render_chat();
    }
    if bindings_changed {
        update_login_button();
    }
    render_errors();
}

fn peer_id_str_to_pubkey(peer_id_str: &str) -> Option<[u8; 32]> {
    let peer_id: libp2p_identity::PeerId = peer_id_str.parse().ok()?;
    let multihash = peer_id.as_ref();
    let digest = multihash.digest();
    let pk = libp2p_identity::PublicKey::try_decode_protobuf(digest).ok()?;
    let ed = pk.try_into_ed25519().ok()?;
    Some(ed.to_bytes())
}

fn pubkey_hex(kp: &Keypair) -> Result<String, LayeError> {
    let ed = kp.public().try_into_ed25519().map_err(|e| {
        build(
            Severity::Error,
            SURFACE,
            "identity-nonEd25519",
            "public key is not Ed25519",
            format!("{e}"),
        )
    })?;
    Ok(hex_lower(&ed.to_bytes()))
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

fn document() -> Result<web_sys::Document, LayeError> {
    web_sys::window()
        .ok_or_else(|| {
            build(
                Severity::Error,
                SURFACE,
                "dom-window",
                "web_sys::window() returned None",
                "no window in this context",
            )
        })?
        .document()
        .ok_or_else(|| {
            build(
                Severity::Error,
                SURFACE,
                "dom-document",
                "window.document is None",
                "window returned but document did not",
            )
        })
}

// ============================================================================
// Chat overlay (unchanged shape; JS-borrow via wasm-bindgen types kept here)
// ============================================================================

fn install_chat_overlay() -> Result<(), LayeError> {
    let doc = document()?;
    inject_stylesheet(&doc)?;

    let body = doc.body().ok_or_else(|| {
        build(
            Severity::Error,
            SURFACE,
            "dom-body",
            "document.body is None",
            "no body element in document",
        )
    })?;

    let container = create_element(&doc, "div")?;
    container.set_class_name("laye-overlay");

    let header = create_element(&doc, "div")?;
    header.set_class_name("laye-header");
    let title = create_element(&doc, "span")?;
    title.set_class_name("laye-header-title");
    title.set_text_content(Some("laye-chat/v1"));
    append_child(&header, &title)?;

    let login_btn_el = create_element(&doc, "button")?;
    let login_btn: web_sys::HtmlButtonElement = login_btn_el
        .dyn_into::<web_sys::HtmlButtonElement>()
        .map_err(|_| {
            build(
                Severity::Error,
                SURFACE,
                "dom-cast",
                "button element cast failed",
                "created <button> would not cast to HtmlButtonElement",
            )
        })?;
    login_btn.set_class_name("laye-login-button");
    login_btn.set_text_content(Some("log in"));
    let login_click = Closure::<dyn FnMut(web_sys::Event)>::new(move |_ev| {
        if let Err(err) = open_login_popup() {
            errpipe::emit(err);
            render_errors();
        }
    });
    login_btn
        .add_event_listener_with_callback("click", login_click.as_ref().unchecked_ref())
        .map_err(|_| {
            build(
                Severity::Warn,
                SURFACE,
                "dom-listener",
                "attach click listener failed",
                "login button add_event_listener_with_callback failed",
            )
        })?;
    login_click.forget();
    append_child(&header, &login_btn)?;
    append_child(&container, &header)?;

    let messages = create_element(&doc, "div")?;
    messages.set_class_name("laye-messages");
    append_child(&container, &messages)?;

    let input_row = create_element(&doc, "div")?;
    input_row.set_class_name("laye-input-row");
    let input_el_raw = create_element(&doc, "input")?;
    let input_el: web_sys::HtmlInputElement =
        input_el_raw.dyn_into::<web_sys::HtmlInputElement>().map_err(|_| {
            build(
                Severity::Error,
                SURFACE,
                "dom-cast",
                "input element cast failed",
                "created <input> would not cast to HtmlInputElement",
            )
        })?;
    input_el.set_class_name("laye-input");
    input_el.set_placeholder("chat…");
    append_child(&input_row, &input_el)?;
    append_child(&container, &input_row)?;

    body.append_child(&container).map_err(|_| {
        build(
            Severity::Error,
            SURFACE,
            "dom-mount",
            "append chat overlay to body failed",
            "body.append_child returned Err",
        )
    })?;

    let messages_html: web_sys::HtmlElement =
        messages.dyn_into::<web_sys::HtmlElement>().map_err(|_| {
            build(
                Severity::Error,
                SURFACE,
                "dom-cast",
                "messages element cast failed",
                "messages div would not cast to HtmlElement",
            )
        })?;

    let input_target = input_el.clone();
    let focus_cb = Closure::<dyn FnMut(web_sys::Event)>::new(move |_ev| {
        STATE.with(|s| s.borrow_mut().is_focused = true);
    });
    input_target
        .add_event_listener_with_callback("focus", focus_cb.as_ref().unchecked_ref())
        .map_err(|_| {
            build(
                Severity::Warn,
                SURFACE,
                "dom-listener",
                "attach focus listener failed",
                "input.add_event_listener(focus) failed",
            )
        })?;
    focus_cb.forget();

    let input_target = input_el.clone();
    let blur_cb = Closure::<dyn FnMut(web_sys::Event)>::new(move |_ev| {
        STATE.with(|s| s.borrow_mut().is_focused = false);
    });
    input_target
        .add_event_listener_with_callback("blur", blur_cb.as_ref().unchecked_ref())
        .map_err(|_| {
            build(
                Severity::Warn,
                SURFACE,
                "dom-listener",
                "attach blur listener failed",
                "input.add_event_listener(blur) failed",
            )
        })?;
    blur_cb.forget();

    let input_for_keydown = input_el.clone();
    let keydown_cb =
        Closure::<dyn FnMut(web_sys::KeyboardEvent)>::new(move |ev: web_sys::KeyboardEvent| {
            if ev.key() != "Enter" {
                return;
            }
            let value = input_for_keydown.value();
            if value.is_empty() {
                return;
            }
            input_for_keydown.set_value("");
            let trimmed = chat::trim_to_char_boundary(&value, MAX_BODY_BYTES);
            if let Err(err) = publish_chat(trimmed) {
                errpipe::emit(err);
                render_errors();
            }
        });
    input_el
        .add_event_listener_with_callback("keydown", keydown_cb.as_ref().unchecked_ref())
        .map_err(|_| {
            build(
                Severity::Warn,
                SURFACE,
                "dom-listener",
                "attach keydown listener failed",
                "input.add_event_listener(keydown) failed",
            )
        })?;
    keydown_cb.forget();

    STATE.with(|s| {
        let mut st = s.borrow_mut();
        st.messages_el = Some(messages_html);
        st.input_el = Some(input_el);
        st.login_button_el = Some(login_btn);
    });

    install_login_listener()?;
    Ok(())
}

fn inject_stylesheet(doc: &web_sys::Document) -> Result<(), LayeError> {
    let head = doc.head().ok_or_else(|| {
        build(
            Severity::Error,
            SURFACE,
            "dom-head",
            "document.head is None",
            "no head element in document",
        )
    })?;
    let style = create_element(doc, "style")?;
    style.set_text_content(Some(
        r#"
.laye-overlay {
    position: fixed; right: 16px; bottom: 16px; width: 320px; max-height: 360px;
    background: rgba(0,0,0,0.82); color: #eee;
    font: 12px ui-monospace, monospace; border-radius: 6px;
    display: flex; flex-direction: column;
    z-index: 2147483000; box-shadow: 0 4px 14px rgba(0,0,0,0.3);
}
.laye-header {
    padding: 6px 10px; border-bottom: 1px solid rgba(255,255,255,0.1);
    color: #aac; display: flex; justify-content: space-between;
    align-items: center; gap: 8px;
}
.laye-header-title { white-space: nowrap; }
.laye-login-button {
    background: rgba(255,255,255,0.06); color: #eee;
    border: 1px solid rgba(255,255,255,0.15); border-radius: 3px;
    padding: 2px 8px; font: inherit; cursor: pointer;
    max-width: 60%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.laye-login-button.laye-logged-in { color: #9c9; border-color: rgba(150,200,150,0.4); }
.laye-login-button:hover { background: rgba(255,255,255,0.1); }
.laye-messages {
    padding: 8px 10px; overflow-y: auto; flex: 1 1 auto;
    min-height: 120px; max-height: 240px;
}
.laye-msg { padding: 2px 0; }
.laye-msg .laye-who { color: #6cf; margin-right: 6px; }
.laye-msg.laye-self .laye-who { color: #9c9; }
.laye-input-row {
    border-top: 1px solid rgba(255,255,255,0.1); padding: 6px 8px;
}
.laye-input {
    width: 100%; background: rgba(255,255,255,0.06); color: #eee;
    border: 1px solid rgba(255,255,255,0.15); border-radius: 3px;
    padding: 4px 6px; font: inherit; box-sizing: border-box;
}
.laye-input:focus { outline: 1px solid #6cf; }

/* Sacred Error overlay (per ERROR.md visual contract). */
.laye-errors {
    position: fixed; top: 16px; right: 16px; width: 420px;
    z-index: 2147483100; display: flex; flex-direction: column; gap: 8px;
    pointer-events: none;
}
.laye-error {
    pointer-events: auto; color: #eee;
    border: 1px solid rgba(255,255,255,0.15); border-left-width: 4px;
    border-radius: 4px; padding: 8px 10px;
    font: 12px ui-monospace, monospace; white-space: pre-wrap;
    box-shadow: 0 4px 14px rgba(0,0,0,0.4);
    background: rgba(0,0,0,0.85);
}
.laye-error.laye-sev-info {
    background: rgba(20,20,40,0.9);
    border-color: rgba(136,136,255,0.3);
    border-left-color: #88f;
}
.laye-error.laye-sev-warn {
    background: #2a2010;
    border-color: rgba(252,204,102,0.35);
    border-left-color: #fc6;
}
.laye-error.laye-sev-error {
    background: #2a0c0c;
    border-color: #4a1414;
    border-left-color: #f66;
}
.laye-error.laye-sev-panic {
    background: #2a0c2a;
    border-color: #4a144a;
    border-left-color: #f0f;
}
.laye-error .laye-err-label { color: #888; }
.laye-error .laye-err-title { color: #fbb; }
.laye-error.laye-sev-warn  .laye-err-title { color: #fc6; }
.laye-error.laye-sev-info  .laye-err-title { color: #adf; }
.laye-error.laye-sev-panic .laye-err-title { color: #f0f; }
.laye-error .laye-err-why    { color: #ddd; }
.laye-error .laye-err-trace  { color: #aaa; padding-left: 12px; }
.laye-error .laye-err-dismiss {
    color: #888; cursor: pointer; float: right; margin-left: 8px;
}
.laye-error .laye-err-dismiss:hover { color: #ddd; }
.laye-error .laye-err-count {
    float: right; margin-left: 6px; color: #f66;
    font-weight: bold; font-size: 11px;
}
.laye-error .laye-err-reload {
    color: #f0f; margin-top: 6px;
}
"#,
    ));
    head.append_child(&style).map_err(|_| {
        build(
            Severity::Error,
            SURFACE,
            "dom-mount",
            "append stylesheet to head failed",
            "head.append_child returned Err",
        )
    })?;
    Ok(())
}

fn publish_chat(body: String) -> Result<(), LayeError> {
    let now = js_sys::Date::now() as u64;
    let bytes = STATE.with(|s| -> Result<Vec<u8>, LayeError> {
        let st = s.borrow();
        let net = st.net.as_ref().ok_or_else(|| {
            build(
                Severity::Error,
                SURFACE,
                "chat-publish-preinit",
                "chat publish before net init",
                "STATE.net is None",
            )
        })?;
        chat::build_signed_wire(net.keypair(), body.clone(), now).ok_or_else(|| {
            build(
                Severity::Error,
                SURFACE,
                "chat-sign",
                "chat signed-wire build failed",
                "build_signed_wire returned None",
            )
        })
    })?;
    if !has_mesh_for(CHAT_TOPIC) {
        enqueue_send(CHAT_TOPIC.to_string(), bytes);
    } else {
        let publish_result = STATE.with(|s| {
            let st = s.borrow();
            st.net
                .as_ref()
                .map(|net| net.publish(&Topic(CHAT_TOPIC.into()), &bytes))
        });
        match publish_result {
            Some(Ok(())) => {}
            Some(Err(e)) => {
                return Err(build(
                    Severity::Warn,
                    SURFACE,
                    "chat-publish-cmd",
                    "chat publish failed",
                    format!("{e}"),
                ));
            }
            None => {
                return Err(build(
                    Severity::Error,
                    SURFACE,
                    "chat-publish-preinit",
                    "chat publish before net init",
                    "STATE.net is None",
                ));
            }
        }
    }
    STATE.with(|s| {
        let mut st = s.borrow_mut();
        st.chat.push(ChatEntry {
            who: "me".to_string(),
            body,
        });
    });
    render_chat();
    Ok(())
}

fn render_chat() {
    let Ok(doc) = document() else { return };
    STATE.with(|s| {
        let st = s.borrow();
        let Some(messages_el) = st.messages_el.as_ref() else {
            return;
        };
        messages_el.set_inner_html("");
        for entry in st.chat.history.iter() {
            let Ok(row) = doc.create_element("div") else {
                continue;
            };
            let class = if entry.who == "me" {
                "laye-msg laye-self"
            } else {
                "laye-msg"
            };
            row.set_class_name(class);
            let Ok(who_span) = doc.create_element("span") else {
                continue;
            };
            who_span.set_class_name("laye-who");
            who_span.set_text_content(Some(&entry.who));
            let _ = row.append_child(&who_span);
            let Ok(body_span) = doc.create_element("span") else {
                continue;
            };
            body_span.set_text_content(Some(&entry.body));
            let _ = row.append_child(&body_span);
            let _ = messages_el.append_child(&row);
        }
        messages_el.set_scroll_top(messages_el.scroll_height());
    });
}

// ============================================================================
// Error overlay (Error.view equivalent) — the sacred render path
// ============================================================================

fn install_error_overlay() -> Result<(), LayeError> {
    let doc = document()?;
    let body = doc.body().ok_or_else(|| {
        build(
            Severity::Error,
            SURFACE,
            "dom-body",
            "document.body is None",
            "no body element in document",
        )
    })?;
    let list = create_element(&doc, "div")?;
    list.set_class_name("laye-errors");
    body.append_child(&list).map_err(|_| {
        build(
            Severity::Error,
            SURFACE,
            "dom-mount",
            "append error overlay to body failed",
            "body.append_child returned Err",
        )
    })?;
    let list_html: web_sys::HtmlElement =
        list.dyn_into::<web_sys::HtmlElement>().map_err(|_| {
            build(
                Severity::Error,
                SURFACE,
                "dom-cast",
                "error list cast failed",
                "list div would not cast to HtmlElement",
            )
        })?;
    STATE.with(|s| s.borrow_mut().error_list_el = Some(list_html));
    Ok(())
}

fn render_errors() {
    let errors = errpipe::drain();
    if errors.is_empty() {
        return;
    }
    let Ok(doc) = document() else { return };
    let list_el = STATE.with(|s| s.borrow().error_list_el.clone());
    let Some(list_el) = list_el else { return };
    for err in errors {
        let key = format!(
            "{}|{}|{}",
            err.context.surface,
            err.context.region.as_deref().unwrap_or(""),
            err.title,
        );
        let existing_id = STATE.with(|s| s.borrow().error_dedup.get(&key).cloned());
        if let Some(id) = existing_id {
            bump_error_counter(&doc, &id);
            continue;
        }
        let node = match render_one_error(&doc, &err) {
            Some(n) => n,
            None => continue,
        };
        let _ = list_el.append_child(&node);
        STATE.with(|s| {
            let mut st = s.borrow_mut();
            st.error_dedup.insert(key, err.id.clone());
            st.error_counts.insert(err.id.clone(), 1);
        });
    }
}

/// Bump the coalesced count on an existing error node. Also un-hides it
/// if the user previously dismissed it — a recurring failure re-surfaces.
fn bump_error_counter(doc: &web_sys::Document, dom_id: &str) {
    let new_count = STATE.with(|s| {
        let mut st = s.borrow_mut();
        let count = st.error_counts.entry(dom_id.to_string()).or_insert(1);
        *count += 1;
        *count
    });
    if let Some(el) = doc.get_element_by_id(dom_id) {
        if let Some(html) = el.dyn_ref::<web_sys::HtmlElement>() {
            let _ = html.style().set_property("display", "");
        }
        let counter_id = format!("{dom_id}--count");
        if let Some(counter_el) = doc.get_element_by_id(&counter_id) {
            counter_el.set_text_content(Some(&format!("×{new_count}")));
            if let Some(counter_html) = counter_el.dyn_ref::<web_sys::HtmlElement>() {
                let _ = counter_html
                    .style()
                    .set_property("visibility", "visible");
            }
        }
    }
}

fn render_one_error(doc: &web_sys::Document, err: &LayeError) -> Option<web_sys::Node> {
    let block = doc.create_element("div").ok()?;
    let sev = match err.severity {
        Severity::Info => "laye-sev-info",
        Severity::Warn => "laye-sev-warn",
        Severity::Error => "laye-sev-error",
        Severity::Panic => "laye-sev-panic",
    };
    let sev_label = match err.severity {
        Severity::Info => "info: ",
        Severity::Warn => "warn: ",
        Severity::Error => "error: ",
        Severity::Panic => "panic: ",
    };
    block.set_class_name(&format!("laye-error {sev}"));
    block.set_id(&err.id);

    let counter = doc.create_element("span").ok()?;
    counter.set_class_name("laye-err-count");
    counter.set_id(&format!("{}--count", err.id));
    counter.set_text_content(Some("×1"));
    if let Some(html) = counter.dyn_ref::<web_sys::HtmlElement>() {
        let _ = html.style().set_property("visibility", "hidden");
    }
    let _ = block.append_child(&counter);

    let dismiss = doc.create_element("span").ok()?;
    dismiss.set_class_name("laye-err-dismiss");
    dismiss.set_text_content(Some("[esc]"));
    let dismiss_id = err.id.clone();
    let dismiss_cb = Closure::<dyn FnMut(web_sys::Event)>::new(move |_ev| {
        if let Ok(doc) = document()
            && let Some(el) = doc.get_element_by_id(&dismiss_id)
            && let Some(html) = el.dyn_ref::<web_sys::HtmlElement>()
        {
            let _ = html.style().set_property("display", "none");
        }
    });
    let _ = dismiss.add_event_listener_with_callback(
        "click",
        dismiss_cb.as_ref().unchecked_ref(),
    );
    dismiss_cb.forget();
    let _ = block.append_child(&dismiss);

    let title = doc.create_element("div").ok()?;
    title.set_class_name("laye-err-title");
    let label = doc.create_element("span").ok()?;
    label.set_class_name("laye-err-label");
    label.set_text_content(Some(sev_label));
    let _ = title.append_child(&label);
    let title_text = doc.create_element("span").ok()?;
    title_text.set_text_content(Some(&err.title));
    let _ = title.append_child(&title_text);
    let _ = block.append_child(&title);

    let why = doc.create_element("div").ok()?;
    why.set_class_name("laye-err-why");
    let why_label = doc.create_element("span").ok()?;
    why_label.set_class_name("laye-err-label");
    why_label.set_text_content(Some("why: "));
    let _ = why.append_child(&why_label);
    let why_text = doc.create_element("span").ok()?;
    let region_str = err.context.region.as_deref().unwrap_or("(no region)");
    why_text.set_text_content(Some(&format!(
        "{} [{}/{}]",
        err.why, err.context.surface, region_str
    )));
    let _ = why.append_child(&why_text);
    let _ = block.append_child(&why);

    if !err.trace.is_empty() {
        let trace = doc.create_element("div").ok()?;
        trace.set_class_name("laye-err-trace");
        let trace_label = doc.create_element("span").ok()?;
        trace_label.set_class_name("laye-err-label");
        trace_label.set_text_content(Some("trace: "));
        let _ = trace.append_child(&trace_label);
        let trace_text = doc.create_element("span").ok()?;
        trace_text.set_text_content(Some(&err.trace.join(" ← ")));
        let _ = trace.append_child(&trace_text);
        let _ = block.append_child(&trace);
    }

    if err.requires_reload {
        let reload = doc.create_element("div").ok()?;
        reload.set_class_name("laye-err-reload");
        reload.set_text_content(Some("reload required"));
        let _ = block.append_child(&reload);
    }

    Some(block.into())
}

// ============================================================================
// Login flow (Mastodon popup + postMessage)
// ============================================================================

fn install_login_listener() -> Result<(), LayeError> {
    let window = web_sys::window().ok_or_else(|| {
        build(
            Severity::Error,
            SURFACE,
            "dom-window",
            "web_sys::window() returned None",
            "no window in this context",
        )
    })?;
    let cb =
        Closure::<dyn FnMut(web_sys::MessageEvent)>::new(move |ev: web_sys::MessageEvent| {
            if ev.origin() != BROKER_ORIGIN {
                return;
            }
            let Ok(json) = js_sys::JSON::stringify(&ev.data()) else {
                return;
            };
            let Some(json_str) = json.as_string() else {
                return;
            };
            if let Err(err) = handle_login_message(&json_str) {
                errpipe::emit(err);
                render_errors();
            }
        });
    window
        .add_event_listener_with_callback("message", cb.as_ref().unchecked_ref())
        .map_err(|_| {
            build(
                Severity::Warn,
                SURFACE,
                "dom-listener",
                "attach window.message listener failed",
                "window.add_event_listener(message) failed",
            )
        })?;
    cb.forget();
    Ok(())
}

fn open_login_popup() -> Result<(), LayeError> {
    let window = web_sys::window().ok_or_else(|| {
        build(
            Severity::Error,
            SURFACE,
            "dom-window",
            "web_sys::window() returned None",
            "no window in this context",
        )
    })?;
    let peer_pubkey_hex = STATE.with(|s| s.borrow().peer_id_hex.clone());
    if peer_pubkey_hex.is_empty() {
        return Err(build(
            Severity::Error,
            SURFACE,
            "login-preinit",
            "login attempted before init",
            "peer_id_hex is empty",
        ));
    }
    // Main tab generates the atproto flow's `state`. Popup and main tab
    // both know it; both can read the shared result via
    // `/me/sign/atproto/result?state=…`. This is how we survive
    // bsky.social's COOP header severing `window.opener` — the main tab
    // no longer needs postMessage to reach it.
    let state = random_state_hex();
    let url = format!(
        "{BROKER_ORIGIN}{BROKER_LOGIN_PATH}?peer={peer_pubkey_hex}&state={state}"
    );
    let popup = window
        .open_with_url_and_target_and_features(&url, POPUP_TARGET, POPUP_FEATURES)
        .map_err(|_| {
            build(
                Severity::Warn,
                SURFACE,
                "login-popup",
                "window.open threw",
                "window.open_with_url_and_target_and_features returned Err",
            )
        })?;
    if popup.is_none() {
        return Err(build(
            Severity::Warn,
            SURFACE,
            "login-popup-blocked",
            "browser blocked the login popup",
            "window.open returned None — check popup blocker",
        ));
    }
    spawn_local(poll_atproto_result(state));
    Ok(())
}

fn random_state_hex() -> String {
    let mut bytes = [0u8; 16];
    if let Err(e) = getrandom::fill(&mut bytes) {
        errpipe::emit(build(
            Severity::Warn,
            SURFACE,
            "state-random-fill",
            "getrandom::fill failed; atproto flow state will be all-zeros",
            format!("{e}"),
        ));
    }
    hex_lower(&bytes)
}

const ATPROTO_RESULT_URL: &str = "https://relaye.sbvh.nl/me/sign/atproto/result";

async fn poll_atproto_result(state: String) {
    // 5 minute cap matches FlowCache TTL server-side. 2s cadence.
    // First responder wins: whichever tab reads the result first triggers
    // handling; the endpoint doesn't drain on read (see oauth_atproto.rs
    // FlowCache::peek_result) so both popup + main tab can consume the
    // same binding without racing.
    for _ in 0..150 {
        wasm_bindgen_futures::JsFuture::from(js_sys::Promise::new(
            &mut |resolve, _reject| {
                if let Some(win) = web_sys::window()
                    && let Err(js_err) = win
                        .set_timeout_with_callback_and_timeout_and_arguments_0(
                            &resolve, 2000,
                        )
                {
                    errpipe::emit(build(
                        Severity::Warn,
                        SURFACE,
                        "atproto-poll-timer",
                        "set_timeout failed to schedule next atproto poll",
                        format!("{js_err:?}"),
                    ));
                }
            },
        ))
        .await
        .ok();
        let url = format!("{ATPROTO_RESULT_URL}?state={state}");
        let Some(win) = web_sys::window() else { continue };
        let fetch_promise = win.fetch_with_str(&url);
        let resp_val = match wasm_bindgen_futures::JsFuture::from(fetch_promise).await {
            Ok(v) => v,
            Err(_) => continue,
        };
        let resp: web_sys::Response = match resp_val.dyn_into() {
            Ok(r) => r,
            Err(_) => continue,
        };
        if resp.status() != 200 {
            continue;
        }
        let text_promise = match resp.text() {
            Ok(p) => p,
            Err(_) => continue,
        };
        let text_val = match wasm_bindgen_futures::JsFuture::from(text_promise).await {
            Ok(v) => v,
            Err(_) => continue,
        };
        let text = match text_val.as_string() {
            Some(t) => t,
            None => continue,
        };
        // Result endpoint returns the SignedBinding wire shape directly.
        // Wrap it in the same envelope handle_login_message expects so the
        // downstream path is identical to the postMessage flow.
        let envelope = format!("{{\"type\":\"laye/identity/link\",\"signed\":{text}}}");
        if let Err(err) = handle_login_message(&envelope) {
            errpipe::emit(err);
            render_errors();
        }
        return;
    }
}

fn handle_login_message(json_str: &str) -> Result<(), LayeError> {
    let msg: BrokerMessageWire = serde_json::from_str(json_str).map_err(|e| {
        build(
            Severity::Warn,
            SURFACE,
            "login-parse",
            "broker postMessage parse failed",
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
            "broker returned no signed binding",
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
    let self_pubkey = STATE.with(|s| s.borrow().self_pubkey).ok_or_else(|| {
        build(
            Severity::Error,
            SURFACE,
            "login-preinit",
            "login callback fired before self_pubkey is known",
            "state.self_pubkey is None",
        )
    })?;
    if binding.claim.peer_pubkey != self_pubkey {
        return Err(build(
            Severity::Warn,
            SURFACE,
            "login-wrong-peer",
            "broker signed a binding for a different peer",
            "claim.peer_pubkey != self_pubkey — dropping",
        ));
    }
    let provider = binding.claim.provider.clone();
    STATE.with(|s| {
        let mut st = s.borrow_mut();
        st.self_bindings.retain(|b| b.claim.provider != provider);
        st.self_bindings.insert(0, binding);
        let clone = st.self_bindings.clone();
        st.bindings.0.insert(self_pubkey, clone);
    });
    // login-bound is not a failure — the login button turning green with
    // the handle is the correct visible signal. Emitting anything here
    // would put a success confirmation in the sacred failure surface.

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
                    render_errors();
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
                render_errors();
            }
        }
    });
    republish_self_bindings();
    update_login_button();
    render_chat();
    render_errors();
    Ok(())
}

fn republish_self_bindings() {
    let (encoded, has_bindings) = STATE.with(|s| {
        let st = s.borrow();
        if st.self_bindings.is_empty() {
            return (Err(String::new()), false);
        }
        (
            identity::encode_wire(&st.self_bindings).map_err(|e| format!("{e}")),
            true,
        )
    });
    if !has_bindings {
        return;
    }
    let bytes = match encoded {
        Ok(b) => b,
        Err(why) => {
            errpipe::emit(build(
                Severity::Warn,
                SURFACE,
                "identity-encode",
                "self bindings encode failed",
                why,
            ));
            return;
        }
    };
    if !has_mesh_for(IDENTITY_TOPIC) {
        enqueue_send(IDENTITY_TOPIC.to_string(), bytes);
        return;
    }
    let publish_result = STATE.with(|s| {
        s.borrow()
            .net
            .as_ref()
            .map(|net| net.publish(&Topic(IDENTITY_TOPIC.into()), &bytes))
    });
    match publish_result {
        Some(Ok(())) | None => {}
        Some(Err(e)) => {
            errpipe::emit(build(
                Severity::Warn,
                SURFACE,
                "identity-republish",
                "self identity republish failed",
                format!("{e}"),
            ));
        }
    }
}

fn update_login_button() {
    let handle: Option<String> = STATE.with(|s| {
        let st = s.borrow();
        let self_pk = st.self_pubkey?;
        st.bindings.resolve_handle(&self_pk).map(String::from)
    });
    STATE.with(|s| {
        let st = s.borrow();
        let Some(btn) = st.login_button_el.as_ref() else {
            return;
        };
        match &handle {
            Some(h) => {
                btn.set_text_content(Some(h));
                let _ = btn
                    .unchecked_ref::<web_sys::Element>()
                    .class_list()
                    .add_1("laye-logged-in");
            }
            None => {
                btn.set_text_content(Some("log in"));
                let _ = btn
                    .unchecked_ref::<web_sys::Element>()
                    .class_list()
                    .remove_1("laye-logged-in");
            }
        }
    });
}

// ============================================================================
// DOM helpers (typed Error surface)
// ============================================================================

fn create_element(doc: &web_sys::Document, tag: &str) -> Result<web_sys::Element, LayeError> {
    doc.create_element(tag).map_err(|_| {
        build(
            Severity::Error,
            SURFACE,
            "dom-create",
            "document.createElement failed",
            format!("tag={tag}"),
        )
    })
}

fn append_child(
    parent: &web_sys::Element,
    child: &web_sys::Element,
) -> Result<(), LayeError> {
    parent.append_child(child).map(|_| ()).map_err(|_| {
        build(
            Severity::Warn,
            SURFACE,
            "dom-append",
            "element.appendChild failed",
            format!("parent={} child={}", parent.tag_name(), child.tag_name()),
        )
    })
}

// ============================================================================
// IndexedDB helpers
// ============================================================================

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

async fn idb_load_bindings(
    db: &web_sys::IdbDatabase,
) -> Result<Vec<SignedBinding>, String> {
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
    let json =
        serde_json::to_vec(bindings).map_err(|e| format!("bindings encode: {e}"))?;
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
    let onsuccess =
        Closure::<dyn FnMut(web_sys::Event)>::new(move |_ev: web_sys::Event| {
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
    let onerror =
        Closure::<dyn FnMut(web_sys::Event)>::new(move |_ev: web_sys::Event| {
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

    rx.await
        .map_err(|_| "IDB oneshot canceled".to_string())?
}
