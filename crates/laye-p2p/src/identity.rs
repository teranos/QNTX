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

/// Parse wire bytes into a peer's binding set. Empty `trusted_signers` trusts
/// nobody: handles stop resolving, which is a chat that says less rather than
/// a chat that lies.
pub fn parse_and_verify(
    bytes: &[u8],
    publisher_pubkey: &[u8; 32],
    trusted_signers: &[[u8; 32]],
) -> Vec<SignedBinding> {
    let Ok(list) = serde_json::from_slice::<Vec<SignedBinding>>(bytes) else {
        return Vec::new();
    };
    list.into_iter()
        .filter(|b| {
            // One peer's message is about that peer's identity.
            &b.claim.peer_pubkey == publisher_pubkey
                // verify() reads the signing key out of the message, so alone
                // it says the message is self-consistent and nothing more.
                // Without this any peer signs "I am @someone" and is believed.
                && trusted_signers.contains(&b.signer_pubkey)
                && b.verify().is_ok()
        })
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
        let signer_pubkey = signer.public().try_into_ed25519().unwrap().to_bytes();
        SignedBinding {
            claim,
            signature: sig,
            signer_pubkey,
        }
    }

    /// A binding and the key that signed it, because a test has to say which
    /// signer it means. The node is that key in production.
    fn signed_by(
        peer_pubkey: [u8; 32],
        handle: &str,
        signer: &laye_me::Keypair,
    ) -> (SignedBinding, [u8; 32]) {
        let binding = sign(
            BindingClaim {
                peer_pubkey,
                provider: "mastodon".into(),
                canonical_id: "https://mastodon.example/@tim".into(),
                handle: Some(handle.into()),
                issued_at: 0,
            },
            signer,
        );
        let signer_pubkey = binding.signer_pubkey;
        (binding, signer_pubkey)
    }

    fn sample_binding(peer_pubkey: [u8; 32], handle: &str) -> SignedBinding {
        signed_by(peer_pubkey, handle, &laye_me::fresh()).0
    }

    #[test]
    fn round_trip_wire_encodes_and_decodes_bindings() {
        let pk = [0x21; 32];
        let (binding, node) = signed_by(pk, "@tim@mastodon.example", &laye_me::fresh());
        let bytes = encode_wire(&[binding]).unwrap();
        let parsed = parse_and_verify(&bytes, &pk, &[node]);
        assert_eq!(parsed.len(), 1);
        assert_eq!(
            parsed[0].claim.handle.as_deref(),
            Some("@tim@mastodon.example")
        );
    }

    /// All a self-signed binding proves is that it is self-consistent.
    /// Believing it is one peer choosing another peer's handle.
    #[test]
    fn parse_drops_a_binding_signed_by_an_untrusted_key() {
        let pk = [0x21; 32];
        let (_, node_pubkey) = signed_by(pk, "@real@mastodon.example", &laye_me::fresh());

        let (forged, _) = signed_by(pk, "@tim@mastodon.example", &laye_me::fresh());
        let bytes = encode_wire(&[forged]).unwrap();

        assert!(parse_and_verify(&bytes, &pk, &[node_pubkey]).is_empty());
    }

    /// A deployment naming no signer resolves no handles, rather than
    /// believing whoever spoke last.
    #[test]
    fn parse_trusts_nobody_when_no_signer_is_named() {
        let pk = [0x21; 32];
        let (binding, _) = signed_by(pk, "@tim@mastodon.example", &laye_me::fresh());
        let bytes = encode_wire(&[binding]).unwrap();

        assert!(parse_and_verify(&bytes, &pk, &[]).is_empty());
    }

    #[test]
    fn parse_drops_bindings_whose_peer_pubkey_mismatches_publisher() {
        let real_pk = [0x21; 32];
        let attacker_pk = [0x99; 32];
        let (binding, node) = signed_by(real_pk, "@tim@mastodon.example", &laye_me::fresh());
        let bytes = encode_wire(&[binding]).unwrap();
        // Attacker publishes bindings that claim they're for real_pk.
        let parsed = parse_and_verify(&bytes, &attacker_pk, &[node]);
        assert!(parsed.is_empty());
    }

    #[test]
    fn parse_drops_bindings_with_bad_signature() {
        let pk = [0x21; 32];
        let (binding, node) = signed_by(pk, "@tim@mastodon.example", &laye_me::fresh());
        let mut bytes = encode_wire(&[binding]).unwrap();
        // Corrupt a signature byte in the middle.
        let idx = bytes.iter().position(|b| *b == b'0').unwrap();
        bytes[idx] = b'f';
        let parsed = parse_and_verify(&bytes, &pk, &[node]);
        assert!(parsed.is_empty());
    }

    #[test]
    fn absorb_inserts_and_replaces_entries() {
        let pk = [0x21; 32];
        let mut table = BindingTable::default();
        absorb(&mut table, pk, vec![sample_binding(pk, "@old@instance")]);
        assert_eq!(table.resolve_handle(&pk), Some("@old@instance"));
        absorb(&mut table, pk, vec![sample_binding(pk, "@new@instance")]);
        assert_eq!(table.resolve_handle(&pk), Some("@new@instance"));
    }

    #[test]
    fn absorb_empty_removes_prior_entry() {
        let pk = [0x21; 32];
        let mut table = BindingTable::default();
        absorb(
            &mut table,
            pk,
            vec![sample_binding(pk, "@tim@mastodon.example")],
        );
        absorb(&mut table, pk, vec![]);
        assert_eq!(table.resolve_handle(&pk), None);
    }
}
