//! Bounded storage wrapper with post-insert eviction (16/64/64 strategy).
//!
//! After every insert, enforces three eviction rules:
//! - **16** attestations per (actor, context) pair — evicts oldest by timestamp
//! - **64** contexts per actor — evicts all attestations in least-used contexts
//! - **64** actors per entity (subject) — evicts all attestations from least-recent actors

use qntx_core::{
    attestation::{Attestation, AxFilter, AxResult},
    storage::{AttestationStore, QueryStore, StorageStats, StoreError},
};

use crate::error::SqliteError;
use crate::SqliteStore;

type StoreResult<T> = Result<T, StoreError>;

/// Default limits matching Go's 16/64/64 strategy.
pub const DEFAULT_ACTOR_CONTEXT_LIMIT: usize = 16;
pub const DEFAULT_ACTOR_CONTEXTS_LIMIT: usize = 64;
pub const DEFAULT_ENTITY_ACTORS_LIMIT: usize = 64;

/// Configurable storage limits for the eviction strategy.
#[derive(Debug, Clone, Copy)]
pub struct BoundedConfig {
    /// Max attestations per (actor, context) pair (default: 16)
    pub actor_context_limit: usize,
    /// Max contexts per actor (default: 64)
    pub actor_contexts_limit: usize,
    /// Max actors per entity/subject (default: 64)
    pub entity_actors_limit: usize,
}

impl Default for BoundedConfig {
    fn default() -> Self {
        Self {
            actor_context_limit: DEFAULT_ACTOR_CONTEXT_LIMIT,
            actor_contexts_limit: DEFAULT_ACTOR_CONTEXTS_LIMIT,
            entity_actors_limit: DEFAULT_ENTITY_ACTORS_LIMIT,
        }
    }
}

/// Eviction counts returned after a put operation.
#[derive(Debug, Clone, Default)]
pub struct EvictionResult {
    /// Number of attestations evicted by actor-context limit
    pub actor_context_evictions: usize,
    /// Number of attestations evicted by actor-contexts limit
    pub actor_contexts_evictions: usize,
    /// Number of attestations evicted by entity-actors limit
    pub entity_actors_evictions: usize,
}

impl EvictionResult {
    /// Total attestations evicted across all rules.
    pub fn total(&self) -> usize {
        self.actor_context_evictions
            + self.actor_contexts_evictions
            + self.entity_actors_evictions
    }
}

/// Bounded storage wrapper enforcing eviction-based limits.
pub struct BoundedStore {
    store: SqliteStore,
    config: BoundedConfig,
}

impl BoundedStore {
    /// Create a bounded store with default 16/64/64 limits.
    pub fn new(store: SqliteStore) -> Self {
        Self {
            store,
            config: BoundedConfig::default(),
        }
    }

    /// Create a bounded store with custom limits.
    pub fn with_config(store: SqliteStore, config: BoundedConfig) -> Self {
        Self { store, config }
    }

    /// Create an in-memory bounded store (for testing).
    pub fn in_memory() -> crate::error::Result<Self> {
        Ok(Self::new(SqliteStore::in_memory()?))
    }

    /// Create an in-memory bounded store with custom limits.
    pub fn in_memory_with_config(config: BoundedConfig) -> crate::error::Result<Self> {
        Ok(Self::with_config(SqliteStore::in_memory()?, config))
    }

    /// Get a reference to the config.
    pub fn config(&self) -> &BoundedConfig {
        &self.config
    }

    /// Get a reference to the underlying store.
    pub fn store(&self) -> &SqliteStore {
        &self.store
    }

    /// Insert an attestation and enforce eviction limits.
    /// Returns eviction counts for telemetry.
    pub fn put_bounded(&mut self, attestation: Attestation) -> StoreResult<EvictionResult> {
        // Insert first
        self.store.put(attestation.clone())?;

        // Enforce limits and collect eviction counts
        let result = self.enforce_limits(&attestation);

        // If enforcement failed, the attestation is still inserted
        // (matches Go behavior: enforcement errors are logged, not fatal)
        result
    }

    /// Enforce all three eviction rules after an insert.
    fn enforce_limits(&mut self, attestation: &Attestation) -> StoreResult<EvictionResult> {
        let mut result = EvictionResult::default();

        // 1. Enforce N attestations per (actor, context) — evict oldest
        for actor in &attestation.actors {
            for context in &attestation.contexts {
                let evicted = self.enforce_actor_context_limit(actor, context)?;
                result.actor_context_evictions += evicted;
            }
        }

        // 2. Enforce N contexts per actor — evict least-used contexts
        for actor in &attestation.actors {
            let evicted = self.enforce_actor_contexts_limit(actor)?;
            result.actor_contexts_evictions += evicted;
        }

        // 3. Enforce N actors per entity — evict least-recent actors
        for subject in &attestation.subjects {
            let evicted = self.enforce_entity_actors_limit(subject)?;
            result.entity_actors_evictions += evicted;
        }

        Ok(result)
    }

    /// Keep only N most recent attestations for this (actor, context) pair.
    fn enforce_actor_context_limit(&mut self, actor: &str, context: &str) -> StoreResult<usize> {
        let limit = self.config.actor_context_limit;

        let count: usize = self
            .store
            .connection()
            .query_row(
                "SELECT COUNT(*) FROM attestations
                 WHERE EXISTS (SELECT 1 FROM json_each(attestations.actors) WHERE value = ?)
                   AND EXISTS (SELECT 1 FROM json_each(attestations.contexts) WHERE value = ? COLLATE NOCASE)",
                rusqlite::params![actor, context],
                |row| row.get(0),
            )
            .map_err(SqliteError::from)?;

        if count > limit {
            let delete_count = count - limit;
            self.store
                .connection()
                .execute(
                    "DELETE FROM attestations WHERE id IN (
                        SELECT id FROM attestations
                        WHERE EXISTS (SELECT 1 FROM json_each(attestations.actors) WHERE value = ?)
                          AND EXISTS (SELECT 1 FROM json_each(attestations.contexts) WHERE value = ? COLLATE NOCASE)
                        ORDER BY timestamp ASC
                        LIMIT ?
                    )",
                    rusqlite::params![actor, context, delete_count],
                )
                .map_err(SqliteError::from)?;

            Ok(delete_count)
        } else {
            Ok(0)
        }
    }

    /// Keep only N most-used contexts per actor, evict all attestations in least-used contexts.
    fn enforce_actor_contexts_limit(&mut self, actor: &str) -> StoreResult<usize> {
        let limit = self.config.actor_contexts_limit;

        // Get all contexts for this actor, ordered by usage count ascending (least used first)
        let mut stmt = self
            .store
            .connection()
            .prepare(
                "SELECT DISTINCT json_extract(contexts, '$') as context_array, COUNT(*) as usage_count
                 FROM attestations
                 WHERE EXISTS (SELECT 1 FROM json_each(attestations.actors) WHERE value = ?)
                 GROUP BY context_array
                 ORDER BY usage_count ASC",
            )
            .map_err(SqliteError::from)?;

        let contexts: Vec<(String, usize)> = stmt
            .query_map([actor], |row| {
                Ok((row.get::<_, String>(0)?, row.get::<_, usize>(1)?))
            })
            .map_err(SqliteError::from)?
            .collect::<Result<Vec<_>, _>>()
            .map_err(SqliteError::from)?;

        if contexts.len() <= limit {
            return Ok(0);
        }

        // Evict attestations from least-used contexts
        let to_evict = contexts.len() - limit;
        let mut total_deleted = 0;

        for (context_array, _) in contexts.iter().take(to_evict) {
            let deleted = self
                .store
                .connection()
                .execute(
                    "DELETE FROM attestations
                     WHERE EXISTS (SELECT 1 FROM json_each(attestations.actors) WHERE value = ?)
                       AND contexts = ?",
                    rusqlite::params![actor, context_array],
                )
                .map_err(SqliteError::from)?;
            total_deleted += deleted;
        }

        Ok(total_deleted)
    }

    /// Keep only N most-recent actors per entity, evict all attestations from least-recent actors.
    fn enforce_entity_actors_limit(&mut self, entity: &str) -> StoreResult<usize> {
        let limit = self.config.entity_actors_limit;

        // Get all actors for this entity, ordered by last-seen ascending (least recent first)
        let mut stmt = self
            .store
            .connection()
            .prepare(
                "SELECT value as actor, MAX(timestamp) as last_seen
                 FROM attestations, json_each(actors)
                 WHERE EXISTS (SELECT 1 FROM json_each(attestations.subjects) WHERE value = ?)
                 GROUP BY actor
                 ORDER BY last_seen ASC",
            )
            .map_err(SqliteError::from)?;

        let actors: Vec<String> = stmt
            .query_map([entity], |row| row.get::<_, String>(0))
            .map_err(SqliteError::from)?
            .collect::<Result<Vec<_>, _>>()
            .map_err(SqliteError::from)?;

        if actors.len() <= limit {
            return Ok(0);
        }

        // Evict attestations from least-recent actors
        let to_evict = actors.len() - limit;
        let mut total_deleted = 0;

        for actor in actors.iter().take(to_evict) {
            let deleted = self
                .store
                .connection()
                .execute(
                    "DELETE FROM attestations
                     WHERE EXISTS (SELECT 1 FROM json_each(attestations.actors) WHERE value = ?)
                       AND EXISTS (SELECT 1 FROM json_each(attestations.subjects) WHERE value = ?)",
                    rusqlite::params![actor, entity],
                )
                .map_err(SqliteError::from)?;
            total_deleted += deleted;
        }

        Ok(total_deleted)
    }
}

// Delegate AttestationStore to inner store (put goes through put_bounded externally)
impl AttestationStore for BoundedStore {
    fn put(&mut self, attestation: Attestation) -> StoreResult<()> {
        self.put_bounded(attestation)?;
        Ok(())
    }

    fn get(&self, id: &str) -> StoreResult<Option<Attestation>> {
        self.store.get(id)
    }

    fn delete(&mut self, id: &str) -> StoreResult<bool> {
        self.store.delete(id)
    }

    fn update(&mut self, attestation: Attestation) -> StoreResult<()> {
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
        actor: &str,
    ) -> Attestation {
        AttestationBuilder::new()
            .id(id)
            .subject(subject)
            .predicate(predicate)
            .context(context)
            .actor(actor)
            .timestamp(1000)
            .source("test")
            .build()
    }

    fn timestamped(
        id: &str,
        subject: &str,
        predicate: &str,
        context: &str,
        actor: &str,
        ts: i64,
    ) -> Attestation {
        AttestationBuilder::new()
            .id(id)
            .subject(subject)
            .predicate(predicate)
            .context(context)
            .actor(actor)
            .timestamp(ts)
            .source("test")
            .build()
    }

    // --- actor-context limit (16 per pair, evicts oldest) ---

    #[test]
    fn actor_context_limit_evicts_oldest() {
        let config = BoundedConfig {
            actor_context_limit: 3,
            actor_contexts_limit: 100,
            entity_actors_limit: 100,
        };
        let mut store = BoundedStore::in_memory_with_config(config).unwrap();

        // Insert 3 at limit
        for i in 0..3 {
            store
                .put(timestamped(
                    &format!("AS-{}", i),
                    "ALICE",
                    "knows",
                    "work",
                    "human:bob",
                    1000 + i as i64,
                ))
                .unwrap();
        }
        assert_eq!(store.count().unwrap(), 3);

        // Insert 4th — should evict the oldest (AS-0, ts=1000)
        let result = store
            .put_bounded(timestamped(
                "AS-3", "ALICE", "knows", "work", "human:bob", 2000,
            ))
            .unwrap();
        assert_eq!(result.actor_context_evictions, 1);
        assert_eq!(store.count().unwrap(), 3);

        // Oldest (AS-0) should be gone
        assert!(store.get("AS-0").unwrap().is_none());
        assert!(store.get("AS-3").unwrap().is_some());
    }

    // --- actor-contexts limit (64 contexts per actor, evicts least-used) ---

    #[test]
    fn actor_contexts_limit_evicts_least_used() {
        // Limit is 3: the 4th context triggers eviction of the least-used one.
        let config = BoundedConfig {
            actor_context_limit: 100,
            actor_contexts_limit: 3,
            entity_actors_limit: 100,
        };
        let mut store = BoundedStore::in_memory_with_config(config).unwrap();

        // Context "work" gets 3 attestations (most used)
        store
            .put(create_test_attestation(
                "AS-1", "ALICE", "knows", "work", "human:bob",
            ))
            .unwrap();
        store
            .put(create_test_attestation(
                "AS-2", "ALICE", "helps", "work", "human:bob",
            ))
            .unwrap();
        store
            .put(create_test_attestation(
                "AS-3", "ALICE", "likes", "work", "human:bob",
            ))
            .unwrap();

        // Context "social" gets 2 attestations (second most used)
        store
            .put(create_test_attestation(
                "AS-4", "ALICE", "knows", "social", "human:bob",
            ))
            .unwrap();
        store
            .put(create_test_attestation(
                "AS-5", "ALICE", "helps", "social", "human:bob",
            ))
            .unwrap();

        // Context "hobby" gets 1 attestation (least used)
        store
            .put(create_test_attestation(
                "AS-6", "ALICE", "knows", "hobby", "human:bob",
            ))
            .unwrap();

        // 3 contexts at limit, no eviction yet
        assert_eq!(store.count().unwrap(), 6);

        // Adding context "family" pushes to 4 contexts, limit is 3.
        // One of the 1-use contexts (hobby or family) gets evicted.
        let result = store
            .put_bounded(create_test_attestation(
                "AS-7", "ALICE", "knows", "family", "human:bob",
            ))
            .unwrap();
        assert_eq!(result.actor_contexts_evictions, 1);

        // "work" (3 uses) always survives
        assert!(store.get("AS-1").unwrap().is_some());
        assert!(store.get("AS-2").unwrap().is_some());
        assert!(store.get("AS-3").unwrap().is_some());

        // "social" (2 uses) always survives — higher usage than any eviction candidate
        assert!(store.get("AS-4").unwrap().is_some());
        assert!(store.get("AS-5").unwrap().is_some());

        // Exactly 3 contexts remain after eviction
        let remaining_contexts = store.contexts().unwrap();
        assert_eq!(remaining_contexts.len(), 3);
        assert!(remaining_contexts.contains(&"work".to_string()));
        assert!(remaining_contexts.contains(&"social".to_string()));
    }

    // --- entity-actors limit (64 actors per subject, evicts least-recent) ---

    #[test]
    fn entity_actors_limit_evicts_least_recent() {
        let config = BoundedConfig {
            actor_context_limit: 100,
            actor_contexts_limit: 100,
            entity_actors_limit: 2,
        };
        let mut store = BoundedStore::in_memory_with_config(config).unwrap();

        // Actor "old" — oldest timestamp
        store
            .put(timestamped(
                "AS-1",
                "ALICE",
                "knows",
                "work",
                "human:old",
                1000,
            ))
            .unwrap();

        // Actor "mid" — middle timestamp
        store
            .put(timestamped(
                "AS-2",
                "ALICE",
                "knows",
                "work",
                "human:mid",
                2000,
            ))
            .unwrap();

        assert_eq!(store.count().unwrap(), 2);

        // Actor "new" — newest, pushes to 3 actors for subject ALICE
        // "old" (last_seen=1000) should be evicted
        let result = store
            .put_bounded(timestamped(
                "AS-3",
                "ALICE",
                "knows",
                "work",
                "human:new",
                3000,
            ))
            .unwrap();
        assert_eq!(result.entity_actors_evictions, 1);

        // old actor's attestation gone
        assert!(store.get("AS-1").unwrap().is_none());
        // mid and new kept
        assert!(store.get("AS-2").unwrap().is_some());
        assert!(store.get("AS-3").unwrap().is_some());
    }

    // --- no eviction when within limits ---

    #[test]
    fn no_eviction_within_limits() {
        let mut store = BoundedStore::in_memory().unwrap();

        let result = store
            .put_bounded(create_test_attestation(
                "AS-1",
                "ALICE",
                "knows",
                "work",
                "human:bob",
            ))
            .unwrap();

        assert_eq!(result.total(), 0);
        assert_eq!(store.count().unwrap(), 1);
    }

    // --- signature round-trip through bounded store ---

    #[test]
    fn signature_round_trip() {
        let mut store = BoundedStore::in_memory().unwrap();

        let attestation = AttestationBuilder::new()
            .id("AS-signed")
            .subject("ALICE")
            .predicate("knows")
            .context("work")
            .actor("human:bob")
            .timestamp(1000)
            .source("test")
            .signature(vec![0xDE, 0xAD, 0xBE, 0xEF])
            .signer_did("did:key:z123")
            .build();

        store.put(attestation).unwrap();

        let retrieved = store.get("AS-signed").unwrap().unwrap();
        assert_eq!(retrieved.signature, Some(vec![0xDE, 0xAD, 0xBE, 0xEF]));
        assert_eq!(retrieved.signer_did, Some("did:key:z123".to_string()));
    }
}
