const ED25519_PUB: [u8; 2] = [0xed, 0x01];

/// The key a passkey belongs to, derived from the authenticator's PRF output.
/// Nothing is stored: the same biometric gives the same seed, which gives the
/// same key, which is what lets a credential say whose it is.
pub fn keypair_from_seed(seed: &[u8]) -> Option<libp2p_identity::ed25519::Keypair> {
    let mut raw: [u8; 32] = seed.try_into().ok()?;
    let secret = libp2p_identity::ed25519::SecretKey::try_from_bytes(&mut raw).ok()?;
    Some(libp2p_identity::ed25519::Keypair::from(secret))
}

pub fn encode(pubkey: &[u8; 32]) -> String {
    let mut buf = Vec::with_capacity(ED25519_PUB.len() + pubkey.len());
    buf.extend_from_slice(&ED25519_PUB);
    buf.extend_from_slice(pubkey);
    format!("did:key:z{}", bs58::encode(buf).into_string())
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;

    fn body_bytes(did: &str) -> Vec<u8> {
        let body = did.strip_prefix("did:key:z").expect("did:key:z prefix");
        bs58::decode(body).into_vec().expect("base58 body")
    }

    #[test]
    fn carries_the_ed25519_multicodec_prefix() {
        let did = encode(&[0xab; 32]);
        assert_eq!(&body_bytes(&did)[..2], &[0xed, 0x01]);
    }

    #[test]
    fn round_trips_the_public_key() {
        let pubkey = [0x21; 32];
        let bytes = body_bytes(&encode(&pubkey));
        assert_eq!(&bytes[2..], &pubkey);
        assert_eq!(bytes.len(), 34);
    }

    #[test]
    fn same_key_encodes_to_the_same_did() {
        let pubkey = [0x07; 32];
        assert_eq!(encode(&pubkey), encode(&pubkey));
    }

    #[test]
    fn different_keys_encode_differently() {
        assert_ne!(encode(&[0x01; 32]), encode(&[0x02; 32]));
    }

    #[test]
    fn encodes_the_same_32_bytes_self_peer_id_reports_as_hex() {
        let kp = laye_me::fresh();
        let pubkey = kp.public().try_into_ed25519().expect("ed25519").to_bytes();
        assert_eq!(&body_bytes(&encode(&pubkey))[2..], &pubkey);
    }

    /// The premise of the owner key: the same finger gives the same DID. A
    /// seed deriving differently each time makes every login a new person.
    #[test]
    fn the_same_seed_derives_the_same_key() {
        let first = keypair_from_seed(&[0x21; 32]).expect("32-byte seed");
        let second = keypair_from_seed(&[0x21; 32]).expect("32-byte seed");

        assert_eq!(first.public().to_bytes(), second.public().to_bytes());
        assert_eq!(
            encode(&first.public().to_bytes()),
            encode(&second.public().to_bytes())
        );
    }

    #[test]
    fn different_seeds_derive_different_keys() {
        let a = keypair_from_seed(&[0x01; 32]).expect("32-byte seed");
        let b = keypair_from_seed(&[0x02; 32]).expect("32-byte seed");

        assert_ne!(a.public().to_bytes(), b.public().to_bytes());
    }

    /// An authenticator answering with something that is not a PRF output
    /// derives nothing, rather than a key from whatever it sent.
    #[test]
    fn only_a_32_byte_seed_derives_anything() {
        assert!(keypair_from_seed(&[]).is_none());
        assert!(keypair_from_seed(&[0x21; 31]).is_none());
        assert!(keypair_from_seed(&[0x21; 33]).is_none());
        assert!(keypair_from_seed(&[0x21; 64]).is_none());
    }

    /// What the node checks: VerifyUserDID decodes the did:key back to the
    /// public half and verifies the signature against it.
    #[test]
    fn the_did_verifies_what_the_seed_signed() {
        let kp = keypair_from_seed(&[0x21; 32]).expect("32-byte seed");
        let challenge = b"a challenge the node chose";
        let signature = kp.sign(challenge);

        assert!(kp.public().verify(challenge, &signature));
        assert!(!kp.public().verify(b"something else", &signature));
    }
}
