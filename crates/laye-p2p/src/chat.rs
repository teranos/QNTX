//! Chat protocol. Moved from bevy-chat as pure logic — laye-p2p is Bevy-free.
//! Owns SignedChat wire, verification, self-echo suppression, author
//! attribution, character-safe trim. Legacy plaintext `rave-chat/v1` receive
//! stays until LC retires it.

use crate::binding::BindingTable;
use laye_me::{Keypair, SignedChat};
use serde::{Deserialize, Serialize};
use std::collections::VecDeque;

pub const CHAT_TOPIC: &str = "laye-chat/v1";
pub const LEGACY_CHAT_TOPIC: &str = "rave-chat/v1";
pub const HISTORY_CAP: usize = 40;
pub const MAX_BODY_BYTES: usize = 512;

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct PlaintextChat {
    pub peer: String,
    pub body: String,
    pub at_ms: u64,
}

#[derive(Debug)]
pub enum IncomingChat {
    Signed(SignedChat),
    Plaintext(PlaintextChat),
}

#[derive(Clone, Debug)]
pub struct ChatEntry {
    pub who: String,
    pub body: String,
}

pub struct ChatState {
    pub history: VecDeque<ChatEntry>,
    pub cap: usize,
}

impl Default for ChatState {
    fn default() -> Self {
        Self {
            history: VecDeque::new(),
            cap: HISTORY_CAP,
        }
    }
}

impl ChatState {
    pub fn push(&mut self, entry: ChatEntry) {
        self.history.push_back(entry);
        while self.history.len() > self.cap {
            self.history.pop_front();
        }
    }
}

pub fn build_signed_wire(keypair: &Keypair, body: String, at_ms: u64) -> Option<Vec<u8>> {
    let author = self_peer_pubkey(keypair)?;
    let unsigned = SignedChat {
        author_peer_pubkey: author,
        body,
        at_ms,
        signature: Vec::new(),
    };
    let signature = keypair.sign(&unsigned.canonical_bytes()).ok()?;
    let signed = SignedChat {
        signature,
        ..unsigned
    };
    serde_json::to_vec(&signed).ok()
}

pub fn self_peer_pubkey(keypair: &Keypair) -> Option<[u8; 32]> {
    keypair
        .public()
        .try_into_ed25519()
        .ok()
        .map(|k| k.to_bytes())
}

pub fn trim_to_char_boundary(body: &str, max_bytes: usize) -> String {
    if body.len() <= max_bytes {
        return body.to_string();
    }
    let mut end = max_bytes;
    while !body.is_char_boundary(end) {
        end -= 1;
    }
    body[..end].to_string()
}

pub fn short_peer_display(peer: &str) -> String {
    peer.chars().take(8).collect()
}

pub fn short_author_display(pubkey: &[u8; 32]) -> String {
    let mut s = String::with_capacity(8);
    const HEX: &[u8; 16] = b"0123456789abcdef";
    for b in &pubkey[..4] {
        s.push(HEX[(b >> 4) as usize] as char);
        s.push(HEX[(b & 0x0f) as usize] as char);
    }
    s
}

pub fn attribute_author(bindings: Option<&BindingTable>, author_peer_pubkey: &[u8; 32]) -> String {
    if let Some(table) = bindings
        && let Some(handle) = table.resolve_handle(author_peer_pubkey)
    {
        return handle.to_string();
    }
    short_author_display(author_peer_pubkey)
}

/// Parse an inbound gossipsub message into an IncomingChat. Returns
/// None on unknown topic, parse failure, bad signature, or self-echo.
pub fn ingest(
    topic: &str,
    bytes: &[u8],
    self_pubkey: Option<&[u8; 32]>,
    self_peer_id: &str,
) -> Option<IncomingChat> {
    if topic == CHAT_TOPIC {
        let chat: SignedChat = serde_json::from_slice(bytes).ok()?;
        chat.verify().ok()?;
        if let Some(sp) = self_pubkey
            && chat.author_peer_pubkey == *sp
        {
            return None;
        }
        Some(IncomingChat::Signed(chat))
    } else if topic == LEGACY_CHAT_TOPIC {
        let chat: PlaintextChat = serde_json::from_slice(bytes).ok()?;
        if chat.peer == self_peer_id {
            return None;
        }
        Some(IncomingChat::Plaintext(chat))
    } else {
        None
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;
    use laye_me::BindingClaim;

    #[test]
    fn signed_wire_round_trips_a_valid_signature() {
        let kp = laye_me::fresh();
        let bytes = build_signed_wire(&kp, "hello".to_string(), 42).expect("build");
        let parsed: SignedChat = serde_json::from_slice(&bytes).expect("parse");
        parsed.verify().expect("verify");
        assert_eq!(parsed.body, "hello");
        assert_eq!(parsed.at_ms, 42);
    }

    #[test]
    fn ingest_signed_returns_signed_variant_for_valid_message() {
        let kp = laye_me::fresh();
        let bytes = build_signed_wire(&kp, "hi".into(), 1).expect("build");
        let out = ingest(CHAT_TOPIC, &bytes, None, "").expect("ingest");
        assert!(matches!(out, IncomingChat::Signed(_)));
    }

    #[test]
    fn ingest_drops_self_authored_signed_chat() {
        let kp = laye_me::fresh();
        let pubkey = self_peer_pubkey(&kp).expect("ed25519");
        let bytes = build_signed_wire(&kp, "loop".into(), 1).expect("build");
        let out = ingest(CHAT_TOPIC, &bytes, Some(&pubkey), "");
        assert!(out.is_none());
    }

    #[test]
    fn ingest_drops_bad_signature() {
        let kp = laye_me::fresh();
        let mut bytes = build_signed_wire(&kp, "hi".into(), 1).expect("build");
        let idx = bytes.len() / 2;
        bytes[idx] ^= 0xff;
        let out = ingest(CHAT_TOPIC, &bytes, None, "");
        assert!(out.is_none());
    }

    #[test]
    fn ingest_plaintext_returns_plaintext_variant_on_legacy_topic() {
        let raw = br#"{"peer":"12D3KooWabc","body":"hi","at_ms":1}"#;
        let out = ingest(LEGACY_CHAT_TOPIC, raw, None, "").expect("ingest");
        assert!(matches!(out, IncomingChat::Plaintext(_)));
    }

    #[test]
    fn ingest_drops_self_plaintext_by_peer_id() {
        let raw = br#"{"peer":"self","body":"loop","at_ms":1}"#;
        let out = ingest(LEGACY_CHAT_TOPIC, raw, None, "self");
        assert!(out.is_none());
    }

    #[test]
    fn ingest_returns_none_for_unknown_topic() {
        let raw = br#"{"peer":"x","body":"y","at_ms":1}"#;
        assert!(ingest("something/else", raw, None, "").is_none());
    }

    #[test]
    fn trim_to_char_boundary_does_not_split_multibyte() {
        let s = "aa€bb"; // € = 3 bytes
        assert_eq!(trim_to_char_boundary(s, 3), "aa");
        assert_eq!(trim_to_char_boundary(s, 4), "aa");
        assert_eq!(trim_to_char_boundary(s, 5), "aa€");
    }

    #[test]
    fn short_author_display_is_first_8_hex_chars() {
        let mut pk = [0u8; 32];
        pk[..4].copy_from_slice(&[0x21, 0xd7, 0x78, 0xae]);
        assert_eq!(short_author_display(&pk), "21d778ae");
    }

    #[test]
    fn attribute_uses_binding_handle_when_present() {
        let pk = [0x21; 32];
        let mut table = BindingTable::default();
        table.0.insert(
            pk,
            vec![laye_me::SignedBinding {
                claim: BindingClaim {
                    peer_pubkey: pk,
                    provider: "mastodon".into(),
                    canonical_id: "https://chaos.social/@onf".into(),
                    handle: Some("@onf@chaos.social".into()),
                    issued_at: 0,
                },
                signature: vec![],
                signer_pubkey: [0u8; 32],
            }],
        );
        assert_eq!(attribute_author(Some(&table), &pk), "@onf@chaos.social");
    }

    #[test]
    fn attribute_falls_back_to_short_hex_without_bindings() {
        let mut pk = [0u8; 32];
        pk[..4].copy_from_slice(&[0xca, 0xfe, 0xba, 0xbe]);
        assert_eq!(attribute_author(None, &pk), "cafebabe");
    }
}
