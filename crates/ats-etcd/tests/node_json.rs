#![allow(
    clippy::unwrap_used,
    clippy::expect_used,
    clippy::panic,
    clippy::indexing_slicing
)]
//! 「[Opus 5.5] The attestation as the node hands it over the FFI, read into what
//! the window keeps. No etcd is needed.」

use ats_etcd::attestation_of_node_json;

/// 「[Opus 5.5] An integer past 2^53 in the attributes stays itself: read through
/// the proto's number it was rounded to 9007199254740992 and kept so, with no
/// error (CodeRabbit on #1765).」
#[test]
fn an_integer_attribute_past_2_53_stays_itself() {
    let json = r#"{"id":"AS-1","subjects":["mycelia"],"predicates":["reads"],"contexts":["etcd"],"actors":["human:bob"],"timestamp":1700000000000,"source":"test","attributes":{"n":9007199254740993,"f":1.5,"s":"x","nested":{"m":-9007199254740993}},"created_at":1700000000000}"#;
    let a = attestation_of_node_json(json).unwrap();
    assert_eq!(a.attributes["n"], serde_json::json!(9007199254740993_u64));
    assert_eq!(
        a.attributes["nested"]["m"],
        serde_json::json!(-9007199254740993_i64)
    );
    assert_eq!(a.attributes["f"], serde_json::json!(1.5));
    assert_eq!(a.attributes["s"], serde_json::json!("x"));
    assert_eq!(a.id, "AS-1");
    assert_eq!(a.timestamp, 1700000000000);
}

#[test]
fn json_that_is_not_an_attestation_is_refused() {
    assert!(attestation_of_node_json("{").is_err());
}
