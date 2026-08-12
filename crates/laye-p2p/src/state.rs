//! Per-topic receive queues. Ingests laye-net's NetEvent stream, filters
//! Message events into a per-topic FIFO. Reports pending_bytes and drains
//! [u32 LE len][bytes]... framing into a caller-owned buffer.
//!
//! Native-testable — no wasm-bindgen, no libp2p transport, just the shape.

use laye_net::NetEvent;
use std::collections::{HashMap, VecDeque};

#[derive(Default)]
pub struct RxState {
    queues: HashMap<String, VecDeque<Vec<u8>>>,
}

impl RxState {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn ingest(&mut self, events: Vec<NetEvent>) {
        for event in events {
            if let NetEvent::Message { topic, bytes, .. } = event {
                self.queues.entry(topic.0).or_default().push_back(bytes);
            }
        }
    }

    pub fn pending_bytes(&self, topic: &str) -> u32 {
        self.queues
            .get(topic)
            .map(|q| q.iter().map(|b| 4 + b.len() as u32).sum())
            .unwrap_or(0)
    }

    /// Writes as many whole `[u32 LE len][bytes]` frames as fit into
    /// `out`. Returns bytes written. Frames that don't fit stay queued.
    pub fn drain_into(&mut self, topic: &str, out: &mut [u8]) -> u32 {
        let Some(queue) = self.queues.get_mut(topic) else {
            return 0;
        };
        let mut written = 0usize;
        while let Some(frame) = queue.front() {
            let need = 4 + frame.len();
            if written + need > out.len() {
                break;
            }
            out[written..written + 4].copy_from_slice(&(frame.len() as u32).to_le_bytes());
            out[written + 4..written + need].copy_from_slice(frame);
            written += need;
            queue.pop_front();
        }
        written as u32
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;
    use laye_protocol::{PeerId, Topic};

    fn msg(topic: &str, bytes: &[u8]) -> NetEvent {
        NetEvent::Message {
            topic: Topic(topic.to_string()),
            from: PeerId("peer".to_string()),
            bytes: bytes.to_vec(),
            at_ms: 0,
        }
    }

    #[test]
    fn empty_state_reports_zero_pending() {
        let s = RxState::new();
        assert_eq!(s.pending_bytes("t"), 0);
    }

    #[test]
    fn ingested_message_bumps_pending_by_frame_size() {
        let mut s = RxState::new();
        s.ingest(vec![msg("t", b"hello")]);
        assert_eq!(s.pending_bytes("t"), 4 + 5);
    }

    #[test]
    fn drain_writes_length_prefixed_frames_in_order() {
        let mut s = RxState::new();
        s.ingest(vec![msg("t", b"aa"), msg("t", b"bbb")]);
        let mut buf = vec![0u8; 32];
        let n = s.drain_into("t", &mut buf);
        assert_eq!(n as usize, 4 + 2 + 4 + 3);
        assert_eq!(&buf[..4], &2u32.to_le_bytes());
        assert_eq!(&buf[4..6], b"aa");
        assert_eq!(&buf[6..10], &3u32.to_le_bytes());
        assert_eq!(&buf[10..13], b"bbb");
        assert_eq!(s.pending_bytes("t"), 0);
    }

    #[test]
    fn drain_leaves_frames_that_dont_fit() {
        let mut s = RxState::new();
        s.ingest(vec![msg("t", b"aa"), msg("t", b"bbbbbbbb")]);
        let mut buf = vec![0u8; 6];
        let n = s.drain_into("t", &mut buf);
        assert_eq!(n as usize, 4 + 2);
        assert_eq!(s.pending_bytes("t"), 4 + 8);
    }

    #[test]
    fn drain_on_unknown_topic_writes_nothing() {
        let mut s = RxState::new();
        s.ingest(vec![msg("a", b"x")]);
        let mut buf = vec![0u8; 32];
        let n = s.drain_into("b", &mut buf);
        assert_eq!(n, 0);
        assert_eq!(s.pending_bytes("a"), 4 + 1);
    }

    #[test]
    fn non_message_events_are_ignored() {
        let mut s = RxState::new();
        s.ingest(vec![
            NetEvent::PeerUp {
                peer: PeerId("p".into()),
                addrs: vec![],
            },
            NetEvent::SubscriptionChange {
                topic: Topic("t".into()),
                peer: PeerId("p".into()),
                joined: true,
            },
        ]);
        assert_eq!(s.pending_bytes("t"), 0);
    }

    #[test]
    fn messages_on_different_topics_go_to_different_queues() {
        let mut s = RxState::new();
        s.ingest(vec![msg("a", b"one"), msg("b", b"two")]);
        assert_eq!(s.pending_bytes("a"), 4 + 3);
        assert_eq!(s.pending_bytes("b"), 4 + 3);
        let mut buf = vec![0u8; 32];
        let n = s.drain_into("a", &mut buf);
        assert_eq!(n as usize, 4 + 3);
        assert_eq!(s.pending_bytes("a"), 0);
        assert_eq!(s.pending_bytes("b"), 4 + 3);
    }
}
