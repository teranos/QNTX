//! Storage trait definitions

use crate::attestation::{Attestation, AxFilter, AxResult};
use crate::storage::error::StoreResult;

/// Core storage operations for attestations.
///
/// This trait defines the minimal interface that all storage backends must implement.
/// It's designed to work across different platforms:
/// - Native: SQLite, PostgreSQL, filesystem
/// - Browser: IndexedDB, localStorage
/// - Testing: In-memory
pub trait AttestationStore {
    /// Store an attestation.
    ///
    /// If an attestation with the same ID already exists, returns `StoreError::AlreadyExists`.
    fn put(&mut self, attestation: Attestation) -> StoreResult<()>;

    /// Retrieve an attestation by ID.
    ///
    /// Returns `None` if not found.
    fn get(&self, id: &str) -> StoreResult<Option<Attestation>>;

    /// Check if an attestation exists.
    fn exists(&self, id: &str) -> StoreResult<bool> {
        Ok(self.get(id)?.is_some())
    }

    /// Delete an attestation by ID.
    ///
    /// Returns `true` if the attestation was deleted, `false` if it didn't exist.
    fn delete(&mut self, id: &str) -> StoreResult<bool>;

    /// Update an existing attestation.
    ///
    /// Returns `StoreError::NotFound` if the attestation doesn't exist.
    fn update(&mut self, attestation: Attestation) -> StoreResult<()>;

    /// Get all attestation IDs.
    fn ids(&self) -> StoreResult<Vec<String>>;

    /// Get the total count of attestations.
    fn count(&self) -> StoreResult<usize> {
        Ok(self.ids()?.len())
    }

    /// Clear all attestations.
    fn clear(&mut self) -> StoreResult<()>;
}

/// Extended query operations for attestation retrieval.
///
/// This trait provides more advanced query capabilities beyond basic CRUD.
/// Not all backends may implement this efficiently.
pub trait QueryStore: AttestationStore {
    /// Execute an AX query filter and return matching attestations.
    fn query(&self, filter: &AxFilter) -> StoreResult<AxResult>;

    /// Get all distinct predicates in the store.
    ///
    /// Used for fuzzy matching index population.
    fn predicates(&self) -> StoreResult<Vec<String>>;

    /// Get all distinct contexts in the store.
    ///
    /// Used for fuzzy matching index population.
    fn contexts(&self) -> StoreResult<Vec<String>>;

    /// Get all distinct subjects in the store.
    fn subjects(&self) -> StoreResult<Vec<String>>;

    /// Get all distinct actors in the store.
    fn actors(&self) -> StoreResult<Vec<String>>;

    /// Get storage statistics.
    fn stats(&self) -> StoreResult<StorageStats>;
}

/// Storage statistics
#[derive(Debug, Clone, Default)]
pub struct StorageStats {
    pub total_attestations: usize,
    pub unique_subjects: usize,
    pub unique_predicates: usize,
    pub unique_contexts: usize,
    pub unique_actors: usize,
}

/// Filter for querying attestations (simpler than AxFilter for basic queries)
#[derive(Debug, Clone, Default)]
#[allow(dead_code)]
pub struct AttestationFilter {
    pub subjects: Option<Vec<String>>,
    pub predicates: Option<Vec<String>>,
    pub contexts: Option<Vec<String>>,
    pub actors: Option<Vec<String>>,
    pub time_start: Option<i64>,
    pub time_end: Option<i64>,
    pub limit: Option<usize>,
}

#[allow(dead_code)]
impl AttestationFilter {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn subjects(mut self, subjects: Vec<String>) -> Self {
        self.subjects = Some(subjects);
        self
    }

    pub fn predicates(mut self, predicates: Vec<String>) -> Self {
        self.predicates = Some(predicates);
        self
    }

    pub fn contexts(mut self, contexts: Vec<String>) -> Self {
        self.contexts = Some(contexts);
        self
    }

    pub fn actors(mut self, actors: Vec<String>) -> Self {
        self.actors = Some(actors);
        self
    }

    pub fn limit(mut self, limit: usize) -> Self {
        self.limit = Some(limit);
        self
    }
}
