//! libp2p Swarm construction + drive task. Cfg-gated transport per target;
//! everything else is shared.

use crate::{NetError, NetEvent};
use futures::channel::mpsc;
use futures::{StreamExt, select_biased};
use laye_protocol::{PeerId, Topic};
use libp2p::core::transport::ListenerId;
use libp2p::core::transport::Transport as _;
use libp2p::core::upgrade;
use libp2p::swarm::{NetworkBehaviour, SwarmEvent};
use libp2p::{Swarm, SwarmBuilder, gossipsub, identify, ping};
use libp2p_connection_limits as connection_limits;
use std::collections::HashSet;
use std::sync::{Arc, Mutex};
use std::time::Duration;

#[derive(NetworkBehaviour)]
pub(crate) struct LayeBehaviour {
    pub(crate) gossipsub: gossipsub::Behaviour,
    pub(crate) identify: identify::Behaviour,
    pub(crate) ping: ping::Behaviour,
    pub(crate) connection_limits: connection_limits::Behaviour,
}

/// How many peers a listening peer holds at once, and how many it lets
/// handshake at once.
const MAX_INCOMING: u32 = 256;
const MAX_PENDING_INCOMING: u32 = 64;

pub(crate) enum Cmd {
    Publish { topic: Topic, bytes: Vec<u8> },
    Subscribe(Topic),
    Unsubscribe(Topic),
}

pub(crate) fn build_swarm(
    keypair: libp2p::identity::Keypair,
    identify_protocol: String,
    listening: bool,
) -> Result<Swarm<LayeBehaviour>, NetError> {
    let gossipsub_config = gossipsub::ConfigBuilder::default()
        .heartbeat_interval(Duration::from_secs(1))
        .validation_mode(gossipsub::ValidationMode::Strict)
        .build()
        .map_err(|e| NetError::ProviderInternal {
            reason: format!("gossipsub config: {e}"),
        })?;
    let gossipsub_b = gossipsub::Behaviour::new(
        gossipsub::MessageAuthenticity::Signed(keypair.clone()),
        gossipsub_config,
    )
    .map_err(|e| NetError::ProviderInternal {
        reason: format!("gossipsub behaviour: {e}"),
    })?;

    let identify_b =
        identify::Behaviour::new(identify::Config::new(identify_protocol, keypair.public()));

    let ping_b = ping::Behaviour::new(ping::Config::new().with_interval(Duration::from_secs(5)));

    // "relaye is dead. Make laye-net able to accept connections"
    //
    // A client keeps one connection out and takes none in. A peer that
    // listens is what others connect to, so it takes them, up to a number
    // that a stream of fresh keypairs cannot get past.
    let conn_limits = if listening {
        connection_limits::ConnectionLimits::default()
            .with_max_established_per_peer(Some(1))
            .with_max_established_incoming(Some(MAX_INCOMING))
            .with_max_pending_incoming(Some(MAX_PENDING_INCOMING))
            .with_max_established(Some(MAX_INCOMING))
    } else {
        connection_limits::ConnectionLimits::default()
            .with_max_established_outgoing(Some(1))
            .with_max_established_incoming(Some(0))
            .with_max_established_per_peer(Some(1))
            .with_max_pending_outgoing(Some(2))
    };
    let connection_limits_b = connection_limits::Behaviour::new(conn_limits);

    let behaviour = LayeBehaviour {
        gossipsub: gossipsub_b,
        identify: identify_b,
        ping: ping_b,
        connection_limits: connection_limits_b,
    };

    build_swarm_with_transport(keypair, behaviour)
}

#[cfg(target_arch = "wasm32")]
fn build_swarm_with_transport(
    keypair: libp2p::identity::Keypair,
    behaviour: LayeBehaviour,
) -> Result<Swarm<LayeBehaviour>, NetError> {
    let noise = libp2p::noise::Config::new(&keypair).map_err(|e| NetError::ProviderInternal {
        reason: format!("noise config from keypair: {e}"),
    })?;
    let swarm = SwarmBuilder::with_existing_identity(keypair)
        .with_wasm_bindgen()
        .with_other_transport(move |_key| {
            let ws = libp2p::websocket_websys::Transport::default()
                .upgrade(upgrade::Version::V1)
                .authenticate(noise)
                .multiplex(libp2p::yamux::Config::default())
                .map(|(p, m), _| (p, libp2p::core::muxing::StreamMuxerBox::new(m)));
            ws.boxed()
        })
        .map_err(|e| NetError::ProviderInternal {
            reason: format!("swarm transport: {e}"),
        })?
        .with_behaviour(|_key| behaviour)
        .map_err(|e| NetError::ProviderInternal {
            reason: format!("swarm behaviour: {e}"),
        })?
        .with_swarm_config(|c| c.with_idle_connection_timeout(Duration::from_secs(60)))
        .with_connection_timeout(Duration::from_secs(60))
        .build();
    Ok(swarm)
}

#[cfg(not(target_arch = "wasm32"))]
fn build_swarm_with_transport(
    keypair: libp2p::identity::Keypair,
    behaviour: LayeBehaviour,
) -> Result<Swarm<LayeBehaviour>, NetError> {
    let noise = libp2p::noise::Config::new(&keypair).map_err(|e| NetError::ProviderInternal {
        reason: format!("noise config from keypair: {e}"),
    })?;
    let swarm = SwarmBuilder::with_existing_identity(keypair)
        .with_tokio()
        .with_other_transport(move |_key| {
            libp2p::websocket::Config::new(libp2p::tcp::tokio::Transport::new(
                libp2p::tcp::Config::default(),
            ))
            .upgrade(upgrade::Version::V1)
            .authenticate(noise)
            .multiplex(libp2p::yamux::Config::default())
        })
        .map_err(|e| NetError::ProviderInternal {
            reason: format!("swarm transport: {e}"),
        })?
        .with_behaviour(|_key| behaviour)
        .map_err(|e| NetError::ProviderInternal {
            reason: format!("swarm behaviour: {e}"),
        })?
        .with_swarm_config(|c| c.with_idle_connection_timeout(Duration::from_secs(60)))
        .build();
    Ok(swarm)
}

pub(crate) async fn drive_swarm(
    mut swarm: Swarm<LayeBehaviour>,
    mut cmd_rx: mpsc::UnboundedReceiver<Cmd>,
    events: Arc<Mutex<Vec<NetEvent>>>,
    bootstrap_addrs: Vec<String>,
    listen_addrs: Vec<String>,
    initial_topics: Vec<Topic>,
) {
    // The listeners still open. A peer that was asked to listen and holds
    // none is one nobody can reach, and says so once.
    let mut live: HashSet<ListenerId> = HashSet::new();
    let mut why_none = Vec::new();
    for addr_str in &listen_addrs {
        let listened = addr_str
            .parse::<libp2p::Multiaddr>()
            .map_err(|e| format!("parse multiaddr {addr_str}: {e}"))
            .and_then(|addr| {
                swarm
                    .listen_on(addr)
                    .map_err(|e| format!("listen on {addr_str}: {}", decode_error_chain(&e)))
            });
        match listened {
            Ok(id) => {
                live.insert(id);
            }
            Err(reason) => {
                why_none.push(reason.clone());
                push_event(&events, NetEvent::Error(NetError::NotListening { reason }));
            }
        }
    }
    if !listen_addrs.is_empty() && live.is_empty() {
        push_event(
            &events,
            NetEvent::Unreachable {
                reason: why_none.join("; "),
            },
        );
    }

    for topic in initial_topics {
        let t = gossipsub::IdentTopic::new(&topic.0);
        if let Err(e) = swarm.behaviour_mut().gossipsub.subscribe(&t) {
            push_event(
                &events,
                NetEvent::Error(NetError::SubscribeFailed {
                    topic,
                    reason: format!("{e}"),
                }),
            );
        }
    }

    for addr_str in &bootstrap_addrs {
        match addr_str.parse::<libp2p::Multiaddr>() {
            Ok(addr) => {
                if let Err(e) = swarm.dial(addr) {
                    push_event(
                        &events,
                        NetEvent::Error(NetError::NotConnected {
                            reason: format!("dial {addr_str}: {e}"),
                        }),
                    );
                }
            }
            Err(e) => {
                push_event(
                    &events,
                    NetEvent::Error(NetError::ProviderInternal {
                        reason: format!("parse multiaddr {addr_str}: {e}"),
                    }),
                );
            }
        }
    }

    loop {
        select_biased! {
            cmd = cmd_rx.next() => {
                match cmd {
                    Some(Cmd::Publish { topic, bytes }) => {
                        let t = gossipsub::IdentTopic::new(&topic.0);
                        if let Err(e) = swarm.behaviour_mut().gossipsub.publish(t, bytes) {
                            push_event(&events, NetEvent::Error(NetError::PublishFailed {
                                topic,
                                reason: format!("{e}"),
                            }));
                        }
                    }
                    Some(Cmd::Subscribe(topic)) => {
                        let t = gossipsub::IdentTopic::new(&topic.0);
                        if let Err(e) = swarm.behaviour_mut().gossipsub.subscribe(&t) {
                            push_event(&events, NetEvent::Error(NetError::SubscribeFailed {
                                topic,
                                reason: format!("{e}"),
                            }));
                        }
                    }
                    Some(Cmd::Unsubscribe(topic)) => {
                        let t = gossipsub::IdentTopic::new(&topic.0);
                        swarm.behaviour_mut().gossipsub.unsubscribe(&t);
                    }
                    None => break,
                }
            }
            event = swarm.select_next_some() => {
                match event {
                    SwarmEvent::ListenerClosed { listener_id, addresses, reason } => {
                        let reason = match reason {
                            Ok(()) => format!("listener on {addresses:?} closed"),
                            Err(e) => format!("listener on {addresses:?} closed: {}", decode_error_chain(&e)),
                        };
                        push_event(&events, NetEvent::Error(NetError::NotListening { reason: reason.clone() }));
                        if live.remove(&listener_id) && live.is_empty() {
                            push_event(&events, NetEvent::Unreachable { reason });
                        }
                    }
                    SwarmEvent::ListenerError { error, .. } => {
                        push_event(&events, NetEvent::Error(NetError::NotListening {
                            reason: decode_error_chain(&error),
                        }));
                    }
                    other => handle_swarm_event(other, &events),
                }
            }
        }
    }
}

fn push_event(events: &Arc<Mutex<Vec<NetEvent>>>, event: NetEvent) {
    events.lock().unwrap_or_else(|p| p.into_inner()).push(event);
}

fn handle_swarm_event(event: SwarmEvent<LayeBehaviourEvent>, events: &Arc<Mutex<Vec<NetEvent>>>) {
    match event {
        SwarmEvent::NewListenAddr { address, .. } => {
            push_event(
                events,
                NetEvent::Listening {
                    addr: address.to_string(),
                },
            );
        }
        SwarmEvent::ConnectionEstablished { peer_id, .. } => {
            push_event(
                events,
                NetEvent::PeerUp {
                    peer: PeerId(peer_id.to_string()),
                    addrs: Vec::new(),
                },
            );
        }
        SwarmEvent::ConnectionClosed { peer_id, cause, .. } => {
            push_event(
                events,
                NetEvent::PeerDown {
                    peer: PeerId(peer_id.to_string()),
                    reason: cause
                        .as_ref()
                        .map(|e| decode_error_chain(e))
                        .unwrap_or_else(|| "graceful close (no cause reported)".into()),
                },
            );
        }
        SwarmEvent::OutgoingConnectionError { peer_id, error, .. } => {
            let peer_str = peer_id
                .map(|p| p.to_string())
                .unwrap_or_else(|| "<unknown>".into());
            let chain = decode_error_chain(&error);
            push_event(
                events,
                NetEvent::Error(NetError::NotConnected {
                    reason: format!("outgoing dial to {peer_str}: {chain}"),
                }),
            );
        }
        SwarmEvent::IncomingConnectionError { error, .. } => {
            let chain = decode_error_chain(&error);
            push_event(
                events,
                NetEvent::Error(NetError::NotConnected {
                    reason: format!("incoming connection error: {chain}"),
                }),
            );
        }
        SwarmEvent::Behaviour(LayeBehaviourEvent::Gossipsub(gossipsub::Event::Message {
            propagation_source,
            message,
            ..
        })) => {
            // Signed source if present (the identity that signed the
            // payload), falling back to propagation_source. In a star
            // topology propagation is always the relay, so signed source
            // is what distinguishes peers.
            let from = message
                .source
                .as_ref()
                .map(|s| s.to_string())
                .unwrap_or_else(|| propagation_source.to_string());
            let from_did = message.source.as_ref().and_then(did_of);
            push_event(
                events,
                NetEvent::Message {
                    topic: Topic(message.topic.to_string()),
                    from: PeerId(from),
                    from_did,
                    bytes: message.data,
                    at_ms: now_ms(),
                },
            );
        }
        SwarmEvent::Behaviour(LayeBehaviourEvent::Gossipsub(gossipsub::Event::Subscribed {
            peer_id,
            topic,
        })) => {
            push_event(
                events,
                NetEvent::SubscriptionChange {
                    topic: Topic(topic.to_string()),
                    peer: PeerId(peer_id.to_string()),
                    joined: true,
                },
            );
        }
        SwarmEvent::Behaviour(LayeBehaviourEvent::Gossipsub(gossipsub::Event::Unsubscribed {
            peer_id,
            topic,
        })) => {
            push_event(
                events,
                NetEvent::SubscriptionChange {
                    topic: Topic(topic.to_string()),
                    peer: PeerId(peer_id.to_string()),
                    joined: false,
                },
            );
        }
        // Ping / Identify / ConnectionLimits behaviour events are
        // expected and uninteresting at the transport layer.
        _ => {}
    }
}

/// The did:key a peer id names. An ed25519 peer id carries its public key
/// inline (an identity multihash), so the key is read out of it, not looked up.
pub(crate) fn did_of(peer: &libp2p::PeerId) -> Option<String> {
    let multihash = libp2p::multihash::Multihash::<64>::from_bytes(&peer.to_bytes()).ok()?;
    if multihash.code() != 0 {
        return None;
    }
    let public = libp2p::identity::PublicKey::try_decode_protobuf(multihash.digest()).ok()?;
    let ed25519 = public.try_into_ed25519().ok()?;
    Some(laye_me::didkey::encode(&ed25519.to_bytes()))
}

/// Walks the error chain via `std::error::Error::source()`, extracting
/// `io::ErrorKind` at each level where the type downcasts to
/// `io::Error`. Stops `format!("{e:?}")`-style collapse — surfaces the
/// actual kinds instead of wrapped Custom errors. Bounded to 8 levels
/// so a pathological cycle can't spin.
fn decode_error_chain(err: &(dyn std::error::Error + 'static)) -> String {
    let mut parts: Vec<String> = Vec::new();
    let mut current: Option<&(dyn std::error::Error + 'static)> = Some(err);
    let mut depth = 0;
    while let Some(e) = current {
        if depth >= 8 {
            parts.push("…(chain truncated)".into());
            break;
        }
        if let Some(io_err) = e.downcast_ref::<std::io::Error>() {
            parts.push(format!("io::{:?}", io_err.kind()));
        } else {
            let s = format!("{e}");
            if !s.is_empty() {
                parts.push(s);
            }
        }
        current = e.source();
        depth += 1;
    }
    if parts.is_empty() {
        "(no detail)".into()
    } else {
        parts.join(" ← ")
    }
}

#[cfg(target_arch = "wasm32")]
fn now_ms() -> u64 {
    js_sys::Date::now() as u64
}

#[cfg(not(target_arch = "wasm32"))]
fn now_ms() -> u64 {
    use std::time::{SystemTime, UNIX_EPOCH};
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_millis() as u64)
        .unwrap_or(0)
}
