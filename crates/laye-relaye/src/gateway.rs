//! WS gateway — per-topic bridge between browser WS clients and
//! the gossipsub mesh. Spec: `crates/relaye/docs/gateway.md`.

use std::collections::{HashMap, VecDeque};
use std::sync::{Arc, Mutex, Weak};

use tokio::sync::Notify;

/// Command sent from a WS gateway client to the main swarm event
/// loop. The swarm owns the gossipsub behaviour; the gateway
/// doesn't touch it directly.
#[derive(Debug)]
pub enum GatewayCmd {
    Publish { topic: String, bytes: Vec<u8> },
}

const GATEWAY_SINK_CAPACITY: usize = 128;

/// Handle one accepted gateway connection: WS handshake, then a
/// bidirectional pump. Incoming binary frames become
/// `GatewayCmd::Publish`; outgoing frames come from the sink the
/// registry gave us. Either direction failing collapses both.
pub async fn handle_client(
    socket: tokio::net::TcpStream,
    topic: String,
    registry: TopicRegistry,
    cmd_tx: tokio::sync::mpsc::UnboundedSender<GatewayCmd>,
) -> Result<(), tokio_tungstenite::tungstenite::Error> {
    use futures::{SinkExt, StreamExt};
    use tokio_tungstenite::tungstenite::Message;

    let ws = tokio_tungstenite::accept_async(socket).await?;
    let (mut ws_sink, mut ws_stream) = ws.split();
    let sink = registry.subscribe(&topic, GATEWAY_SINK_CAPACITY);

    let read_topic = topic.clone();
    let read_registry = registry.clone();
    let read = async {
        while let Some(msg) = ws_stream.next().await {
            let Ok(m) = msg else { return };
            let bytes = match m {
                Message::Binary(b) => b,
                Message::Close(_) => return,
                _ => continue, // ignore text/ping/pong for MVP
            };
            // Fan out to same-node WS subscribers immediately. libp2p
            // gossipsub does not deliver own-published messages back
            // to the publishing node's Message event, so this local
            // broadcast is what makes two clients on the same relaye
            // share bytes. Remote nodes get it via the gossipsub
            // publish below → their handle_event → their registry.
            read_registry.broadcast(&read_topic, &bytes);
            let cmd = GatewayCmd::Publish {
                topic: read_topic.clone(),
                bytes,
            };
            if cmd_tx.send(cmd).is_err() {
                return;
            }
        }
    };

    let write = async {
        loop {
            let bytes = sink.next().await;
            if ws_sink.send(Message::Binary(bytes)).await.is_err() {
                return;
            }
        }
    };

    tokio::select! {
        _ = read => {}
        _ = write => {}
    }
    Ok(())
}

/// One subscriber's outbox: a bounded ring of enqueued frames + a
/// wakeup notifier. Spec: "if a sink's buffered outbox is full,
/// drop the oldest queued frame — position updates are lossy by
/// nature." The drop-oldest behavior lives in `push`; the WS-write
/// task on the other end awaits `next()`.
pub struct BroadcastSink {
    cap: usize,
    queue: Mutex<VecDeque<Vec<u8>>>,
    notify: Notify,
}

impl BroadcastSink {
    fn new(cap: usize) -> Self {
        Self {
            cap,
            queue: Mutex::new(VecDeque::with_capacity(cap)),
            notify: Notify::new(),
        }
    }

    /// Enqueue a frame. When the queue is at capacity, drop the
    /// oldest queued frame — position updates are lossy and the
    /// freshest is always what a slow reader should get.
    fn push(&self, bytes: Vec<u8>) {
        let mut q = self.queue.lock().unwrap_or_else(|p| p.into_inner());
        if q.len() >= self.cap {
            q.pop_front();
        }
        q.push_back(bytes);
        self.notify.notify_one();
    }

    /// Pop the next frame, awaiting a notification if the queue is
    /// empty. The WS-write task loops on this.
    pub async fn next(&self) -> Vec<u8> {
        loop {
            {
                let mut q = self.queue.lock().unwrap_or_else(|p| p.into_inner());
                if let Some(bytes) = q.pop_front() {
                    return bytes;
                }
            }
            self.notify.notified().await;
        }
    }

    /// Non-blocking peek + pop. Returns `None` if the queue is
    /// empty. Used in tests and — later — in a poll-driven pipe.
    #[allow(dead_code)]
    pub fn try_next(&self) -> Option<Vec<u8>> {
        let mut q = self.queue.lock().unwrap_or_else(|p| p.into_inner());
        q.pop_front()
    }
}

/// Per-topic map of subscriber sinks. Registry holds `Weak`
/// references so a subscriber dropping its `Arc<BroadcastSink>`
/// implicitly unregisters — the next broadcast prunes the dead
/// entry from the Vec, and `subscriber_count` filters live sinks
/// by strong count.
///
/// Shared between the gossipsub event loop (`broadcast`) and each
/// WS accept task (`subscribe`). One process lifetime, in-memory
/// only, dies with the process.
#[derive(Clone, Default)]
pub struct TopicRegistry {
    inner: Arc<Mutex<HashMap<String, Vec<Weak<BroadcastSink>>>>>,
}

impl TopicRegistry {
    pub fn new() -> Self {
        Self::default()
    }

    /// Register a new subscriber for a topic. Returns the
    /// `Arc<BroadcastSink>` — caller pops from it via `.next()` on
    /// the WS-write side.
    pub fn subscribe(&self, topic: &str, cap: usize) -> Arc<BroadcastSink> {
        let sink = Arc::new(BroadcastSink::new(cap));
        let weak = Arc::downgrade(&sink);
        let mut map = self.inner.lock().unwrap_or_else(|p| p.into_inner());
        map.entry(topic.to_string()).or_default().push(weak);
        sink
    }

    /// Fan bytes out to every live sink registered for a topic.
    /// Dead weak references (subscriber dropped their Arc) get
    /// pruned in place.
    pub fn broadcast(&self, topic: &str, bytes: &[u8]) {
        let mut map = self.inner.lock().unwrap_or_else(|p| p.into_inner());
        let Some(sinks) = map.get_mut(topic) else {
            return;
        };
        sinks.retain(|weak| {
            if let Some(sink) = weak.upgrade() {
                sink.push(bytes.to_vec());
                true
            } else {
                false
            }
        });
    }

    /// Number of live sinks currently registered for a topic —
    /// filtered by `strong_count > 0` so dropped subscribers stop
    /// counting immediately, whether or not a broadcast has run
    /// since.
    #[allow(dead_code)]
    pub fn subscriber_count(&self, topic: &str) -> usize {
        let map = self.inner.lock().unwrap_or_else(|p| p.into_inner());
        map.get(topic)
            .map(|v| v.iter().filter(|w| w.strong_count() > 0).count())
            .unwrap_or(0)
    }
}

/// Classification of an incoming TCP connection based on the
/// peeked bytes. Consumed by `status_page::handle_conn` to pick
/// which handler runs.
#[derive(Debug, PartialEq, Eq)]
pub enum WsUpgradeRoute {
    /// WS upgrade to `/` — hand off to the libp2p loopback.
    Libp2p,
    /// WS upgrade to `/ws/<topic>` where `topic` is allow-listed.
    Gateway { topic: String },
    /// WS upgrade to a path we don't route (404 candidate).
    UnknownPath,
    /// Not a WS upgrade at all — fall to the HTTP router.
    NotUpgrade,
}

/// True if the peeked bytes contain a WebSocket upgrade header
/// (case-insensitive `upgrade: websocket`). Cheap-enough
/// case-fold: lowercase every byte then window-scan for the
/// needle. Called by `classify_ws_upgrade` and — post cycle 3b —
/// by `status_page::handle_conn` via that classifier.
pub fn looks_like_websocket_upgrade(bytes: &[u8]) -> bool {
    let needle = b"upgrade: websocket";
    let lower: Vec<u8> = bytes.iter().map(|b| b.to_ascii_lowercase()).collect();
    lower.windows(needle.len()).any(|w| w == needle)
}

/// Classify a peeked incoming request into one of the four
/// connection routes. Combines the WS-upgrade sniff, the request-
/// target parse, and an allow-list check for `/ws/<topic>` paths.
///
/// Returns `NotUpgrade` when the bytes are not a WS upgrade OR
/// when the request line hasn't fully landed yet — either way the
/// caller falls through to the HTTP router.
pub fn classify_ws_upgrade(peek: &[u8], allow_list: &[&str]) -> WsUpgradeRoute {
    if !looks_like_websocket_upgrade(peek) {
        return WsUpgradeRoute::NotUpgrade;
    }
    let Some(target) = parse_request_target(peek) else {
        return WsUpgradeRoute::NotUpgrade;
    };
    if target == "/" {
        return WsUpgradeRoute::Libp2p;
    }
    if let Some(topic) = target.strip_prefix("/ws/")
        && allow_list.contains(&topic)
    {
        return WsUpgradeRoute::Gateway {
            topic: topic.to_string(),
        };
    }
    WsUpgradeRoute::UnknownPath
}

/// Build an HTTP 404 response for a WS upgrade to a path we
/// don't route (`UnknownPath`).
pub fn build_404_response() -> Vec<u8> {
    let body = "not found\n";
    format!(
        "HTTP/1.1 404 Not Found\r\nContent-Type: text/plain; charset=utf-8\r\n\
Content-Length: {}\r\nCache-Control: no-store\r\nConnection: close\r\n\r\n{}",
        body.len(),
        body,
    )
    .into_bytes()
}

/// Build an HTTP 501 response. Kept for future use (e.g., during
/// a controlled rollback of the gateway). The reason string ends
/// up in the body.
#[allow(dead_code)]
pub fn build_501_response(reason: &str) -> Vec<u8> {
    let body = format!("{reason}\n");
    format!(
        "HTTP/1.1 501 Not Implemented\r\nContent-Type: text/plain; charset=utf-8\r\n\
Content-Length: {}\r\nCache-Control: no-store\r\nConnection: close\r\n\r\n{}",
        body.len(),
        body,
    )
    .into_bytes()
}

/// Extract the request-target (path) from a raw peeked HTTP
/// request. Returns `None` if the first line hasn't fully
/// arrived (`\r\n` not yet seen), if the buffer isn't UTF-8, or
/// if the first line doesn't have the shape `METHOD TARGET
/// VERSION`.
///
/// Used by the connection-accept fork in `status_page` to decide
/// between the libp2p WSS path (`/`) and a gateway WSS path
/// (`/ws/<topic>`). Kept pure and byte-agnostic — the WS-upgrade
/// header check stays where it is; this only pulls the target.
pub fn parse_request_target(peek: &[u8]) -> Option<&str> {
    let first_crlf = peek.windows(2).position(|w| w == b"\r\n")?;
    let line = std::str::from_utf8(&peek[..first_crlf]).ok()?;
    let mut parts = line.splitn(3, ' ');
    let _method = parts.next()?;
    let target = parts.next()?;
    let _version = parts.next()?;
    Some(target)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn broadcast_reaches_all_subscribers_on_the_topic() {
        let reg = TopicRegistry::new();
        let sink_a = reg.subscribe("/ws/positions", 8);
        let sink_b = reg.subscribe("/ws/positions", 8);

        reg.broadcast("/ws/positions", b"hello");

        assert_eq!(sink_a.next().await, b"hello".to_vec());
        assert_eq!(sink_b.next().await, b"hello".to_vec());
    }

    #[tokio::test]
    async fn broadcast_does_not_reach_subscribers_on_a_different_topic() {
        let reg = TopicRegistry::new();
        let sink_pos = reg.subscribe("/ws/positions", 8);
        let sink_chat = reg.subscribe("/ws/chat", 8);

        reg.broadcast("/ws/positions", b"pos");

        assert_eq!(sink_pos.next().await, b"pos".to_vec());
        assert!(
            sink_chat.try_next().is_none(),
            "chat should not have received"
        );
    }

    #[tokio::test]
    async fn dropped_subscribers_stop_counting() {
        let reg = TopicRegistry::new();
        let sink_a = reg.subscribe("/ws/positions", 8);
        let _sink_b = reg.subscribe("/ws/positions", 8);
        assert_eq!(reg.subscriber_count("/ws/positions"), 2);

        drop(sink_a);

        assert_eq!(
            reg.subscriber_count("/ws/positions"),
            1,
            "dropped subscriber should stop counting",
        );
    }

    const ALLOW_LIST: &[&str] = &["rave-positions/v1", "rave-chat/v1", "laye-identity/v1"];

    #[test]
    fn build_404_response_is_a_well_formed_http_404() {
        let out = build_404_response();
        let s = std::str::from_utf8(&out).expect("utf8");
        assert!(s.starts_with("HTTP/1.1 404 Not Found\r\n"));
        assert!(s.contains("Content-Length: "));
        assert!(s.contains("Connection: close"));
    }

    #[test]
    fn build_501_response_is_a_well_formed_http_501_carrying_the_reason() {
        let out = build_501_response("gateway handler not implemented");
        let s = std::str::from_utf8(&out).expect("utf8");
        assert!(s.starts_with("HTTP/1.1 501 Not Implemented\r\n"));
        assert!(s.contains("Content-Length: "));
        assert!(s.contains("Connection: close"));
        assert!(s.contains("gateway handler not implemented"));
    }

    #[test]
    fn looks_like_ws_upgrade_normal_case_header() {
        let req = b"GET / HTTP/1.1\r\nHost: relaye.sbvh.nl\r\nUpgrade: websocket\r\n\
Connection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: x\r\n\r\n";
        assert!(looks_like_websocket_upgrade(req));
    }

    #[test]
    fn looks_like_ws_upgrade_lowercase_header() {
        let req = b"get / http/1.1\r\nhost: relaye.sbvh.nl\r\nupgrade: websocket\r\n\r\n";
        assert!(looks_like_websocket_upgrade(req));
    }

    #[test]
    fn looks_like_ws_upgrade_rejects_plain_http_get() {
        let req = b"GET / HTTP/1.1\r\nHost: relaye.sbvh.nl\r\nUser-Agent: curl/8\r\n\r\n";
        assert!(!looks_like_websocket_upgrade(req));
    }

    fn ws_upgrade_bytes(target: &str) -> Vec<u8> {
        format!(
            "GET {target} HTTP/1.1\r\n\
Host: relaye.sbvh.nl\r\n\
Upgrade: websocket\r\n\
Connection: Upgrade\r\n\
Sec-WebSocket-Version: 13\r\n\
Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n"
        )
        .into_bytes()
    }

    #[test]
    fn classify_ws_upgrade_root_is_libp2p() {
        assert_eq!(
            classify_ws_upgrade(&ws_upgrade_bytes("/"), ALLOW_LIST),
            WsUpgradeRoute::Libp2p,
        );
    }

    #[test]
    fn classify_ws_upgrade_allow_listed_topic_is_gateway() {
        assert_eq!(
            classify_ws_upgrade(&ws_upgrade_bytes("/ws/rave-positions/v1"), ALLOW_LIST),
            WsUpgradeRoute::Gateway {
                topic: "rave-positions/v1".to_string()
            },
        );
    }

    #[test]
    fn classify_ws_upgrade_unlisted_topic_is_unknown_path() {
        assert_eq!(
            classify_ws_upgrade(&ws_upgrade_bytes("/ws/some-other-topic"), ALLOW_LIST),
            WsUpgradeRoute::UnknownPath,
        );
    }

    #[test]
    fn classify_ws_upgrade_non_ws_get_falls_through_to_http() {
        let plain_get = b"GET / HTTP/1.1\r\nHost: relaye.sbvh.nl\r\nUser-Agent: curl/8\r\n\r\n";
        assert_eq!(
            classify_ws_upgrade(plain_get, ALLOW_LIST),
            WsUpgradeRoute::NotUpgrade,
        );
    }

    #[test]
    fn classify_ws_upgrade_post_me_sign_falls_through_to_http() {
        let post = b"POST /me/sign HTTP/1.1\r\nHost: relaye.sbvh.nl\r\n\
Content-Type: application/json\r\nContent-Length: 2\r\n\r\n{}";
        assert_eq!(
            classify_ws_upgrade(post, ALLOW_LIST),
            WsUpgradeRoute::NotUpgrade,
        );
    }

    #[test]
    fn parse_request_target_extracts_gateway_topic_path() {
        let req = b"GET /ws/positions HTTP/1.1\r\n\
Host: relaye.sbvh.nl\r\n\
Upgrade: websocket\r\n\
Connection: Upgrade\r\n\r\n";
        assert_eq!(parse_request_target(req), Some("/ws/positions"));
    }

    #[test]
    fn parse_request_target_extracts_root_for_libp2p_upgrade() {
        let req = b"GET / HTTP/1.1\r\nHost: relaye.sbvh.nl\r\nUpgrade: websocket\r\n\r\n";
        assert_eq!(parse_request_target(req), Some("/"));
    }

    #[test]
    fn parse_request_target_returns_none_when_first_line_incomplete() {
        assert_eq!(parse_request_target(b"GET / HTTP/1.1"), None);
        assert_eq!(parse_request_target(b""), None);
    }

    #[test]
    fn parse_request_target_returns_none_on_malformed_request_line() {
        assert_eq!(parse_request_target(b"garbage\r\n"), None);
        assert_eq!(parse_request_target(b"GET\r\n"), None);
    }

    #[tokio::test]
    async fn backpressure_drops_the_oldest_queued_frame_at_capacity() {
        // Spec: "if a sink's buffered outbox is full, drop the
        // oldest queued frame". Position updates are lossy — the
        // freshest 3 are what a slow client should get, not the
        // stalest 3.
        let reg = TopicRegistry::new();
        let sink = reg.subscribe("/ws/positions", 3);
        for i in 0..5u8 {
            reg.broadcast("/ws/positions", &[i]);
        }

        // Queue holds frames [2, 3, 4] — frames 0 and 1 dropped.
        assert_eq!(sink.try_next(), Some(vec![2]));
        assert_eq!(sink.try_next(), Some(vec![3]));
        assert_eq!(sink.try_next(), Some(vec![4]));
        assert_eq!(sink.try_next(), None);
    }
}
