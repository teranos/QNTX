#![allow(
    clippy::unwrap_used,
    clippy::expect_used,
    clippy::panic,
    clippy::indexing_slicing
)]

// "prove it with two native peers passing one message through a third."

use std::collections::HashSet;
use std::time::{Duration, Instant};

use laye_net::{Net, NetConfig, NetEvent, PeerId, Topic};

struct Seen {
    connected_to: HashSet<PeerId>,
    messages: Vec<(Option<String>, Vec<u8>)>,
}

fn take(net: &Net, seen: &mut Seen) -> Vec<NetEvent> {
    let events = net.poll_events();
    for event in &events {
        match event {
            NetEvent::PeerUp { peer, .. } => {
                seen.connected_to.insert(peer.clone());
            }
            NetEvent::Message {
                from_did, bytes, ..
            } => {
                seen.messages.push((from_did.clone(), bytes.clone()));
            }
            _ => {}
        }
    }
    events
}

fn client(dial: &str, topic: &Topic) -> (Net, laye_me::Keypair) {
    let keypair = laye_me::fresh();
    let (net, drive) = laye_net::new(NetConfig {
        bootstrap_addrs: vec![dial.to_string()],
        listen_addrs: Vec::new(),
        keypair: keypair.clone(),
        topics: vec![topic.clone()],
        identify_protocol: laye_peer::IDENTIFY.into(),
    })
    .unwrap();
    tokio::spawn(drive);
    (net, keypair)
}

#[tokio::test(flavor = "multi_thread")]
async fn one_message_goes_from_one_peer_to_another_through_a_third() {
    let topic = Topic("laye/through-a-peer".into());
    let deadline = Instant::now() + Duration::from_secs(30);

    let (hub, drive) = laye_peer::listening(
        laye_me::fresh(),
        vec!["/ip4/127.0.0.1/tcp/0/ws".into()],
        vec![topic.clone()],
    )
    .unwrap();
    tokio::spawn(drive);

    let mut hub_seen = Seen {
        connected_to: HashSet::new(),
        messages: Vec::new(),
    };
    let addr = loop {
        assert!(
            Instant::now() < deadline,
            "the peer never said where it listens"
        );
        let found = take(&hub, &mut hub_seen).into_iter().find_map(|e| match e {
            NetEvent::Listening { addr } => Some(addr),
            _ => None,
        });
        if let Some(addr) = found {
            break addr;
        }
        tokio::time::sleep(Duration::from_millis(50)).await;
    };
    let dial = format!("{addr}/p2p/{}", hub.identity().0);

    let (sender, sender_key) = client(&dial, &topic);
    let (receiver, _) = client(&dial, &topic);
    let sender_did = laye_peer::did_of(&sender_key).unwrap();

    let mut sender_seen = Seen {
        connected_to: HashSet::new(),
        messages: Vec::new(),
    };
    let mut receiver_seen = Seen {
        connected_to: HashSet::new(),
        messages: Vec::new(),
    };
    let said = b"one message".to_vec();

    // Until the mesh forms a publish has no one to go to, so it is said again
    // each half second until it arrives.
    while receiver_seen.messages.is_empty() {
        assert!(Instant::now() < deadline, "the message never arrived");
        sender.publish(&topic, &said).unwrap();
        tokio::time::sleep(Duration::from_millis(500)).await;
        take(&hub, &mut hub_seen);
        take(&sender, &mut sender_seen);
        take(&receiver, &mut receiver_seen);
    }

    let (from_did, bytes) = &receiver_seen.messages[0];
    assert_eq!(bytes, &said);
    assert_eq!(from_did.as_deref(), Some(sender_did.as_str()));

    // Through the third: each of the two is connected to the hub, and to no
    // one else.
    let hub_only: HashSet<PeerId> = [hub.identity().clone()].into();
    assert_eq!(sender_seen.connected_to, hub_only);
    assert_eq!(receiver_seen.connected_to, hub_only);
}
