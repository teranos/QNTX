//! Bounded storage wrapper enforcing quota limits
//!
//! Provides a wrapper around SqliteStore that enforces storage quotas:
//! - Maximum number of attestations
//! - Maximum unique predicates
//! - Maximum unique contexts
//!
//! Default quotas match standard tier: 16 attestations, 64 predicates, 64 contexts

use qntx_core::{
    attestation::{Attestation, AxFilter, AxResult},
    storage::{AttestationStore, QueryStore, StoreError, StorageStats},
};

use crate::SqliteStore;

type StoreResult<T> = Result<T, StoreError>;

/// Storage quotas configuration
#[derive(Debug, Clone, Copy)]
pub struct StorageQuotas {
    /// Maximum number of attestations
    pub max_attestations: usize,
    /// Maximum number of unique predicates
    pub max_predicates: usize,
    /// Maximum number of unique contexts
    pub max_contexts: usize,
}

impl Default for StorageQuotas {
    fn default() -> Self {
        Self {
            max_attestations: 16,
            max_predicates: 64,
            max_contexts: 64,
        }
    }
}

impl StorageQuotas {
    /// Create quotas with custom limits
    pub fn new(max_attestations: usize, max_predicates: usize, max_contexts: usize) -> Self {
        Self {
            max_attestations,
            max_predicates,
            max_contexts,
        }
    }

    /// Standard tier quotas (16/64/64)
    pub fn standard() -> Self {
        Self::default()
    }

    /// Unlimited quotas (for testing)
    pub fn unlimited() -> Self {
        Self {
            max_attestations: usize::MAX,
            max_predicates: usize::MAX,
            max_contexts: usize::MAX,
        }
    }
}

/// Bounded storage wrapper enforcing quotas
pub struct BoundedStore {
    store: SqliteStore,
    quotas: StorageQuotas,
}

impl BoundedStore {
    /// Create a new bounded store with default quotas
    pub fn new(store: SqliteStore) -> Self {
        Self {
            store,
            quotas: StorageQuotas::default(),
        }
    }

    /// Create a bounded store with custom quotas
    pub fn with_quotas(store: SqliteStore, quotas: StorageQuotas) -> Self {
        Self { store, quotas }
    }

    /// Create an in-memory bounded store (for testing)
    pub fn in_memory() -> crate::error::Result<Self> {
        Ok(Self::new(SqliteStore::in_memory()?))
    }

    /// Create an in-memory bounded store with custom quotas
    pub fn in_memory_with_quotas(quotas: StorageQuotas) -> crate::error::Result<Self> {
        Ok(Self::with_quotas(SqliteStore::in_memory()?, quotas))
    }

    /// Get a reference to the quotas
    pub fn quotas(&self) -> &StorageQuotas {
        &self.quotas
    }

    /// Get a reference to the underlying store
    pub fn store(&self) -> &SqliteStore {
        &self.store
    }

    /// Check if adding an attestation would exceed quotas
    fn check_quotas(&self, attestation: &Attestation) -> StoreResult<()> {
        // Get actor (first one, or "unknown")
        let actor = attestation.actors.first()
            .map(|s| s.as_str())
            .unwrap_or("unknown");

        // Check attestation count
        let current_count = self.store.count()?;
        if current_count >= self.quotas.max_attestations {
            return Err(StoreError::QuotaExceeded {
                actor: actor.to_string(),
                context: "attestations".to_string(),
                current: current_count,
                limit: self.quotas.max_attestations,
            });
        }

        // Get current unique predicates and contexts
        let current_predicates = self.store.predicates()?;
        let current_contexts = self.store.contexts()?;

        // Check predicates quota
        for predicate in &attestation.predicates {
            if !current_predicates.contains(predicate)
                && current_predicates.len() >= self.quotas.max_predicates
            {
                return Err(StoreError::QuotaExceeded {
                    actor: actor.to_string(),
                    context: "predicates".to_string(),
                    current: current_predicates.len(),
                    limit: self.quotas.max_predicates,
                });
            }
        }

        // Check contexts quota
        for context in &attestation.contexts {
            if !current_contexts.contains(context)
                && current_contexts.len() >= self.quotas.max_contexts
            {
                return Err(StoreError::QuotaExceeded {
                    actor: actor.to_string(),
                    context: "contexts".to_string(),
                    current: current_contexts.len(),
                    limit: self.quotas.max_contexts,
                });
            }
        }

        Ok(())
    }
}

impl AttestationStore for BoundedStore {
    fn put(&mut self, attestation: Attestation) -> StoreResult<()> {
        self.check_quotas(&attestation)?;
        self.store.put(attestation)
    }

    fn get(&self, id: &str) -> StoreResult<Option<Attestation>> {
        self.store.get(id)
    }

    fn delete(&mut self, id: &str) -> StoreResult<bool> {
        self.store.delete(id)
    }

    fn update(&mut self, attestation: Attestation) -> StoreResult<()> {
        // For updates, only check new predicates/contexts (not total count)
        let current_predicates = self.store.predicates()?;
        let current_contexts = self.store.contexts()?;

        // Get actor
        let actor = attestation.actors.first()
            .map(|s| s.as_str())
            .unwrap_or("unknown");

        // Check new predicates
        for predicate in &attestation.predicates {
            if !current_predicates.contains(predicate)
                && current_predicates.len() >= self.quotas.max_predicates
            {
                return Err(StoreError::QuotaExceeded {
                    actor: actor.to_string(),
                    context: "predicates".to_string(),
                    current: current_predicates.len(),
                    limit: self.quotas.max_predicates,
                });
            }
        }

        // Check new contexts
        for context in &attestation.contexts {
            if !current_contexts.contains(context)
                && current_contexts.len() >= self.quotas.max_contexts
            {
                return Err(StoreError::QuotaExceeded {
                    actor: actor.to_string(),
                    context: "contexts".to_string(),
                    current: current_contexts.len(),
                    limit: self.quotas.max_contexts,
                });
            }
        }

        self.store.update(attestation)
    }

    fn ids(&self) -> StoreResult<Vec<String>> {
        self.store.ids()
    }

    fn clear(&mut self) -> StoreResult<()> {
        self.store.clear()
    }
}

impl QueryStore for BoundedStore {
    fn query(&self, filter: &AxFilter) -> StoreResult<AxResult> {
        self.store.query(filter)
    }

    fn predicates(&self) -> StoreResult<Vec<String>> {
        self.store.predicates()
    }

    fn contexts(&self) -> StoreResult<Vec<String>> {
        self.store.contexts()
    }

    fn subjects(&self) -> StoreResult<Vec<String>> {
        self.store.subjects()
    }

    fn actors(&self) -> StoreResult<Vec<String>> {
        self.store.actors()
    }

    fn stats(&self) -> StoreResult<StorageStats> {
        self.store.stats()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use qntx_core::AttestationBuilder;

    fn create_test_attestation(
        id: &str,
        subject: &str,
        predicate: &str,
        context: &str,
    ) -> Attestation {
        AttestationBuilder::new()
            .id(id)
            .subject(subject)
            .predicate(predicate)
            .context(context)
            .actor("human:test")
            .timestamp(1000)
            .source("test")
            .build()
    }

    #[test]
    fn test_bounded_put_within_quota() {
        let mut store = BoundedStore::in_memory().unwrap();
        let attestation = create_test_attestation("AS-1", "ALICE", "knows", "work");

        // Should succeed - within quota
        store.put(attestation).unwrap();
        assert_eq!(store.count().unwrap(), 1);
    }

    #[test]
    fn test_bounded_put_exceeds_attestation_quota() {
        let quotas = StorageQuotas::new(2, 10, 10);
        let mut store = BoundedStore::in_memory_with_quotas(quotas).unwrap();

        // Add up to quota
        store.put(create_test_attestation("AS-1", "ALICE", "knows", "work")).unwrap();
        store.put(create_test_attestation("AS-2", "BOB", "knows", "work")).unwrap();

        // Should fail - exceeds quota
        let result = store.put(create_test_attestation("AS-3", "CHARLIE", "knows", "work"));
        assert!(matches!(result, Err(StoreError::QuotaExceeded { .. })));
    }

    #[test]
    fn test_bounded_put_exceeds_predicate_quota() {
        let quotas = StorageQuotas::new(10, 2, 10);
        let mut store = BoundedStore::in_memory_with_quotas(quotas).unwrap();

        // Add up to quota
        store.put(create_test_attestation("AS-1", "ALICE", "knows", "work")).unwrap();
        store.put(create_test_attestation("AS-2", "BOB", "works_at", "ACME")).unwrap();

        // Should fail - exceeds predicate quota
        let result = store.put(create_test_attestation("AS-3", "CHARLIE", "manages", "team"));
        assert!(matches!(result, Err(StoreError::QuotaExceeded { .. })));
    }

    #[test]
    fn test_bounded_put_exceeds_context_quota() {
        let quotas = StorageQuotas::new(10, 10, 2);
        let mut store = BoundedStore::in_memory_with_quotas(quotas).unwrap();

        // Add up to quota
        store.put(create_test_attestation("AS-1", "ALICE", "knows", "work")).unwrap();
        store.put(create_test_attestation("AS-2", "BOB", "knows", "social")).unwrap();

        // Should fail - exceeds context quota
        let result = store.put(create_test_attestation("AS-3", "CHARLIE", "knows", "family"));
        assert!(matches!(result, Err(StoreError::QuotaExceeded { .. })));
    }

    #[test]
    fn test_bounded_put_reuses_existing_predicate() {
        let quotas = StorageQuotas::new(10, 1, 10);
        let mut store = BoundedStore::in_memory_with_quotas(quotas).unwrap();

        // Add first attestation
        store.put(create_test_attestation("AS-1", "ALICE", "knows", "work")).unwrap();

        // Should succeed - reuses existing predicate
        let result = store.put(create_test_attestation("AS-2", "BOB", "knows", "social"));
        assert!(result.is_ok());
    }

    #[test]
    fn test_bounded_update_with_new_predicate_exceeds_quota() {
        let quotas = StorageQuotas::new(10, 2, 10);
        let mut store = BoundedStore::in_memory_with_quotas(quotas).unwrap();

        // Add attestations
        store.put(create_test_attestation("AS-1", "ALICE", "knows", "work")).unwrap();
        store.put(create_test_attestation("AS-2", "BOB", "works_at", "ACME")).unwrap();

        // Try to update with new predicate - should fail
        let updated = create_test_attestation("AS-1", "ALICE", "manages", "work");
        let result = store.update(updated);
        assert!(matches!(result, Err(StoreError::QuotaExceeded { .. })));
    }

    #[test]
    fn test_bounded_delete_frees_space() {
        let quotas = StorageQuotas::new(2, 10, 10);
        let mut store = BoundedStore::in_memory_with_quotas(quotas).unwrap();

        // Fill quota
        store.put(create_test_attestation("AS-1", "ALICE", "knows", "work")).unwrap();
        store.put(create_test_attestation("AS-2", "BOB", "knows", "work")).unwrap();

        // Delete one
        store.delete("AS-1").unwrap();

        // Should now be able to add
        let result = store.put(create_test_attestation("AS-3", "CHARLIE", "knows", "work"));
        assert!(result.is_ok());
    }

    #[test]
    fn test_bounded_unlimited_quotas() {
        let mut store = BoundedStore::in_memory_with_quotas(StorageQuotas::unlimited()).unwrap();

        // Should be able to add many attestations
        for i in 0..100 {
            store.put(create_test_attestation(
                &format!("AS-{}", i),
                "ALICE",
                &format!("pred_{}", i),
                &format!("ctx_{}", i),
            )).unwrap();
        }

        assert_eq!(store.count().unwrap(), 100);
    }
}
