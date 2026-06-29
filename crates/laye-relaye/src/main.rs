use std::time::{Duration, Instant};

use anyhow::{Context, Result};
use libp2p::{
    Multiaddr, PeerId, Swarm, SwarmBuilder, core::transport::Transport, futures::StreamExt,
    gossipsub, identify, identity::Keypair, noise, ping, relay, swarm::SwarmEvent, websocket,
    yamux,
};
use tracing::{info, warn};

mod base64;
mod metrics;

use metrics::{Metrics, StdoutSink, read_proc_memory_rss};

const DEFAULT_IDENTIFY_PROTOCOL: &str = "/laye/1.0.0";
const DEFAULT_LISTEN_HOST: &str = "0.0.0.0";
const DEFAULT_LISTEN_PORT: u16 = 9001;
const DEFAULT_METRICS_INTERVAL_SECS: u64 = 60;
const MAX_RELAY_RESERVATIONS: usize = 128;
const GOSSIPSUB_HEARTBEAT: Duration = Duration::from_secs(1);
const IDLE_CONNECTION_TIMEOUT: Duration = Duration::from_secs(60);

#[derive(libp2p::swarm::NetworkBehaviour)]
struct RelayeBehaviour {
    gossipsub: gossipsub::Behaviour,
    identify: identify::Behaviour,
    ping: ping::Behaviour,
    relay: relay::Behaviour,
}

#[derive(Default)]
struct Stats {
    connections: u64,
    total_msgs: u64,
}

#[tokio::main]
async fn main() -> Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| tracing_subscriber::EnvFilter::new("info,relaye=debug")),
        )
        .init();

    let listen_host =
        std::env::var("RELAYE_LISTEN_HOST").unwrap_or_else(|_| DEFAULT_LISTEN_HOST.to_string());
    let listen_port: u16 = std::env::var("RELAYE_LISTEN_PORT")
        .ok()
        .map(|s| s.parse().context("RELAYE_LISTEN_PORT must be a u16"))
        .transpose()?
        .unwrap_or(DEFAULT_LISTEN_PORT);
    let identify_protocol = std::env::var("RELAYE_IDENTIFY_PROTOCOL")
        .unwrap_or_else(|_| DEFAULT_IDENTIFY_PROTOCOL.to_string());
    let topics = parse_topics();
    let metrics_interval_secs: u64 = std::env::var("RELAYE_METRICS_INTERVAL_SECS")
        .ok()
        .and_then(|s| s.parse().ok())
        .unwrap_or(DEFAULT_METRICS_INTERVAL_SECS);

    let keypair = load_identity()?;
    let local_peer_id = PeerId::from(keypair.public());
    info!(peer_id = %local_peer_id, "identity loaded");

    let mut swarm: Swarm<RelayeBehaviour> = SwarmBuilder::with_existing_identity(keypair)
        .with_tokio()
        .with_other_transport(|key| {
            websocket::Config::new(libp2p::tcp::tokio::Transport::new(
                libp2p::tcp::Config::default(),
            ))
            .upgrade(libp2p::core::upgrade::Version::V1)
            .authenticate(noise::Config::new(key).expect("noise config"))
            .multiplex(yamux::Config::default())
        })?
        .with_behaviour(|key| {
            let gossipsub_config = gossipsub::ConfigBuilder::default()
                .heartbeat_interval(GOSSIPSUB_HEARTBEAT)
                .validation_mode(gossipsub::ValidationMode::Strict)
                .build()
                .expect("gossipsub config");
            let gossipsub = gossipsub::Behaviour::new(
                gossipsub::MessageAuthenticity::Signed(key.clone()),
                gossipsub_config,
            )
            .expect("gossipsub behaviour");

            let identify = identify::Behaviour::new(identify::Config::new(
                identify_protocol.clone(),
                key.public(),
            ));

            let ping = ping::Behaviour::new(ping::Config::new());

            let relay = relay::Behaviour::new(
                key.public().to_peer_id(),
                relay::Config {
                    max_reservations: MAX_RELAY_RESERVATIONS,
                    ..Default::default()
                },
            );

            RelayeBehaviour {
                gossipsub,
                identify,
                ping,
                relay,
            }
        })?
        .with_swarm_config(|c| c.with_idle_connection_timeout(IDLE_CONNECTION_TIMEOUT))
        .build();

    for topic in &topics {
        let t = gossipsub::IdentTopic::new(topic);
        swarm
            .behaviour_mut()
            .gossipsub
            .subscribe(&t)
            .with_context(|| format!("subscribe {topic}"))?;
        info!(topic = %topic, "subscribed");
    }

    let listen_addr: Multiaddr = format!("/ip4/{listen_host}/tcp/{listen_port}/ws")
        .parse()
        .context("listen multiaddr")?;
    swarm.listen_on(listen_addr.clone())?;
    info!(addr = %listen_addr, "listening");

    let metrics_sink: Box<dyn Metrics> = Box::new(StdoutSink);
    let start = Instant::now();
    let mut stats = Stats::default();
    let mut last_msgs: u64 = 0;
    let mut metrics_interval =
        tokio::time::interval(Duration::from_secs(metrics_interval_secs));
    metrics_interval.tick().await;

    loop {
        tokio::select! {
            event = swarm.select_next_some() => handle_event(event, &mut stats),
            _ = metrics_interval.tick() => {
                let msgs_delta = stats.total_msgs.saturating_sub(last_msgs);
                last_msgs = stats.total_msgs;
                let rate = msgs_delta as f64 / metrics_interval_secs as f64;
                metrics_sink.gauge("relaye_connections", stats.connections as f64);
                metrics_sink.gauge("relaye_msgs_per_sec", rate);
                metrics_sink.gauge("relaye_uptime_secs", start.elapsed().as_secs_f64());
                metrics_sink.gauge("relaye_mem_rss_bytes", read_proc_memory_rss() as f64);
                metrics_sink.counter("relaye_messages_total", msgs_delta);
            }
            _ = tokio::signal::ctrl_c() => {
                info!("ctrl-c — shutting down");
                break;
            }
        }
    }

    Ok(())
}

/// Identity resolution order: RELAYE_IDENTITY_FILE, then
/// RELAYE_IDENTITY_BYTES (base64), then fresh-mint.
fn load_identity() -> Result<Keypair> {
    if let Some(raw) = std::env::var_os("RELAYE_IDENTITY_FILE") {
        let path = std::path::PathBuf::from(&raw);
        if path.exists() {
            let bytes = std::fs::read(&path)
                .with_context(|| format!("read identity file {path:?}"))?;
            return laye_me::load(&bytes)
                .map_err(|e| anyhow::anyhow!("decode identity from file: {e}"));
        }
        info!(path = ?path, "RELAYE_IDENTITY_FILE missing — minting and persisting");
        let keypair = laye_me::fresh();
        let bytes = laye_me::to_bytes(&keypair)
            .map_err(|e| anyhow::anyhow!("encode fresh identity: {e}"))?;
        if let Some(parent) = path.parent() {
            std::fs::create_dir_all(parent)
                .with_context(|| format!("create identity dir {parent:?}"))?;
        }
        std::fs::write(&path, &bytes)
            .with_context(|| format!("write identity file {path:?}"))?;
        return Ok(keypair);
    }
    if let Ok(b64) = std::env::var("RELAYE_IDENTITY_BYTES") {
        let bytes =
            base64::decode(&b64).context("RELAYE_IDENTITY_BYTES base64 decode")?;
        return laye_me::load(&bytes)
            .map_err(|e| anyhow::anyhow!("decode identity from env: {e}"));
    }
    info!("no RELAYE_IDENTITY_FILE / RELAYE_IDENTITY_BYTES — minting fresh");
    Ok(laye_me::fresh())
}

fn parse_topics() -> Vec<String> {
    std::env::var("RELAYE_TOPICS")
        .ok()
        .map(|s| {
            s.split(',')
                .map(|t| t.trim().to_string())
                .filter(|t| !t.is_empty())
                .collect()
        })
        .unwrap_or_default()
}

fn handle_event(event: SwarmEvent<RelayeBehaviourEvent>, stats: &mut Stats) {
    match event {
        SwarmEvent::NewListenAddr { address, .. } => {
            info!(addr = %address, "new listen address");
        }
        SwarmEvent::ConnectionEstablished {
            peer_id, endpoint, ..
        } => {
            stats.connections = stats.connections.saturating_add(1);
            info!(peer = %peer_id, ?endpoint, conns = stats.connections, "connection:open");
        }
        SwarmEvent::ConnectionClosed {
            peer_id, cause, ..
        } => {
            stats.connections = stats.connections.saturating_sub(1);
            info!(peer = %peer_id, cause = ?cause, conns = stats.connections, "connection:close");
        }
        SwarmEvent::IncomingConnectionError { error, .. } => {
            warn!(error = ?error, "incoming connection error");
        }
        SwarmEvent::OutgoingConnectionError { peer_id, error, .. } => {
            warn!(peer = ?peer_id, error = ?error, "outgoing connection error");
        }
        SwarmEvent::Behaviour(RelayeBehaviourEvent::Gossipsub(gossipsub::Event::Message {
            ..
        })) => {
            stats.total_msgs = stats.total_msgs.saturating_add(1);
        }
        SwarmEvent::Behaviour(RelayeBehaviourEvent::Gossipsub(gossipsub::Event::Subscribed {
            peer_id,
            topic,
        })) => {
            info!(peer = %peer_id, topic = %topic, "subscription +");
        }
        SwarmEvent::Behaviour(RelayeBehaviourEvent::Gossipsub(
            gossipsub::Event::Unsubscribed { peer_id, topic },
        )) => {
            info!(peer = %peer_id, topic = %topic, "subscription -");
        }
        _ => {}
    }
}
