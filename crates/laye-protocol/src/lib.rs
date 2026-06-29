//! Pure-data wire types for the laye networking stack.

use serde::{Deserialize, Serialize};

/// libp2p PeerId in its base58btc form (`12D3KooW…` for Ed25519 keys).
#[derive(Clone, Debug, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub struct PeerId(pub String);

/// gossipsub topic name. Single string, no hashing — matches the
/// libp2p `IdentTopic` shape on the transport side.
#[derive(Clone, Debug, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub struct Topic(pub String);

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
    fn peer_id_round_trips() {
        round_trip(PeerId("12D3KooWXYZ".to_string()));
    }

    #[test]
    fn topic_round_trips() {
        round_trip(Topic("laye-positions/v1".to_string()));
    }
}
