use std::collections::VecDeque;
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use anyhow::Context;
use tokio::io::AsyncWriteExt;
use tracing::info;

pub const STATS_HISTORY_LEN: usize = 60;

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

pub async fn run(
    public_host: String,
    public_port: u16,
    libp2p_port: u16,
    peer_id: String,
    stats: Arc<Mutex<RelayeStats>>,
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
                tracing::warn!(error = %e, "status_page accept error");
                continue;
            }
        };
        let peer_id = peer_id.clone();
        let stats = stats.clone();
        tokio::spawn(async move {
            if let Err(e) = handle_conn(socket, libp2p_port, &peer_id, &stats).await {
                tracing::debug!(error = %e, "status_page connection ended with error");
            }
        });
    }
}

async fn handle_conn(
    mut socket: tokio::net::TcpStream,
    libp2p_port: u16,
    peer_id: &str,
    stats: &Arc<Mutex<RelayeStats>>,
) -> std::io::Result<()> {
    let mut peek_buf = vec![0u8; 8192];
    let n = socket.peek(&mut peek_buf).await?;
    if n == 0 {
        return Ok(());
    }
    if looks_like_websocket_upgrade(&peek_buf[..n]) {
        let mut upstream =
            tokio::net::TcpStream::connect(("127.0.0.1", libp2p_port)).await?;
        tokio::io::copy_bidirectional(&mut socket, &mut upstream).await?;
    } else {
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
        let response = format_status_response(&body);
        socket.write_all(&response).await?;
        socket.shutdown().await?;
    }
    Ok(())
}

fn looks_like_websocket_upgrade(bytes: &[u8]) -> bool {
    let needle = b"upgrade: websocket";
    let lower: Vec<u8> = bytes.iter().map(|b| b.to_ascii_lowercase()).collect();
    lower.windows(needle.len()).any(|w| w == needle)
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

    #[test]
    fn classifies_firefox_ws_upgrade() {
        let req = b"GET / HTTP/1.1\r\nHost: relaye.sbvh.nl\r\nUpgrade: websocket\r\n\
Connection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: x\r\n\r\n";
        assert!(looks_like_websocket_upgrade(req));
    }

    #[test]
    fn classifies_lowercase_upgrade_header() {
        let req = b"get / http/1.1\r\nhost: relaye.sbvh.nl\r\nupgrade: websocket\r\n\r\n";
        assert!(looks_like_websocket_upgrade(req));
    }

    #[test]
    fn rejects_plain_http_get() {
        let req = b"GET / HTTP/1.1\r\nHost: relaye.sbvh.nl\r\nUser-Agent: curl/8\r\n\r\n";
        assert!(!looks_like_websocket_upgrade(req));
    }

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
        assert_eq!(format_uptime(Duration::from_secs(2 * 86400 + 3 * 3600 + 14 * 60)), "2d 03h 14m");
    }

    #[test]
    fn http_response_well_formed() {
        let r = format_status_response("<html>hi</html>");
        let s = String::from_utf8(r).unwrap();
        assert!(s.starts_with("HTTP/1.1 200 OK\r\n"));
        assert!(s.contains("Content-Length: 15"));
        assert!(s.contains("Content-Type: text/html"));
    }
}
