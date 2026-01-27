//! In-memory storage backend
//!
//! A simple HashMap-based implementation for testing and development.
//! Not suitable for production use due to lack of persistence.

use std::collections::{HashMap, HashSet};

use crate::attestation::{Attestation, AxFilter, AxResult, AxSummary};
use crate::storage::error::{StoreError, StoreResult};
use crate::storage::traits::{AttestationStore, QueryStore, StorageStats};

/// In-memory attestation store.
///
/// Stores attestations in a HashMap. Useful for:
/// - Unit testing
/// - Development/prototyping
/// - Short-lived processes that don't need persistence
#[derive(Debug, Default)]
pub struct MemoryStore {
    attestations: HashMap<String, Attestation>,
}

impl MemoryStore {
    /// Create a new empty memory store.
    pub fn new() -> Self {
        Self {
            attestations: HashMap::new(),
        }
    }

    /// Create a memory store with initial attestations.
    pub fn with_attestations(attestations: Vec<Attestation>) -> Self {
        let mut store = Self::new();
        for attestation in attestations {
            let _ = store.put(attestation);
        }
        store
    }

    /// Get a reference to all attestations (for testing).
    pub fn all(&self) -> &HashMap<String, Attestation> {
        &self.attestations
    }
}

impl AttestationStore for MemoryStore {
    fn put(&mut self, attestation: Attestation) -> StoreResult<()> {
        if self.attestations.contains_key(&attestation.id) {
            return Err(StoreError::AlreadyExists(attestation.id));
        }
        self.attestations
            .insert(attestation.id.clone(), attestation);
        Ok(())
    }

    fn get(&self, id: &str) -> StoreResult<Option<Attestation>> {
        Ok(self.attestations.get(id).cloned())
    }

    fn delete(&mut self, id: &str) -> StoreResult<bool> {
        Ok(self.attestations.remove(id).is_some())
    }

    fn update(&mut self, attestation: Attestation) -> StoreResult<()> {
        if !self.attestations.contains_key(&attestation.id) {
            return Err(StoreError::NotFound(attestation.id));
        }
        self.attestations
            .insert(attestation.id.clone(), attestation);
        Ok(())
    }

    fn ids(&self) -> StoreResult<Vec<String>> {
        Ok(self.attestations.keys().cloned().collect())
    }

    fn clear(&mut self) -> StoreResult<()> {
        self.attestations.clear();
        Ok(())
    }
}

impl QueryStore for MemoryStore {
    fn query(&self, filter: &AxFilter) -> StoreResult<AxResult> {
        let mut matching: Vec<Attestation> = self
            .attestations
            .values()
            .filter(|a| matches_filter(a, filter))
            .cloned()
            .collect();

        // Apply limit
        if let Some(limit) = filter.limit {
            matching.truncate(limit);
        }

        // Build summary
        let summary = build_summary(&matching);

        Ok(AxResult {
            attestations: matching,
            conflicts: Vec::new(), // TODO: implement conflict detection
            summary,
        })
    }

    fn predicates(&self) -> StoreResult<Vec<String>> {
        let mut predicates: HashSet<String> = HashSet::new();
        for attestation in self.attestations.values() {
            for predicate in &attestation.predicates {
                predicates.insert(predicate.clone());
            }
        }
        let mut result: Vec<String> = predicates.into_iter().collect();
        result.sort();
        Ok(result)
    }

    fn contexts(&self) -> StoreResult<Vec<String>> {
        let mut contexts: HashSet<String> = HashSet::new();
        for attestation in self.attestations.values() {
            for context in &attestation.contexts {
                contexts.insert(context.clone());
            }
        }
        let mut result: Vec<String> = contexts.into_iter().collect();
        result.sort();
        Ok(result)
    }

    fn subjects(&self) -> StoreResult<Vec<String>> {
        let mut subjects: HashSet<String> = HashSet::new();
        for attestation in self.attestations.values() {
            for subject in &attestation.subjects {
                subjects.insert(subject.clone());
            }
        }
        let mut result: Vec<String> = subjects.into_iter().collect();
        result.sort();
        Ok(result)
    }

    fn actors(&self) -> StoreResult<Vec<String>> {
        let mut actors: HashSet<String> = HashSet::new();
        for attestation in self.attestations.values() {
            for actor in &attestation.actors {
                actors.insert(actor.clone());
            }
        }
        let mut result: Vec<String> = actors.into_iter().collect();
        result.sort();
        Ok(result)
    }

    fn stats(&self) -> StoreResult<StorageStats> {
        Ok(StorageStats {
            total_attestations: self.attestations.len(),
            unique_subjects: self.subjects()?.len(),
            unique_predicates: self.predicates()?.len(),
            unique_contexts: self.contexts()?.len(),
            unique_actors: self.actors()?.len(),
        })
    }
}

/// Check if an attestation matches the given filter.
fn matches_filter(attestation: &Attestation, filter: &AxFilter) -> bool {
    // Check subjects
    if !filter.subjects.is_empty() {
        let has_match = attestation
            .subjects
            .iter()
            .any(|s| filter.subjects.contains(s));
        if !has_match {
            return false;
        }
    }

    // Check predicates
    if !filter.predicates.is_empty() {
        let has_match = attestation
            .predicates
            .iter()
            .any(|p| filter.predicates.contains(p));
        if !has_match {
            return false;
        }
    }

    // Check contexts
    if !filter.contexts.is_empty() {
        let has_match = attestation
            .contexts
            .iter()
            .any(|c| filter.contexts.contains(c));
        if !has_match {
            return false;
        }
    }

    // Check actors
    if !filter.actors.is_empty() {
        let has_match = attestation.actors.iter().any(|a| filter.actors.contains(a));
        if !has_match {
            return false;
        }
    }

    // Check time range
    if let Some(start) = filter.time_start {
        if attestation.timestamp < start {
            return false;
        }
    }
    if let Some(end) = filter.time_end {
        if attestation.timestamp > end {
            return false;
        }
    }

    true
}

/// Build a summary from a list of attestations.
fn build_summary(attestations: &[Attestation]) -> AxSummary {
    let mut summary = AxSummary {
        total_attestations: attestations.len(),
        unique_subjects: HashMap::new(),
        unique_predicates: HashMap::new(),
        unique_contexts: HashMap::new(),
        unique_actors: HashMap::new(),
    };

    for attestation in attestations {
        for subject in &attestation.subjects {
            *summary.unique_subjects.entry(subject.clone()).or_insert(0) += 1;
        }
        for predicate in &attestation.predicates {
            *summary
                .unique_predicates
                .entry(predicate.clone())
                .or_insert(0) += 1;
        }
        for context in &attestation.contexts {
            *summary.unique_contexts.entry(context.clone()).or_insert(0) += 1;
        }
        for actor in &attestation.actors {
            *summary.unique_actors.entry(actor.clone()).or_insert(0) += 1;
        }
    }

    summary
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::attestation::AttestationBuilder;

    fn test_attestation(id: &str) -> Attestation {
        AttestationBuilder::new()
            .id(id)
            .subject("ALICE")
            .predicate("knows")
            .context("work")
            .actor("human:bob")
            .timestamp(1704067200000)
            .source("test")
            .build()
    }

    #[test]
    fn test_put_and_get() {
        let mut store = MemoryStore::new();
        let attestation = test_attestation("AS-test-1");

        store.put(attestation.clone()).unwrap();

        let retrieved = store.get("AS-test-1").unwrap();
        assert!(retrieved.is_some());
        assert_eq!(retrieved.unwrap().id, "AS-test-1");
    }

    #[test]
    fn test_put_duplicate() {
        let mut store = MemoryStore::new();
        let attestation = test_attestation("AS-test-1");

        store.put(attestation.clone()).unwrap();
        let result = store.put(attestation);

        assert!(matches!(result, Err(StoreError::AlreadyExists(_))));
    }

    #[test]
    fn test_delete() {
        let mut store = MemoryStore::new();
        store.put(test_attestation("AS-test-1")).unwrap();

        assert!(store.delete("AS-test-1").unwrap());
        assert!(!store.delete("AS-test-1").unwrap());
        assert!(store.get("AS-test-1").unwrap().is_none());
    }

    #[test]
    fn test_update() {
        let mut store = MemoryStore::new();
        store.put(test_attestation("AS-test-1")).unwrap();

        let mut updated = test_attestation("AS-test-1");
        updated.subjects = vec!["BOB".to_string()];

        store.update(updated).unwrap();

        let retrieved = store.get("AS-test-1").unwrap().unwrap();
        assert_eq!(retrieved.subjects, vec!["BOB"]);
    }

    #[test]
    fn test_update_not_found() {
        let mut store = MemoryStore::new();
        let attestation = test_attestation("AS-nonexistent");

        let result = store.update(attestation);
        assert!(matches!(result, Err(StoreError::NotFound(_))));
    }

    #[test]
    fn test_predicates() {
        let mut store = MemoryStore::new();

        let a1 = AttestationBuilder::new()
            .id("AS-1")
            .subject("A")
            .predicate("knows")
            .build();
        let a2 = AttestationBuilder::new()
            .id("AS-2")
            .subject("B")
            .predicate("works_with")
            .build();

        store.put(a1).unwrap();
        store.put(a2).unwrap();

        let predicates = store.predicates().unwrap();
        assert_eq!(predicates.len(), 2);
        assert!(predicates.contains(&"knows".to_string()));
        assert!(predicates.contains(&"works_with".to_string()));
    }

    #[test]
    fn test_query_by_subject() {
        let mut store = MemoryStore::new();

        store
            .put(
                AttestationBuilder::new()
                    .id("AS-1")
                    .subject("ALICE")
                    .predicate("knows")
                    .build(),
            )
            .unwrap();
        store
            .put(
                AttestationBuilder::new()
                    .id("AS-2")
                    .subject("BOB")
                    .predicate("knows")
                    .build(),
            )
            .unwrap();

        let filter = AxFilter {
            subjects: vec!["ALICE".to_string()],
            ..Default::default()
        };

        let result = store.query(&filter).unwrap();
        assert_eq!(result.attestations.len(), 1);
        assert_eq!(result.attestations[0].subjects, vec!["ALICE"]);
    }

    #[test]
    fn test_stats() {
        let mut store = MemoryStore::new();

        store
            .put(
                AttestationBuilder::new()
                    .id("AS-1")
                    .subject("ALICE")
                    .predicate("knows")
                    .context("work")
                    .actor("human:bob")
                    .build(),
            )
            .unwrap();
        store
            .put(
                AttestationBuilder::new()
                    .id("AS-2")
                    .subject("BOB")
                    .predicate("works_at")
                    .context("work")
                    .actor("human:alice")
                    .build(),
            )
            .unwrap();

        let stats = store.stats().unwrap();
        assert_eq!(stats.total_attestations, 2);
        assert_eq!(stats.unique_subjects, 2);
        assert_eq!(stats.unique_predicates, 2);
        assert_eq!(stats.unique_contexts, 1); // Both have "work"
        assert_eq!(stats.unique_actors, 2);
    }
}
