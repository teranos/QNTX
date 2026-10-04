#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]
//! The store against the Postgres QNTX_POSTGRES_URL names, which
//! scripts/with-postgres.sh starts and names. Unset fails rather than skips: a
//! test that passes without a database says nothing.

use std::collections::HashMap;
use std::sync::atomic::{AtomicUsize, Ordering};

use ats::attestation::Attestation;
use ats::storage::{AttestationStore, StoreError};
use ats_postgres::{schema_tables, PostgresStore, QueryFilter};

fn url() -> String {
    std::env::var("QNTX_POSTGRES_URL")
        .expect("QNTX_POSTGRES_URL names no Postgres; run under scripts/with-postgres.sh")
}

/// A namespace no other test touches.
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
    let mut store = PostgresStore::open(&url(), "", &namespace()).unwrap();
    let a = attestation("AS-1", "ALICE", 1000);
    store.put(a.clone()).unwrap();
    assert_eq!(store.get("AS-1").unwrap(), Some(a.clone()));
    assert!(store.exists("AS-1").unwrap());
    assert_eq!(store.count().unwrap(), 1);
    assert!(matches!(store.put(a), Err(StoreError::AlreadyExists(_))));
    assert_eq!(store.get("AS-2").unwrap(), None);
}

#[test]
fn a_query_filters_and_answers_newest_first() {
    let mut store = PostgresStore::open(&url(), "", &namespace()).unwrap();
    store.put(attestation("AS-1", "ALICE", 1000)).unwrap();
    store.put(attestation("AS-2", "BOB", 2000)).unwrap();
    store.put(attestation("AS-3", "ALICE", 3000)).unwrap();

    let alice = store
        .query(&QueryFilter {
            subjects: vec!["ALICE".to_string()],
            ..Default::default()
        })
        .unwrap();
    let ids: Vec<_> = alice.iter().map(|a| a.id.as_str()).collect();
    assert_eq!(ids, ["AS-3", "AS-1"]);

    let since = store
        .query(&QueryFilter {
            time_start: Some(2000),
            ..Default::default()
        })
        .unwrap();
    assert_eq!(since.len(), 2);
}

#[test]
fn a_batch_sent_twice_is_held_once() {
    let store = PostgresStore::open(&url(), "", &namespace()).unwrap();
    let batch = vec![
        attestation("AS-1", "ALICE", 1000),
        attestation("AS-2", "BOB", 2000),
    ];
    assert_eq!(store.write_batch(&batch).unwrap(), 2);
    assert_eq!(store.write_batch(&batch).unwrap(), 2);
    assert_eq!(store.count().unwrap(), 2);
}

#[test]
fn namespaces_do_not_see_each_other() {
    let mut a = PostgresStore::open(&url(), "", &namespace()).unwrap();
    let b = PostgresStore::open(&url(), "", &namespace()).unwrap();
    a.put(attestation("AS-1", "ALICE", 1000)).unwrap();
    assert_eq!(b.count().unwrap(), 0);
}

#[test]
fn a_namespace_that_cannot_name_a_schema_is_refused() {
    assert!(PostgresStore::open(&url(), "", "x\"; DROP SCHEMA public; --").is_err());
}

#[test]
fn the_schema_is_what_the_migrations_leave() {
    let (tables, version) = schema_tables(&url(), "", &namespace()).unwrap();
    assert_eq!(tables, ["attestations", "schema_migrations"]);
    assert!(!version.is_empty());
}
