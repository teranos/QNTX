//! Engine-agnostic libp2p gossipsub transport for laye. wasm32 + native, same API.

mod swarm;

use futures::channel::mpsc;
pub use laye_me::Keypair;
pub use laye_protocol::{PeerId, Topic};
use serde::{Deserialize, Serialize};
use std::sync::{Arc, Mutex};

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum NetError {
    PublishFailed { topic: Topic, reason: String },
    SubscribeFailed { topic: Topic, reason: String },
    NotConnected { reason: String },
    InvalidTopic { topic: Topic, reason: String },
    ProviderInternal { reason: String },
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum NetEvent {
    PeerUp {
        peer: PeerId,
        addrs: Vec<String>,
    },
    PeerDown {
        peer: PeerId,
        reason: String,
    },
    Message {
        topic: Topic,
        from: PeerId,
        bytes: Vec<u8>,
        at_ms: u64,
    },
    SubscriptionChange {
        topic: Topic,
        peer: PeerId,
        joined: bool,
    },
    Error(NetError),
}

pub struct NetConfig {
    pub bootstrap_addrs: Vec<String>,
    pub keypair: Keypair,
    pub topics: Vec<Topic>,
    pub identify_protocol: String,
}

pub struct Net {
    self_peer_id: PeerId,
    cmd_tx: mpsc::UnboundedSender<swarm::Cmd>,
    events: Arc<Mutex<Vec<NetEvent>>>,
}

#[cfg(target_arch = "wasm32")]
pub type NetDrive = std::pin::Pin<Box<dyn std::future::Future<Output = ()>>>;

#[cfg(not(target_arch = "wasm32"))]
pub type NetDrive = std::pin::Pin<Box<dyn std::future::Future<Output = ()> + Send>>;

pub fn new(config: NetConfig) -> Result<(Net, NetDrive), NetError> {
    let peer_id = libp2p::PeerId::from(config.keypair.public());
    let self_peer_id = PeerId(peer_id.to_string());

    let swarm = swarm::build_swarm(config.keypair, config.identify_protocol)?;
    let (cmd_tx, cmd_rx) = mpsc::unbounded::<swarm::Cmd>();
    let events: Arc<Mutex<Vec<NetEvent>>> = Arc::new(Mutex::new(Vec::new()));

    let drive = Box::pin(swarm::drive_swarm(
        swarm,
        cmd_rx,
        events.clone(),
        config.bootstrap_addrs,
        config.topics,
    ));

    Ok((
        Net {
            self_peer_id,
            cmd_tx,
            events,
        },
        drive,
    ))
}

impl Net {
    pub fn identity(&self) -> &PeerId {
        &self.self_peer_id
    }

    pub fn publish(&self, topic: &Topic, bytes: &[u8]) -> Result<(), NetError> {
        self.cmd_tx
            .unbounded_send(swarm::Cmd::Publish {
                topic: topic.clone(),
                bytes: bytes.to_vec(),
            })
            .map_err(|e| NetError::ProviderInternal {
                reason: format!("publish cmd send: {e}"),
            })
    }

    pub fn subscribe(&self, topic: &Topic) -> Result<(), NetError> {
        self.cmd_tx
            .unbounded_send(swarm::Cmd::Subscribe(topic.clone()))
            .map_err(|e| NetError::ProviderInternal {
                reason: format!("subscribe cmd send: {e}"),
            })
    }

    pub fn unsubscribe(&self, topic: &Topic) -> Result<(), NetError> {
        self.cmd_tx
            .unbounded_send(swarm::Cmd::Unsubscribe(topic.clone()))
            .map_err(|e| NetError::ProviderInternal {
                reason: format!("unsubscribe cmd send: {e}"),
            })
    }

    pub fn poll_events(&self) -> Vec<NetEvent> {
        std::mem::take(&mut *self.events.lock().unwrap_or_else(|p| p.into_inner()))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn round_trip<T>(value: T)
    where
        T: serde::Serialize + for<'de> serde::Deserialize<'de> + PartialEq + std::fmt::Debug,
    {
        let json = serde_json::to_string(&value).expect("serialize");
        let parsed: T = serde_json::from_str(&json).expect("deserialize");
        assert_eq!(value, parsed);
    }

    #[test]
    fn net_error_variants_round_trip() {
        round_trip(NetError::PublishFailed {
            topic: Topic("t".into()),
            reason: "queue full".into(),
        });
        round_trip(NetError::SubscribeFailed {
            topic: Topic("t".into()),
            reason: "transport down".into(),
        });
        round_trip(NetError::NotConnected {
            reason: "no mesh peers".into(),
        });
        round_trip(NetError::InvalidTopic {
            topic: Topic("".into()),
            reason: "empty topic name".into(),
        });
        round_trip(NetError::ProviderInternal {
            reason: "wasm-bindgen panic".into(),
        });
    }

    #[test]
    fn net_event_variants_round_trip() {
        round_trip(NetEvent::PeerUp {
            peer: PeerId("p".into()),
            addrs: vec!["/dns4/x/tcp/443/wss".into()],
        });
        round_trip(NetEvent::PeerDown {
            peer: PeerId("p".into()),
            reason: "timeout".into(),
        });
        round_trip(NetEvent::Message {
            topic: Topic("t".into()),
            from: PeerId("p".into()),
            bytes: vec![1, 2, 3],
            at_ms: 1_700_000_000_000,
        });
        round_trip(NetEvent::SubscriptionChange {
            topic: Topic("t".into()),
            peer: PeerId("p".into()),
            joined: true,
        });
        round_trip(NetEvent::Error(NetError::NotConnected {
            reason: "no mesh peers".into(),
        }));
    }

    #[cfg(not(target_arch = "wasm32"))]
    #[test]
    fn new_with_fresh_keypair_returns_net_with_matching_peer_id() {
        let keypair = laye_me::fresh();
        let expected = PeerId(libp2p::PeerId::from(keypair.public()).to_string());
        let (net, _drive) = new(NetConfig {
            bootstrap_addrs: vec![],
            keypair,
            topics: vec![],
            identify_protocol: "/laye/1.0.0".into(),
        })
        .expect("Net::new");
        assert_eq!(net.identity(), &expected);
    }
}
