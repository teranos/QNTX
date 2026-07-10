//! Peer-pubkey → SignedBinding table. Moved from bevy-me as a plain
//! struct — laye-p2p is Bevy-free. LM will populate this from the
//! Mastodon login flow; LT reads it for author attribution.

use laye_me::SignedBinding;
use std::collections::HashMap;

#[derive(Default, Debug)]
pub struct BindingTable(pub HashMap<[u8; 32], Vec<SignedBinding>>);

impl BindingTable {
    pub fn resolve_handle(&self, peer_pubkey: &[u8; 32]) -> Option<&str> {
        self.0
            .get(peer_pubkey)?
            .iter()
            .find_map(|b| b.claim.handle.as_deref())
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;
    use laye_me::BindingClaim;

    #[test]
    fn resolve_returns_first_non_empty_handle() {
        let pk = [0x21; 32];
        let mut table = BindingTable::default();
        table.0.insert(
            pk,
            vec![
                SignedBinding {
                    claim: BindingClaim {
                        peer_pubkey: pk,
                        provider: "atproto".into(),
                        canonical_id: "did:plc:xxx".into(),
                        handle: None,
                        issued_at: 0,
                    },
                    signature: vec![],
                    signer_pubkey: [0u8; 32],
                },
                SignedBinding {
                    claim: BindingClaim {
                        peer_pubkey: pk,
                        provider: "mastodon".into(),
                        canonical_id: "https://chaos.social/@onf".into(),
                        handle: Some("@onf@chaos.social".into()),
                        issued_at: 1,
                    },
                    signature: vec![],
                    signer_pubkey: [0u8; 32],
                },
            ],
        );
        assert_eq!(table.resolve_handle(&pk), Some("@onf@chaos.social"));
    }

    #[test]
    fn resolve_returns_none_for_unseen_peer() {
        let table = BindingTable::default();
        assert_eq!(table.resolve_handle(&[0u8; 32]), None);
    }
}
