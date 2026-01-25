//! Query tests for SqliteStore

use qntx_core::{AttestationBuilder, storage::{AttestationStore, QueryStore}, AxFilter};
use qntx_sqlite::SqliteStore;

/// Helper to create a test attestation
fn create_attestation(
    id: &str,
    subject: &str,
    predicate: &str,
    context: &str,
    actor: &str,
    timestamp: i64,
) -> qntx_core::Attestation {
    AttestationBuilder::new()
        .id(id)
        .subject(subject)
        .predicate(predicate)
        .context(context)
        .actor(actor)
        .timestamp(timestamp)
        .source("test")
        .build()
}

#[test]
fn test_query_by_subject() {
    let mut store = SqliteStore::in_memory().unwrap();

    store.put(create_attestation("AS-1", "ALICE", "knows", "work", "human:bob", 1000)).unwrap();
    store.put(create_attestation("AS-2", "BOB", "knows", "work", "human:alice", 2000)).unwrap();
    store.put(create_attestation("AS-3", "ALICE", "works_at", "ACME", "human:bob", 3000)).unwrap();

    let filter = AxFilter {
        subjects: vec!["ALICE".to_string()],
        ..Default::default()
    };

    let result = store.query(&filter).unwrap();
    assert_eq!(result.attestations.len(), 2);
    assert!(result.attestations.iter().all(|a| a.subjects.contains(&"ALICE".to_string())));
}

#[test]
fn test_query_by_predicate() {
    let mut store = SqliteStore::in_memory().unwrap();

    store.put(create_attestation("AS-1", "ALICE", "knows", "work", "human:bob", 1000)).unwrap();
    store.put(create_attestation("AS-2", "BOB", "knows", "work", "human:alice", 2000)).unwrap();
    store.put(create_attestation("AS-3", "ALICE", "works_at", "ACME", "human:bob", 3000)).unwrap();

    let filter = AxFilter {
        predicates: vec!["knows".to_string()],
        ..Default::default()
    };

    let result = store.query(&filter).unwrap();
    assert_eq!(result.attestations.len(), 2);
    assert!(result.attestations.iter().all(|a| a.predicates.contains(&"knows".to_string())));
}

#[test]
fn test_query_by_context() {
    let mut store = SqliteStore::in_memory().unwrap();

    store.put(create_attestation("AS-1", "ALICE", "knows", "work", "human:bob", 1000)).unwrap();
    store.put(create_attestation("AS-2", "BOB", "knows", "social", "human:alice", 2000)).unwrap();
    store.put(create_attestation("AS-3", "ALICE", "works_at", "ACME", "human:bob", 3000)).unwrap();

    let filter = AxFilter {
        contexts: vec!["work".to_string()],
        ..Default::default()
    };

    let result = store.query(&filter).unwrap();
    assert_eq!(result.attestations.len(), 1);
    assert_eq!(result.attestations[0].contexts, vec!["work"]);
}

#[test]
fn test_query_by_actor() {
    let mut store = SqliteStore::in_memory().unwrap();

    store.put(create_attestation("AS-1", "ALICE", "knows", "work", "human:bob", 1000)).unwrap();
    store.put(create_attestation("AS-2", "BOB", "knows", "work", "human:alice", 2000)).unwrap();
    store.put(create_attestation("AS-3", "ALICE", "works_at", "ACME", "human:bob", 3000)).unwrap();

    let filter = AxFilter {
        actors: vec!["human:bob".to_string()],
        ..Default::default()
    };

    let result = store.query(&filter).unwrap();
    assert_eq!(result.attestations.len(), 2);
    assert!(result.attestations.iter().all(|a| a.actors.contains(&"human:bob".to_string())));
}

#[test]
fn test_query_by_time_range() {
    let mut store = SqliteStore::in_memory().unwrap();

    store.put(create_attestation("AS-1", "ALICE", "knows", "work", "human:bob", 1000)).unwrap();
    store.put(create_attestation("AS-2", "BOB", "knows", "work", "human:alice", 2000)).unwrap();
    store.put(create_attestation("AS-3", "ALICE", "works_at", "ACME", "human:bob", 3000)).unwrap();

    let filter = AxFilter {
        time_start: Some(1500),
        time_end: Some(2500),
        ..Default::default()
    };

    let result = store.query(&filter).unwrap();
    assert_eq!(result.attestations.len(), 1);
    assert_eq!(result.attestations[0].id, "AS-2");
}

#[test]
fn test_query_with_limit() {
    let mut store = SqliteStore::in_memory().unwrap();

    store.put(create_attestation("AS-1", "ALICE", "knows", "work", "human:bob", 1000)).unwrap();
    store.put(create_attestation("AS-2", "BOB", "knows", "work", "human:alice", 2000)).unwrap();
    store.put(create_attestation("AS-3", "ALICE", "works_at", "ACME", "human:bob", 3000)).unwrap();

    let filter = AxFilter {
        limit: Some(2),
        ..Default::default()
    };

    let result = store.query(&filter).unwrap();
    assert_eq!(result.attestations.len(), 2);
}

#[test]
fn test_query_combined_filters() {
    let mut store = SqliteStore::in_memory().unwrap();

    store.put(create_attestation("AS-1", "ALICE", "knows", "work", "human:bob", 1000)).unwrap();
    store.put(create_attestation("AS-2", "ALICE", "knows", "social", "human:alice", 2000)).unwrap();
    store.put(create_attestation("AS-3", "ALICE", "works_at", "work", "human:bob", 3000)).unwrap();
    store.put(create_attestation("AS-4", "BOB", "knows", "work", "human:alice", 4000)).unwrap();

    let filter = AxFilter {
        subjects: vec!["ALICE".to_string()],
        predicates: vec!["knows".to_string()],
        contexts: vec!["work".to_string()],
        ..Default::default()
    };

    let result = store.query(&filter).unwrap();
    assert_eq!(result.attestations.len(), 1);
    assert_eq!(result.attestations[0].id, "AS-1");
}

#[test]
fn test_query_empty_filter_returns_all() {
    let mut store = SqliteStore::in_memory().unwrap();

    store.put(create_attestation("AS-1", "ALICE", "knows", "work", "human:bob", 1000)).unwrap();
    store.put(create_attestation("AS-2", "BOB", "knows", "work", "human:alice", 2000)).unwrap();

    let filter = AxFilter::default();

    let result = store.query(&filter).unwrap();
    assert_eq!(result.attestations.len(), 2);
}

#[test]
fn test_query_summary() {
    let mut store = SqliteStore::in_memory().unwrap();

    store.put(create_attestation("AS-1", "ALICE", "knows", "work", "human:bob", 1000)).unwrap();
    store.put(create_attestation("AS-2", "ALICE", "works_at", "ACME", "human:alice", 2000)).unwrap();
    store.put(create_attestation("AS-3", "BOB", "knows", "social", "human:bob", 3000)).unwrap();

    let filter = AxFilter::default();
    let result = store.query(&filter).unwrap();

    assert_eq!(result.summary.total_attestations, 3);
    assert_eq!(result.summary.unique_subjects.len(), 2);
    assert_eq!(result.summary.unique_predicates.len(), 2);
    assert_eq!(result.summary.unique_contexts.len(), 3);
    assert_eq!(result.summary.unique_actors.len(), 2);

    // Check counts
    assert_eq!(result.summary.unique_subjects.get("ALICE"), Some(&2));
    assert_eq!(result.summary.unique_subjects.get("BOB"), Some(&1));
}

#[test]
fn test_predicates() {
    let mut store = SqliteStore::in_memory().unwrap();

    store.put(create_attestation("AS-1", "ALICE", "knows", "work", "human:bob", 1000)).unwrap();
    store.put(create_attestation("AS-2", "BOB", "works_at", "ACME", "human:alice", 2000)).unwrap();
    store.put(create_attestation("AS-3", "ALICE", "knows", "social", "human:bob", 3000)).unwrap();

    let predicates = store.predicates().unwrap();
    assert_eq!(predicates.len(), 2);
    assert!(predicates.contains(&"knows".to_string()));
    assert!(predicates.contains(&"works_at".to_string()));
}

#[test]
fn test_contexts() {
    let mut store = SqliteStore::in_memory().unwrap();

    store.put(create_attestation("AS-1", "ALICE", "knows", "work", "human:bob", 1000)).unwrap();
    store.put(create_attestation("AS-2", "BOB", "works_at", "ACME", "human:alice", 2000)).unwrap();
    store.put(create_attestation("AS-3", "ALICE", "knows", "social", "human:bob", 3000)).unwrap();

    let contexts = store.contexts().unwrap();
    assert_eq!(contexts.len(), 3);
    assert!(contexts.contains(&"work".to_string()));
    assert!(contexts.contains(&"ACME".to_string()));
    assert!(contexts.contains(&"social".to_string()));
}

#[test]
fn test_subjects() {
    let mut store = SqliteStore::in_memory().unwrap();

    store.put(create_attestation("AS-1", "ALICE", "knows", "work", "human:bob", 1000)).unwrap();
    store.put(create_attestation("AS-2", "BOB", "works_at", "ACME", "human:alice", 2000)).unwrap();
    store.put(create_attestation("AS-3", "ALICE", "knows", "social", "human:bob", 3000)).unwrap();

    let subjects = store.subjects().unwrap();
    assert_eq!(subjects.len(), 2);
    assert!(subjects.contains(&"ALICE".to_string()));
    assert!(subjects.contains(&"BOB".to_string()));
}

#[test]
fn test_actors() {
    let mut store = SqliteStore::in_memory().unwrap();

    store.put(create_attestation("AS-1", "ALICE", "knows", "work", "human:bob", 1000)).unwrap();
    store.put(create_attestation("AS-2", "BOB", "works_at", "ACME", "human:alice", 2000)).unwrap();
    store.put(create_attestation("AS-3", "ALICE", "knows", "social", "human:bob", 3000)).unwrap();

    let actors = store.actors().unwrap();
    assert_eq!(actors.len(), 2);
    assert!(actors.contains(&"human:bob".to_string()));
    assert!(actors.contains(&"human:alice".to_string()));
}

#[test]
fn test_stats() {
    let mut store = SqliteStore::in_memory().unwrap();

    store.put(create_attestation("AS-1", "ALICE", "knows", "work", "human:bob", 1000)).unwrap();
    store.put(create_attestation("AS-2", "BOB", "works_at", "ACME", "human:alice", 2000)).unwrap();
    store.put(create_attestation("AS-3", "ALICE", "knows", "social", "human:bob", 3000)).unwrap();

    let stats = store.stats().unwrap();
    assert_eq!(stats.total_attestations, 3);
    assert_eq!(stats.unique_subjects, 2);
    assert_eq!(stats.unique_predicates, 2);
    assert_eq!(stats.unique_contexts, 3);
    assert_eq!(stats.unique_actors, 2);
}

#[test]
fn test_query_with_multiple_values_in_filter() {
    let mut store = SqliteStore::in_memory().unwrap();

    store.put(create_attestation("AS-1", "ALICE", "knows", "work", "human:bob", 1000)).unwrap();
    store.put(create_attestation("AS-2", "BOB", "knows", "work", "human:alice", 2000)).unwrap();
    store.put(create_attestation("AS-3", "CHARLIE", "works_at", "ACME", "human:bob", 3000)).unwrap();

    let filter = AxFilter {
        subjects: vec!["ALICE".to_string(), "BOB".to_string()],
        ..Default::default()
    };

    let result = store.query(&filter).unwrap();
    assert_eq!(result.attestations.len(), 2);
    assert!(result.attestations.iter().any(|a| a.subjects.contains(&"ALICE".to_string())));
    assert!(result.attestations.iter().any(|a| a.subjects.contains(&"BOB".to_string())));
}
