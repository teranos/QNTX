#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]
//! 「[Opus 5.5] The store against the etcd QNTX_ETCD_ENDPOINT names, which
//! 「[Opus 5.5] scripts/with-etcd.sh starts and names. Unset fails rather than skips: a」
//! 「[Opus 5.5] test that passes without an etcd says nothing.」」

use std::collections::HashMap;
use std::sync::atomic::{AtomicUsize, Ordering};

use ats::attestation::Attestation;
use ats::storage::{AttestationStore, StoreError};
use ats_etcd::{EtcdStore, SPAN};

fn endpoint() -> String {
    std::env::var("QNTX_ETCD_ENDPOINT")
        .expect("QNTX_ETCD_ENDPOINT names no etcd; run under scripts/with-etcd.sh")
}

/// 「[Opus 5.5] A namespace no other test touches.」
fn namespace() -> String {
    static N: AtomicUsize = AtomicUsize::new(0);
    format!(
        "t{}_{}",
        std::process::id(),
        N.fetch_add(1, Ordering::SeqCst)
    )
}

fn attestation(id: &str, subject: &str, timestamp: i64) -> Attestation {
    let mut attributes = HashMap::new();
    attributes.insert("k".to_string(), serde_json::json!({"n": 1.5}));
    Attestation {
        id: id.to_string(),
        subjects: vec![subject.to_string()],
        predicates: vec!["knows".to_string()],
        contexts: vec!["work".to_string()],
        actors: vec!["human:bob".to_string()],
        timestamp,
        source: "test".to_string(),
        attributes,
        created_at: timestamp,
        signature: Some(vec![1, 2, 3]),
        signer_did: Some("did:key:z".to_string()),
    }
}

#[test]
fn an_attestation_round_trips() {
    let mut store = EtcdStore::open(&endpoint(), &namespace()).unwrap();
    let a = attestation("AS-1", "ALICE", 1000);
    store.put(a.clone()).unwrap();
    assert_eq!(store.get("AS-1").unwrap(), Some(a.clone()));
    assert!(store.exists("AS-1").unwrap());
    assert_eq!(store.count().unwrap(), 1);
    assert!(matches!(store.put(a), Err(StoreError::AlreadyExists(_))));
    assert_eq!(store.get("AS-2").unwrap(), None);
}

#[test]
fn a_put_is_held_for_the_window() {
    assert_eq!(SPAN, std::time::Duration::from_secs(72 * 60 * 60));
    let mut store = EtcdStore::open(&endpoint(), &namespace()).unwrap();
    store.put(attestation("AS-1", "ALICE", 1000)).unwrap();
    assert_eq!(store.granted("AS-1").unwrap(), Some(SPAN));
    assert_eq!(store.granted("AS-2").unwrap(), None);
}

#[test]
fn namespaces_do_not_see_each_other() {
    let mut a = EtcdStore::open(&endpoint(), &namespace()).unwrap();
    let b = EtcdStore::open(&endpoint(), &namespace()).unwrap();
    a.put(attestation("AS-1", "ALICE", 1000)).unwrap();
    assert_eq!(b.get("AS-1").unwrap(), None);
    assert_eq!(b.count().unwrap(), 0);
}
