use std::collections::VecDeque;
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use anyhow::Context;
use laye_me::Keypair;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tracing::{info, warn};

use crate::oauth_atproto::{self, ClientConfig, FlowCache};
use crate::sign_endpoint;

pub const STATS_HISTORY_LEN: usize = 60;
const MAX_HTTP_REQUEST_BYTES: usize = 64 * 1024;
const GATEWAY_ALLOW_LIST: &[&str] = &[
    "rave-positions/v1",
    "rave-chat/v1",
    "laye-chat/v1",
    "laye-identity/v1",
];

pub struct RelayeStats {
    pub start: Instant,
    pub peer_count: u64,
    pub conn_count: u64,
    pub total_conns_accepted: u64,
    pub total_msgs_relayed: u64,
    pub peer_history: VecDeque<u64>,
    pub msg_rate_history: VecDeque<f64>,
}

impl Default for RelayeStats {
    fn default() -> Self {
        Self {
            start: Instant::now(),
            peer_count: 0,
            conn_count: 0,
            total_conns_accepted: 0,
            total_msgs_relayed: 0,
            peer_history: VecDeque::with_capacity(STATS_HISTORY_LEN),
            msg_rate_history: VecDeque::with_capacity(STATS_HISTORY_LEN),
        }
    }
}

impl RelayeStats {
    pub fn uptime(&self) -> Duration {
        self.start.elapsed()
    }

    pub fn push_sample(&mut self, msg_rate: f64) {
        if self.peer_history.len() == STATS_HISTORY_LEN {
            self.peer_history.pop_front();
        }
        self.peer_history.push_back(self.peer_count);
        if self.msg_rate_history.len() == STATS_HISTORY_LEN {
            self.msg_rate_history.pop_front();
        }
        self.msg_rate_history.push_back(msg_rate);
    }
}

struct StatsSnapshot {
    uptime: Duration,
    peer_count: u64,
    conn_count: u64,
    total_conns_accepted: u64,
    total_msgs_relayed: u64,
    peer_history: Vec<u64>,
    msg_rate_history: Vec<f64>,
}

#[allow(clippy::too_many_arguments)]
pub async fn run(
    public_host: String,
    public_port: u16,
    libp2p_port: u16,
    peer_id: String,
    signing_keypair: Keypair,
    stats: Arc<Mutex<RelayeStats>>,
    registry: crate::gateway::TopicRegistry,
    gateway_cmd_tx: tokio::sync::mpsc::UnboundedSender<crate::gateway::GatewayCmd>,
    oauth_client: Arc<ClientConfig>,
    flow_cache: FlowCache,
) -> anyhow::Result<()> {
    let bind_addr = format!("{public_host}:{public_port}");
    let listener = tokio::net::TcpListener::bind(&bind_addr)
        .await
        .with_context(|| format!("status_page bind {bind_addr}"))?;
    info!(addr = %bind_addr, libp2p_port, "status_page listening");
    loop {
        let (socket, _peer) = match listener.accept().await {
            Ok(p) => p,
            Err(e) => {
                warn!(error = %e, "status_page accept error");
                continue;
            }
        };
        let peer_id = peer_id.clone();
        let stats = stats.clone();
        let signing_keypair = signing_keypair.clone();
        let registry = registry.clone();
        let gateway_cmd_tx = gateway_cmd_tx.clone();
        let oauth_client = oauth_client.clone();
        let flow_cache = flow_cache.clone();
        tokio::spawn(async move {
            if let Err(e) = handle_conn(
                socket,
                libp2p_port,
                &peer_id,
                &signing_keypair,
                &stats,
                registry,
                gateway_cmd_tx,
                oauth_client,
                flow_cache,
            )
            .await
            {
                tracing::debug!(error = %e, "status_page connection ended with error");
            }
        });
    }
}

/// How long a client gets to finish sending its request headers.
const HEADER_PEEK_DEADLINE: std::time::Duration = std::time::Duration::from_secs(5);

/// How long to wait before peeking again when the headers are still partial.
const HEADER_PEEK_PAUSE: std::time::Duration = std::time::Duration::from_millis(5);

/// Peek until the header terminator is in the buffer. One peek returns
/// whatever a single TCP segment carried, and classifying on that cannot tell
/// "not an upgrade" from "the Upgrade header has not arrived yet".
async fn peek_headers(
    socket: &mut tokio::net::TcpStream,
    buf: &mut [u8],
) -> std::io::Result<usize> {
    let deadline = tokio::time::Instant::now() + HEADER_PEEK_DEADLINE;
    loop {
        let n = socket.peek(buf).await?;
        if n == 0 {
            return Ok(0);
        }
        if find_header_end(&buf[..n]).is_some() || n == buf.len() {
            return Ok(n);
        }
        if tokio::time::Instant::now() >= deadline {
            // Classify on what did arrive rather than dropping the connection:
            // a client sending no terminator is malformed either way.
            return Ok(n);
        }
        // The bytes stay in the socket, so this polls rather than waits —
        // readable() is already true whenever anything is buffered.
        tokio::time::sleep(HEADER_PEEK_PAUSE).await;
    }
}

fn find_header_end(bytes: &[u8]) -> Option<usize> {
    bytes.windows(4).position(|w| w == b"\r\n\r\n")
}

#[allow(clippy::too_many_arguments)]
async fn handle_conn(
    mut socket: tokio::net::TcpStream,
    libp2p_port: u16,
    peer_id: &str,
    signing_keypair: &Keypair,
    stats: &Arc<Mutex<RelayeStats>>,
    registry: crate::gateway::TopicRegistry,
    gateway_cmd_tx: tokio::sync::mpsc::UnboundedSender<crate::gateway::GatewayCmd>,
    oauth_client: Arc<ClientConfig>,
    flow_cache: FlowCache,
) -> std::io::Result<()> {
    let mut peek_buf = vec![0u8; 8192];
    let n = peek_headers(&mut socket, &mut peek_buf).await?;
    if n == 0 {
        return Ok(());
    }
    match crate::gateway::classify_ws_upgrade(&peek_buf[..n], GATEWAY_ALLOW_LIST) {
        crate::gateway::WsUpgradeRoute::Libp2p => {
            let mut upstream = tokio::net::TcpStream::connect(("127.0.0.1", libp2p_port)).await?;
            tokio::io::copy_bidirectional(&mut socket, &mut upstream).await?;
            return Ok(());
        }
        crate::gateway::WsUpgradeRoute::Gateway { topic } => {
            if let Err(e) =
                crate::gateway::handle_client(socket, topic.clone(), registry, gateway_cmd_tx).await
            {
                tracing::debug!(topic = %topic, error = %e, "gateway client ended with error");
            }
            return Ok(());
        }
        crate::gateway::WsUpgradeRoute::UnknownPath => {
            let resp = crate::gateway::build_404_response();
            socket.write_all(&resp).await?;
            socket.shutdown().await?;
            return Ok(());
        }
        crate::gateway::WsUpgradeRoute::NotUpgrade => {}
    }

    let request = match read_full_request(&mut socket).await? {
        Some(r) => r,
        None => return Ok(()),
    };

    let response = route(
        &request,
        peer_id,
        signing_keypair,
        stats,
        &oauth_client,
        &flow_cache,
    )
    .await;
    socket.write_all(&response).await?;
    socket.shutdown().await?;
    Ok(())
}

struct ParsedRequest {
    method: String,
    path: String,
    body: Vec<u8>,
}

async fn read_full_request(
    socket: &mut tokio::net::TcpStream,
) -> std::io::Result<Option<ParsedRequest>> {
    let mut buf = Vec::with_capacity(4096);
    let mut tmp = [0u8; 4096];
    loop {
        let n = socket.read(&mut tmp).await?;
        if n == 0 {
            break;
        }
        buf.extend_from_slice(&tmp[..n]);
        if let Some(head_end) = find_double_crlf(&buf) {
            let content_length = parse_content_length(&buf[..head_end]);
            let body_start = head_end + 4;
            let need = content_length.unwrap_or(0);
            let have = buf.len().saturating_sub(body_start);
            if have >= need {
                return Ok(parse_request(&buf, head_end, body_start, need));
            }
        }
        if buf.len() > MAX_HTTP_REQUEST_BYTES {
            return Err(std::io::Error::new(
                std::io::ErrorKind::InvalidData,
                "HTTP request exceeds size limit",
            ));
        }
    }
    Ok(None)
}

fn parse_request(
    buf: &[u8],
    head_end: usize,
    body_start: usize,
    body_len: usize,
) -> Option<ParsedRequest> {
    let head = std::str::from_utf8(&buf[..head_end]).ok()?;
    let first = head.split("\r\n").next()?;
    let mut parts = first.splitn(3, ' ');
    let method = parts.next()?.to_string();
    let path = parts.next()?.to_string();
    let body = buf[body_start..body_start + body_len].to_vec();
    Some(ParsedRequest { method, path, body })
}

fn find_double_crlf(buf: &[u8]) -> Option<usize> {
    let needle = b"\r\n\r\n";
    buf.windows(needle.len()).position(|w| w == needle)
}

fn parse_content_length(head: &[u8]) -> Option<usize> {
    let head_str = std::str::from_utf8(head).ok()?;
    for line in head_str.split("\r\n") {
        let Some(colon) = line.find(':') else {
            continue;
        };
        let name = line[..colon].trim();
        if name.eq_ignore_ascii_case("content-length") {
            let value = line[colon + 1..].trim();
            return value.parse::<usize>().ok();
        }
    }
    None
}

async fn route(
    request: &ParsedRequest,
    peer_id: &str,
    signing_keypair: &Keypair,
    stats: &Arc<Mutex<RelayeStats>>,
    oauth_client: &Arc<ClientConfig>,
    flow_cache: &FlowCache,
) -> Vec<u8> {
    let (path, query) = split_path_and_query(&request.path);
    match (request.method.as_str(), path) {
        ("POST", "/me/sign/mastodon") => handle_sign_route(&request.body, signing_keypair).await,
        ("POST", "/me/oauth/atproto/start") => {
            handle_atproto_start_route(&request.body, oauth_client, flow_cache).await
        }
        ("GET", "/me/callback/atproto") => {
            handle_atproto_callback_route(&query, oauth_client, flow_cache, signing_keypair).await
        }
        ("GET", "/me/sign/atproto/result") => handle_atproto_result_route(&query, flow_cache).await,
        _ => handle_status_route(peer_id, stats),
    }
}

fn split_path_and_query(raw: &str) -> (&str, std::collections::HashMap<String, String>) {
    let mut params = std::collections::HashMap::new();
    let (path, qs) = match raw.split_once('?') {
        Some((p, q)) => (p, q),
        None => (raw, ""),
    };
    for pair in qs.split('&') {
        if pair.is_empty() {
            continue;
        }
        let (k, v) = match pair.split_once('=') {
            Some((k, v)) => (k, v),
            None => (pair, ""),
        };
        params.insert(percent_decode(k), percent_decode(v));
    }
    (path, params)
}

fn percent_decode(s: &str) -> String {
    let bytes = s.as_bytes();
    let mut out = Vec::with_capacity(bytes.len());
    let mut i = 0;
    while i < bytes.len() {
        if bytes[i] == b'+' {
            out.push(b' ');
            i += 1;
        } else if bytes[i] == b'%' && i + 2 < bytes.len() {
            let hi = hex_nibble(bytes[i + 1]);
            let lo = hex_nibble(bytes[i + 2]);
            match (hi, lo) {
                (Some(h), Some(l)) => {
                    out.push((h << 4) | l);
                    i += 3;
                }
                _ => {
                    out.push(bytes[i]);
                    i += 1;
                }
            }
        } else {
            out.push(bytes[i]);
            i += 1;
        }
    }
    String::from_utf8(out).unwrap_or_default()
}

fn hex_nibble(c: u8) -> Option<u8> {
    match c {
        b'0'..=b'9' => Some(c - b'0'),
        b'a'..=b'f' => Some(10 + c - b'a'),
        b'A'..=b'F' => Some(10 + c - b'A'),
        _ => None,
    }
}

async fn handle_sign_route(body: &[u8], signing_keypair: &Keypair) -> Vec<u8> {
    match sign_endpoint::handle_sign(body, signing_keypair).await {
        Ok(json_bytes) => build_json_response(200, &json_bytes),
        Err(e) => {
            let status = e.http_status();
            let body = serde_json::json!({ "error": e.message() });
            let bytes = serde_json::to_vec(&body).unwrap_or_else(|_| b"{}".to_vec());
            build_json_response(status, &bytes)
        }
    }
}

async fn handle_atproto_start_route(
    body: &[u8],
    oauth_client: &Arc<ClientConfig>,
    flow_cache: &FlowCache,
) -> Vec<u8> {
    match oauth_atproto::handle_start(body, oauth_client, flow_cache).await {
        Ok(json_bytes) => build_json_response(200, &json_bytes),
        Err(e) => flow_error_response(e),
    }
}

async fn handle_atproto_callback_route(
    query: &std::collections::HashMap<String, String>,
    oauth_client: &Arc<ClientConfig>,
    flow_cache: &FlowCache,
    relay_signing_key: &Keypair,
) -> Vec<u8> {
    match oauth_atproto::handle_callback(query, oauth_client, flow_cache, relay_signing_key).await {
        Ok(location) => build_redirect_response(&location),
        Err(e) => flow_error_response(e),
    }
}

async fn handle_atproto_result_route(
    query: &std::collections::HashMap<String, String>,
    flow_cache: &FlowCache,
) -> Vec<u8> {
    match oauth_atproto::handle_result(query, flow_cache).await {
        Ok(json_bytes) => build_json_response(200, &json_bytes),
        Err(e) => flow_error_response(e),
    }
}

fn flow_error_response(e: oauth_atproto::FlowError) -> Vec<u8> {
    let status = e.http_status();
    let body = serde_json::json!({ "error": e.to_string() });
    let bytes = serde_json::to_vec(&body).unwrap_or_else(|_| b"{}".to_vec());
    build_json_response(status, &bytes)
}

/// A header value ends at the first CR or LF, so anything past one is a header
/// the caller wrote. Truncating there is what keeps this one response.
fn header_safe(value: &str) -> &str {
    match value.find(['\r', '\n']) {
        Some(cut) => &value[..cut],
        None => value,
    }
}

/// The same string lands in markup, where the danger is different.
fn html_escaped(value: &str) -> String {
    let mut out = String::with_capacity(value.len());
    for c in value.chars() {
        match c {
            '&' => out.push_str("&amp;"),
            '<' => out.push_str("&lt;"),
            '>' => out.push_str("&gt;"),
            '"' => out.push_str("&quot;"),
            '\'' => out.push_str("&#39;"),
            _ => out.push(c),
        }
    }
    out
}

fn build_redirect_response(location: &str) -> Vec<u8> {
    let location = header_safe(location);
    let shown = html_escaped(location);
    let body = format!("<html><body>redirecting to <a href=\"{shown}\">{shown}</a></body></html>");
    format!(
        "HTTP/1.1 302 Found\r\nLocation: {location}\r\n\
Content-Type: text/html; charset=utf-8\r\n\
Content-Length: {}\r\nCache-Control: no-store\r\nConnection: close\r\n\r\n{}",
        body.len(),
        body,
    )
    .into_bytes()
}

fn handle_status_route(peer_id: &str, stats: &Arc<Mutex<RelayeStats>>) -> Vec<u8> {
    let snapshot = {
        let s = stats.lock().unwrap_or_else(|p| p.into_inner());
        StatsSnapshot {
            uptime: s.uptime(),
            peer_count: s.peer_count,
            conn_count: s.conn_count,
            total_conns_accepted: s.total_conns_accepted,
            total_msgs_relayed: s.total_msgs_relayed,
            peer_history: s.peer_history.iter().copied().collect(),
            msg_rate_history: s.msg_rate_history.iter().copied().collect(),
        }
    };
    let body = build_status_html(peer_id, &snapshot);
    format_status_response(&body)
}

fn build_json_response(status: u16, body: &[u8]) -> Vec<u8> {
    let reason = match status {
        200 => "OK",
        400 => "Bad Request",
        401 => "Unauthorized",
        404 => "Not Found",
        500 => "Internal Server Error",
        502 => "Bad Gateway",
        _ => "Response",
    };
    // CORS on every /me/* JSON response so the main tab at laye.sbvh.nl
    // can poll cross-origin. Same story game.sbvh.nl needed via the
    // previous laye_p2p_cors CloudFront policy — folded into the server
    // now so it's one origin serving one contract.
    let mut out = format!(
        "HTTP/1.1 {status} {reason}\r\nContent-Type: application/json; charset=utf-8\r\n\
Content-Length: {}\r\nCache-Control: no-store\r\n\
Access-Control-Allow-Origin: *\r\n\
Access-Control-Allow-Methods: GET, POST, OPTIONS\r\n\
Access-Control-Allow-Headers: Content-Type\r\nConnection: close\r\n\r\n",
        body.len(),
    )
    .into_bytes();
    out.extend_from_slice(body);
    out
}

fn build_status_html(peer_id: &str, snap: &StatsSnapshot) -> String {
    let version = env!("CARGO_PKG_VERSION");
    let uptime = format_uptime(snap.uptime);
    let peers_spark = render_sparkline_u64(&snap.peer_history);
    let msgs_spark = render_sparkline_f64(&snap.msg_rate_history);
    let msg_rate_now = snap.msg_rate_history.last().copied().unwrap_or(0.0);
    format!(
        "<!doctype html><html lang=en><head><meta charset=utf-8>\
<title>relaye</title>\
<style>body{{font:14px/1.5 ui-monospace,Menlo,monospace;max-width:42em;\
margin:3em auto;padding:0 1em;color:#ddd;background:#111}}\
a{{color:#6cf}}code{{background:#222;padding:0 .3em}}\
h1{{font-size:1.2em;margin:0 0 1em}}p{{margin:.5em 0}}\
table{{border-collapse:collapse;margin:1em 0;width:100%}}\
th,td{{padding:.3em .6em;border-bottom:1px solid #222;text-align:left}}\
th{{color:#9ad;font-weight:normal;width:14em}}\
.spark{{height:1.6em;vertical-align:middle}}\
.spark path{{fill:none;stroke:#6cf;stroke-width:1.4}}\
.spark .bg{{fill:#1a1a1a;stroke:none}}\
.muted{{color:#888;font-size:.9em}}</style></head><body>\
<h1>relaye</h1>\
<p>libp2p relay for the laye stack.</p>\
<p>WebSocket: <code>wss://relaye.sbvh.nl/</code></p>\
<table>\
<tr><th>PeerId</th><td><code>{peer_id}</code></td></tr>\
<tr><th>Version</th><td>relaye {version}</td></tr>\
<tr><th>Uptime</th><td>{uptime}</td></tr>\
<tr><th>Connected peers</th><td>{peers_now} (open conns: {conns_now})</td></tr>\
<tr><th>Peers (1h)</th><td>{peers_spark}</td></tr>\
<tr><th>Pubsub msgs/s</th><td>{rate_now:.2}</td></tr>\
<tr><th>Pubsub rate (1h)</th><td>{msgs_spark}</td></tr>\
<tr><th>Connections accepted</th><td>{total_conns}</td></tr>\
<tr><th>Messages relayed</th><td>{total_msgs}</td></tr>\
</table>\
<p class=muted>Sparklines cover the last hour at 60s cadence.</p>\
<p>Source: <a href=\"https://github.com/teranos/laye/tree/main/crates/relaye\">\
github.com/teranos/laye/crates/relaye</a></p>\
</body></html>",
        peer_id = peer_id,
        version = version,
        uptime = uptime,
        peers_now = snap.peer_count,
        conns_now = snap.conn_count,
        peers_spark = peers_spark,
        rate_now = msg_rate_now,
        msgs_spark = msgs_spark,
        total_conns = snap.total_conns_accepted,
        total_msgs = snap.total_msgs_relayed,
    )
}

fn format_uptime(d: Duration) -> String {
    let total_secs = d.as_secs();
    if total_secs < 60 {
        return "<1m".into();
    }
    let days = total_secs / 86_400;
    let hours = (total_secs % 86_400) / 3_600;
    let mins = (total_secs % 3_600) / 60;
    if days > 0 {
        format!("{days}d {hours:02}h {mins:02}m")
    } else if hours > 0 {
        format!("{hours}h {mins:02}m")
    } else {
        format!("{mins}m")
    }
}

fn render_sparkline_u64(samples: &[u64]) -> String {
    let as_f64: Vec<f64> = samples.iter().map(|&v| v as f64).collect();
    render_sparkline_f64(&as_f64)
}

fn render_sparkline_f64(samples: &[f64]) -> String {
    let width = 240.0_f64;
    let height = 24.0_f64;
    if samples.is_empty() {
        return format!(
            "<svg class=spark viewBox=\"0 0 {w} {h}\" width=\"{w}\" \
height=\"{h}\" preserveAspectRatio=\"none\">\
<rect class=bg width=\"{w}\" height=\"{h}\"/>\
<text x=\"{tx}\" y=\"{ty}\" fill=\"#666\" font-size=\"10\" \
text-anchor=\"middle\">no data yet</text></svg>",
            w = width,
            h = height,
            tx = width / 2.0,
            ty = height / 2.0 + 3.0,
        );
    }
    let min = samples.iter().cloned().fold(f64::INFINITY, f64::min);
    let max = samples.iter().cloned().fold(f64::NEG_INFINITY, f64::max);
    let range = (max - min).max(1e-9);
    let slots = STATS_HISTORY_LEN.max(samples.len()) as f64;
    let step = width / (slots - 1.0).max(1.0);
    let leading_blank = (slots as usize).saturating_sub(samples.len());
    let mut d = String::new();
    for (i, v) in samples.iter().enumerate() {
        let x = (leading_blank + i) as f64 * step;
        let y = if max <= min {
            height / 2.0
        } else {
            2.0 + (height - 4.0) * (1.0 - (v - min) / range)
        };
        if i == 0 {
            d.push_str(&format!("M{x:.1},{y:.1}"));
        } else {
            d.push_str(&format!(" L{x:.1},{y:.1}"));
        }
    }
    format!(
        "<svg class=spark viewBox=\"0 0 {w} {h}\" width=\"{w}\" \
height=\"{h}\" preserveAspectRatio=\"none\">\
<rect class=bg width=\"{w}\" height=\"{h}\"/>\
<path d=\"{d}\"/></svg>",
        w = width,
        h = height,
        d = d,
    )
}

fn format_status_response(body: &str) -> Vec<u8> {
    format!(
        "HTTP/1.1 200 OK\r\nContent-Type: text/html; charset=utf-8\r\n\
Content-Length: {}\r\nCache-Control: max-age=60\r\nConnection: close\r\n\r\n{}",
        body.len(),
        body
    )
    .into_bytes()
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;

    fn fake_snapshot() -> StatsSnapshot {
        StatsSnapshot {
            uptime: Duration::from_secs(3725),
            peer_count: 4,
            conn_count: 5,
            total_conns_accepted: 42,
            total_msgs_relayed: 9001,
            peer_history: vec![1, 2, 3, 4],
            msg_rate_history: vec![0.5, 1.25, 2.0, 1.0],
        }
    }

    /// A header value ends at the first CR or LF. Anything after one is a
    /// second response the caller wrote into ours.
    #[test]
    fn a_redirect_cannot_carry_its_own_headers() {
        let response =
            build_redirect_response("/me/?atproto_result=x\r\nSet-Cookie: stolen=1\r\n\r\nHI");
        let text = String::from_utf8_lossy(&response);

        assert!(!text.contains("Set-Cookie"));
        assert!(!text.contains("HI"));
        assert!(text.contains("Location: /me/?atproto_result=x\r\n"));
    }

    /// The header is a URL and the body is markup. Only the body is a place
    /// where an angle bracket becomes an element.
    #[test]
    fn a_redirect_cannot_carry_markup() {
        let response = build_redirect_response("/me/?atproto_result=<script>alert(1)</script>");
        let text = String::from_utf8_lossy(&response);
        let (_, body) = text.split_once("\r\n\r\n").unwrap();

        assert!(!body.contains("<script>"));
        assert!(body.contains("&lt;script&gt;"));
    }

    /// Content-Length has to describe the body that was written, or the next
    /// response on the connection starts inside this one.
    #[test]
    fn a_redirect_body_matches_its_length() {
        let response = build_redirect_response("/me/?atproto_result=<x>&y=\"z\"");
        let text = String::from_utf8_lossy(&response);

        let (head, body) = text.split_once("\r\n\r\n").unwrap();
        let declared: usize = head
            .lines()
            .find_map(|l| l.strip_prefix("Content-Length: "))
            .unwrap()
            .trim()
            .parse()
            .unwrap();
        assert_eq!(declared, body.len());
    }

    #[test]
    fn header_end_is_found_only_when_complete() {
        assert!(find_header_end(b"GET / HTTP/1.1\r\nHost: x\r\n").is_none());
        assert!(find_header_end(b"GET / HTTP/1.1\r\nHost: x\r\n\r\n").is_some());
    }

    #[test]
    fn status_html_embeds_peer_id_and_stats() {
        let html = build_status_html("12D3KooWtestPeer", &fake_snapshot());
        assert!(html.contains("12D3KooWtestPeer"));
        assert!(html.contains("wss://relaye.sbvh.nl/"));
        assert!(html.contains("github.com/teranos/laye"));
        assert!(html.contains("42"));
        assert!(html.contains("9001"));
        assert!(html.contains("<svg class=spark"));
    }

    #[test]
    fn uptime_formats() {
        assert_eq!(format_uptime(Duration::from_secs(30)), "<1m");
        assert_eq!(format_uptime(Duration::from_secs(120)), "2m");
        assert_eq!(format_uptime(Duration::from_secs(3725)), "1h 02m");
        assert_eq!(
            format_uptime(Duration::from_secs(2 * 86400 + 3 * 3600 + 14 * 60)),
            "2d 03h 14m"
        );
    }

    #[test]
    fn http_response_well_formed() {
        let r = format_status_response("<html>hi</html>");
        let s = String::from_utf8(r).unwrap();
        assert!(s.starts_with("HTTP/1.1 200 OK\r\n"));
        assert!(s.contains("Content-Length: 15"));
        assert!(s.contains("Content-Type: text/html"));
    }

    #[test]
    fn parse_request_line_extracts_method_and_path() {
        let req = b"POST /me/sign HTTP/1.1\r\nHost: relaye.sbvh.nl\r\n\
Content-Type: application/json\r\nContent-Length: 5\r\n\r\nhello";
        let head_end = find_double_crlf(req).expect("double crlf present");
        let body_start = head_end + 4;
        let body_len = parse_content_length(&req[..head_end]).expect("content-length");
        let parsed = parse_request(req, head_end, body_start, body_len).expect("parses");
        assert_eq!(parsed.method, "POST");
        assert_eq!(parsed.path, "/me/sign");
        assert_eq!(parsed.body, b"hello");
    }

    #[test]
    fn parse_content_length_case_insensitive() {
        let head = b"POST / HTTP/1.1\r\nHost: x\r\ncontent-length: 42\r\n";
        assert_eq!(parse_content_length(head), Some(42));
    }

    #[test]
    fn json_response_header_lines_present() {
        let out = build_json_response(400, b"{\"error\":\"nope\"}");
        let s = String::from_utf8(out).unwrap();
        assert!(s.starts_with("HTTP/1.1 400 Bad Request\r\n"));
        assert!(s.contains("Content-Type: application/json"));
        assert!(s.contains("Content-Length: 16"));
        assert!(s.contains("Cache-Control: no-store"));
    }
}
