//! Storage trait definitions

use std::collections::HashMap;

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

/// Bidirectional identifier equivalence.
///
/// An alias says two names denote the same thing, so a query for either finds
/// attestations written with the other. Aliases are symmetric: creating
/// `A -> B` also creates `B -> A`, and both directions are stored explicitly.
///
/// Matching is case-insensitive but original case is preserved — the `aliases`
/// table declares `COLLATE NOCASE` on both columns (migration 012), so
/// `"Weave"` and `"weave"` are one identifier.
///
/// The Go counterpart is the `ats.AliasResolver` interface, implemented by
/// `ats/storage/alias_store.go`.
pub trait AliasStore {
    /// Every identifier that should be searched when looking for `identifier`,
    /// including `identifier` itself.
    ///
    /// An identifier with no aliases resolves to just itself, never an empty
    /// vector. Results are sorted and deduplicated.
    fn resolve_alias(&self, identifier: &str) -> StoreResult<Vec<String>>;

    /// Create a bidirectional alias between two identifiers.
    ///
    /// Returns `StoreError::InvalidData` if either side is empty, or if the two
    /// are the same identifier ignoring case — neither carries information.
    /// Creating an alias that already exists is not an error.
    fn create_alias(&mut self, alias: &str, target: &str, created_by: &str) -> StoreResult<()>;

    /// Remove the alias between two identifiers, in both directions.
    ///
    /// Removing an alias that does not exist is not an error.
    fn remove_alias(&mut self, alias: &str, target: &str) -> StoreResult<()>;

    /// Every alias mapping, keyed by alias. Both directions of each pair appear,
    /// since both are stored.
    fn all_aliases(&self) -> StoreResult<HashMap<String, Vec<String>>>;
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
