//! Identity link protocol. laye-identity/v1 wire is a JSON array of
//! SignedBinding — one per gossipsub message = one peer's full binding
//! set. Receivers verify each and index by peer_pubkey into BindingTable.
//!
//! self_bindings persist to IndexedDB (same DB as the keypair, key
//! `bindings`) so "log in → reload → still logged in" (LM milestone).

use crate::binding::BindingTable;
use laye_me::SignedBinding;

pub const IDENTITY_TOPIC: &str = "laye-identity/v1";

pub fn encode_wire(bindings: &[SignedBinding]) -> serde_json::Result<Vec<u8>> {
    serde_json::to_vec(bindings)
}

/// Parse wire bytes into a peer's binding set. All bindings are verified
/// individually. Bindings whose claim.peer_pubkey disagrees with each
/// other or with the expected publisher pubkey are dropped — one peer's
/// message must be about that peer's identity.
pub fn parse_and_verify(bytes: &[u8], publisher_pubkey: &[u8; 32]) -> Vec<SignedBinding> {
    let Ok(list) = serde_json::from_slice::<Vec<SignedBinding>>(bytes) else {
        return Vec::new();
    };
    list.into_iter()
        .filter(|b| &b.claim.peer_pubkey == publisher_pubkey && b.verify().is_ok())
        .collect()
}

/// Absorb a peer's verified binding set into the table, replacing any
/// prior entry for that peer (the peer republishes their full set).
pub fn absorb(table: &mut BindingTable, peer_pubkey: [u8; 32], bindings: Vec<SignedBinding>) {
    if bindings.is_empty() {
        table.0.remove(&peer_pubkey);
    } else {
        table.0.insert(peer_pubkey, bindings);
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;
    use laye_me::BindingClaim;

    fn sign(claim: BindingClaim, signer: &laye_me::Keypair) -> SignedBinding {
        let sig = signer.sign(&claim.canonical_bytes()).unwrap();
        let signer_pubkey = signer
            .public()
            .try_into_ed25519()
            .unwrap()
            .to_bytes();
        SignedBinding {
            claim,
            signature: sig,
            signer_pubkey,
        }
    }

    fn sample_binding(peer_pubkey: [u8; 32], handle: &str) -> SignedBinding {
        let signer = laye_me::fresh();
        sign(
            BindingClaim {
                peer_pubkey,
                provider: "mastodon".into(),
                canonical_id: "https://chaos.social/@onf".into(),
                handle: Some(handle.into()),
                issued_at: 0,
            },
            &signer,
        )
    }

    #[test]
    fn round_trip_wire_encodes_and_decodes_bindings() {
        let pk = [0x21; 32];
        let bindings = vec![sample_binding(pk, "@onf@chaos.social")];
        let bytes = encode_wire(&bindings).unwrap();
        let parsed = parse_and_verify(&bytes, &pk);
        assert_eq!(parsed.len(), 1);
        assert_eq!(parsed[0].claim.handle.as_deref(), Some("@onf@chaos.social"));
    }

    #[test]
    fn parse_drops_bindings_whose_peer_pubkey_mismatches_publisher() {
        let real_pk = [0x21; 32];
        let attacker_pk = [0x99; 32];
        let bindings = vec![sample_binding(real_pk, "@onf@chaos.social")];
        let bytes = encode_wire(&bindings).unwrap();
        // Attacker publishes bindings that claim they're for real_pk.
        let parsed = parse_and_verify(&bytes, &attacker_pk);
        assert!(parsed.is_empty());
    }

    #[test]
    fn parse_drops_bindings_with_bad_signature() {
        let pk = [0x21; 32];
        let bindings = vec![sample_binding(pk, "@onf@chaos.social")];
        let mut bytes = encode_wire(&bindings).unwrap();
        // Corrupt a signature byte in the middle.
        let idx = bytes.iter().position(|b| *b == b'0').unwrap();
        bytes[idx] = b'f';
        let parsed = parse_and_verify(&bytes, &pk);
        assert!(parsed.is_empty());
    }

    #[test]
    fn absorb_inserts_and_replaces_entries() {
        let pk = [0x21; 32];
        let mut table = BindingTable::default();
        absorb(
            &mut table,
            pk,
            vec![sample_binding(pk, "@old@instance")],
        );
        assert_eq!(table.resolve_handle(&pk), Some("@old@instance"));
        absorb(
            &mut table,
            pk,
            vec![sample_binding(pk, "@new@instance")],
        );
        assert_eq!(table.resolve_handle(&pk), Some("@new@instance"));
    }

    #[test]
    fn absorb_empty_removes_prior_entry() {
        let pk = [0x21; 32];
        let mut table = BindingTable::default();
        absorb(
            &mut table,
            pk,
            vec![sample_binding(pk, "@onf@chaos.social")],
        );
        absorb(&mut table, pk, vec![]);
        assert_eq!(table.resolve_handle(&pk), None);
    }
}
