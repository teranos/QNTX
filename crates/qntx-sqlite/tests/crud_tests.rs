//! CRUD operation tests for SqliteStore

use qntx_core::{storage::AttestationStore, AttestationBuilder};
use qntx_sqlite::SqliteStore;

/// Helper to create a test attestation
fn create_test_attestation(id: &str) -> qntx_core::Attestation {
    AttestationBuilder::new()
        .id(id)
        .subject("ALICE")
        .predicate("knows")
        .context("work")
        .actor("human:bob")
        .timestamp(1704067200000) // 2024-01-01 00:00:00 UTC
        .source("test")
        .build()
}

#[test]
fn test_put_and_get() {
    let mut store = SqliteStore::in_memory().unwrap();
    let attestation = create_test_attestation("AS-test-1");

    // Put attestation
    store.put(attestation.clone()).unwrap();

    // Get it back
    let retrieved = store.get("AS-test-1").unwrap();
    assert!(retrieved.is_some());

    let retrieved = retrieved.unwrap();
    assert_eq!(retrieved.id, "AS-test-1");
    assert_eq!(retrieved.subjects, vec!["ALICE"]);
    assert_eq!(retrieved.predicates, vec!["knows"]);
    assert_eq!(retrieved.contexts, vec!["work"]);
    assert_eq!(retrieved.actors, vec!["human:bob"]);
}

#[test]
fn test_put_duplicate_fails() {
    let mut store = SqliteStore::in_memory().unwrap();
    let attestation = create_test_attestation("AS-test-1");

    // First put should succeed
    store.put(attestation.clone()).unwrap();

    // Second put should fail
    let result = store.put(attestation);
    assert!(result.is_err());
}

#[test]
fn test_get_nonexistent() {
    let store = SqliteStore::in_memory().unwrap();
    let result = store.get("AS-nonexistent").unwrap();
    assert!(result.is_none());
}

#[test]
fn test_delete() {
    let mut store = SqliteStore::in_memory().unwrap();
    let attestation = create_test_attestation("AS-test-1");

    // Put and verify exists
    store.put(attestation).unwrap();
    assert!(store.exists("AS-test-1").unwrap());

    // Delete
    let deleted = store.delete("AS-test-1").unwrap();
    assert!(deleted);

    // Verify doesn't exist anymore
    assert!(!store.exists("AS-test-1").unwrap());
}

#[test]
fn test_delete_nonexistent() {
    let mut store = SqliteStore::in_memory().unwrap();
    let deleted = store.delete("AS-nonexistent").unwrap();
    assert!(!deleted);
}

#[test]
fn test_update() {
    let mut store = SqliteStore::in_memory().unwrap();
    let mut attestation = create_test_attestation("AS-test-1");

    // Put initial version
    store.put(attestation.clone()).unwrap();

    // Update it
    attestation.subjects = vec!["BOB".to_string()];
    store.update(attestation).unwrap();

    // Verify update
    let retrieved = store.get("AS-test-1").unwrap().unwrap();
    assert_eq!(retrieved.subjects, vec!["BOB"]);
}

#[test]
fn test_update_nonexistent() {
    let mut store = SqliteStore::in_memory().unwrap();
    let attestation = create_test_attestation("AS-nonexistent");

    // Update should fail
    let result = store.update(attestation);
    assert!(result.is_err());
}

#[test]
fn test_ids() {
    let mut store = SqliteStore::in_memory().unwrap();

    // Initially empty
    assert_eq!(store.ids().unwrap().len(), 0);

    // Add some attestations
    store.put(create_test_attestation("AS-1")).unwrap();
    store.put(create_test_attestation("AS-2")).unwrap();
    store.put(create_test_attestation("AS-3")).unwrap();

    // Get IDs
    let ids = store.ids().unwrap();
    assert_eq!(ids.len(), 3);
    assert!(ids.contains(&"AS-1".to_string()));
    assert!(ids.contains(&"AS-2".to_string()));
    assert!(ids.contains(&"AS-3".to_string()));
}

#[test]
fn test_count() {
    let mut store = SqliteStore::in_memory().unwrap();

    // Initially zero
    assert_eq!(store.count().unwrap(), 0);

    // Add attestations
    store.put(create_test_attestation("AS-1")).unwrap();
    store.put(create_test_attestation("AS-2")).unwrap();

    // Count should be 2
    assert_eq!(store.count().unwrap(), 2);
}

#[test]
fn test_clear() {
    let mut store = SqliteStore::in_memory().unwrap();

    // Add some attestations
    store.put(create_test_attestation("AS-1")).unwrap();
    store.put(create_test_attestation("AS-2")).unwrap();
    assert_eq!(store.count().unwrap(), 2);

    // Clear
    store.clear().unwrap();

    // Should be empty
    assert_eq!(store.count().unwrap(), 0);
}

#[test]
fn test_exists() {
    let mut store = SqliteStore::in_memory().unwrap();
    let attestation = create_test_attestation("AS-test-1");

    // Initially doesn't exist
    assert!(!store.exists("AS-test-1").unwrap());

    // Put it
    store.put(attestation).unwrap();

    // Now exists
    assert!(store.exists("AS-test-1").unwrap());
}

#[test]
fn test_multiple_subjects() {
    let mut store = SqliteStore::in_memory().unwrap();
    let attestation = AttestationBuilder::new()
        .id("AS-multi")
        .subjects(vec!["ALICE", "BOB", "CHARLIE"])
        .predicate("attend")
        .context("meeting")
        .build();

    store.put(attestation).unwrap();

    let retrieved = store.get("AS-multi").unwrap().unwrap();
    assert_eq!(retrieved.subjects, vec!["ALICE", "BOB", "CHARLIE"]);
}

#[test]
fn test_attributes() {
    let mut store = SqliteStore::in_memory().unwrap();

    let attestation = AttestationBuilder::new()
        .id("AS-attrs")
        .subject("ALICE")
        .predicate("has")
        .context("metadata")
        .attribute("key1", serde_json::json!("value1"))
        .attribute("key2", serde_json::json!(42))
        .build();

    store.put(attestation).unwrap();

    let retrieved = store.get("AS-attrs").unwrap().unwrap();
    assert_eq!(
        retrieved.attributes.get("key1").unwrap(),
        &serde_json::json!("value1")
    );
    assert_eq!(
        retrieved.attributes.get("key2").unwrap(),
        &serde_json::json!(42)
    );
}

#[test]
fn test_empty_attributes() {
    let mut store = SqliteStore::in_memory().unwrap();
    let attestation = AttestationBuilder::new()
        .id("AS-no-attrs")
        .subject("ALICE")
        .predicate("exists")
        .context("_")
        .build();

    store.put(attestation).unwrap();

    let retrieved = store.get("AS-no-attrs").unwrap().unwrap();
    assert!(retrieved.attributes.is_empty());
}

#[test]
fn test_timestamp_preservation() {
    let mut store = SqliteStore::in_memory().unwrap();
    let timestamp = 1704067200000i64; // 2024-01-01 00:00:00 UTC

    let attestation = AttestationBuilder::new()
        .id("AS-time")
        .subject("ALICE")
        .predicate("action")
        .context("now")
        .timestamp(timestamp)
        .build();

    store.put(attestation).unwrap();

    let retrieved = store.get("AS-time").unwrap().unwrap();
    assert_eq!(retrieved.timestamp, timestamp);
}
