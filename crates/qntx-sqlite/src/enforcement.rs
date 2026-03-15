//! Bounded storage enforcement for SqliteStore
//!
//! Implements the 16/64/64 enforcement strategy:
//! - N attestations per (actor, context) pair — delete oldest
//! - N contexts per actor — delete least-used contexts
//! - N actors per entity/subject — delete least-recent actors
//!
//! Also handles telemetry logging to the storage_events table.

use qntx_core::storage::enforcement::{EnforcementEvent, EnforcementInput, EvictionDetails};

use crate::error::SqliteError;
use crate::SqliteStore;

impl SqliteStore {
    /// Run all enforcement checks for the given actors, contexts, and subjects.
    /// Returns a list of enforcement events for telemetry logging.
    pub fn enforce_limits(
        &mut self,
        input: &EnforcementInput,
    ) -> Result<Vec<EnforcementEvent>, SqliteError> {
        let mut events = Vec::new();

        // 1. Enforce N attestations per (actor, context) pair
        for actor in &input.actors {
            for context in &input.contexts {
                match self.enforce_actor_context_limit(
                    actor,
                    context,
                    input.config.actor_context_limit,
                ) {
                    Ok(Some(ev)) => events.push(ev),
                    Ok(None) => {}
                    Err(e) => {
                        eprintln!(
                            "qntx-sqlite: enforce_actor_context_limit failed for actor={} context={}: {}",
                            actor, context, e
                        );
                    }
                }
            }
        }

        // 2. Enforce N contexts per actor
        for actor in &input.actors {
            match self.enforce_actor_contexts_limit(actor, input.config.actor_contexts_limit) {
                Ok(Some(ev)) => events.push(ev),
                Ok(None) => {}
                Err(e) => {
                    eprintln!(
                        "qntx-sqlite: enforce_actor_contexts_limit failed for actor={}: {}",
                        actor, e
                    );
                }
            }
        }

        // 3. Enforce N actors per entity/subject
        for subject in &input.subjects {
            match self.enforce_entity_actors_limit(subject, input.config.entity_actors_limit) {
                Ok(Some(ev)) => events.push(ev),
                Ok(None) => {}
                Err(e) => {
                    eprintln!(
                        "qntx-sqlite: enforce_entity_actors_limit failed for subject={}: {}",
                        subject, e
                    );
                }
            }
        }

        // Log all events to storage_events table
        for ev in &events {
            if let Err(e) = self.log_storage_event(ev) {
                eprintln!("qntx-sqlite: failed to log storage event: {}", e);
            }
        }

        Ok(events)
    }

    /// Enforce actor_context_limit: keep only N most recent attestations per (actor, context).
    fn enforce_actor_context_limit(
        &self,
        actor: &str,
        context: &str,
        limit: usize,
    ) -> Result<Option<EnforcementEvent>, SqliteError> {
        let count: i64 = self.conn.query_row(
            "SELECT COUNT(*) FROM attestations
             WHERE EXISTS (
                 SELECT 1 FROM json_each(attestations.actors) WHERE value = ?1
             ) AND EXISTS (
                 SELECT 1 FROM json_each(attestations.contexts) WHERE value = ?2 COLLATE NOCASE
             )",
            rusqlite::params![actor, context],
            |row| row.get::<_, i64>(0),
        )?;

        if count as usize <= limit {
            return Ok(None);
        }

        let delete_count = count as usize - limit;

        // Collect sample data before deletion
        let mut details = EvictionDetails {
            evicted_actors: Vec::new(),
            evicted_contexts: Vec::new(),
            sample_predicates: Vec::new(),
            sample_subjects: Vec::new(),
            last_seen: None,
        };

        {
            let mut stmt = self.conn.prepare(
                "SELECT predicates, subjects, timestamp FROM attestations, json_each(actors) as a, json_each(contexts) as c
                 WHERE a.value = ?1 AND c.value = ?2
                 ORDER BY timestamp ASC
                 LIMIT 3",
            )?;
            let mut rows = stmt.query(rusqlite::params![actor, context])?;
            while let Some(row) = rows.next()? {
                let pred_json: String = row.get(0)?;
                let subj_json: String = row.get(1)?;
                let timestamp: String = row.get(2)?;
                details.sample_predicates.push(pred_json);
                details.sample_subjects.push(subj_json);
                if details.last_seen.is_none()
                    || details.last_seen.as_deref() < Some(timestamp.as_str())
                {
                    details.last_seen = Some(timestamp);
                }
            }
        }

        // Delete oldest attestations
        self.conn.execute(
            "DELETE FROM attestations
             WHERE id IN (
                 SELECT id FROM attestations
                 WHERE EXISTS (
                     SELECT 1 FROM json_each(attestations.actors) WHERE value = ?1
                 ) AND EXISTS (
                     SELECT 1 FROM json_each(attestations.contexts) WHERE value = ?2 COLLATE NOCASE
                 )
                 ORDER BY timestamp ASC
                 LIMIT ?3
             )",
            rusqlite::params![actor, context, delete_count],
        )?;

        Ok(Some(EnforcementEvent {
            event_type: "actor_context_limit".to_string(),
            actor: Some(actor.to_string()),
            context: Some(context.to_string()),
            entity: None,
            deleted_count: delete_count,
            limit_value: limit,
            eviction_details: Some(details),
        }))
    }

    /// Enforce actor_contexts_limit: keep only N most-used contexts per actor.
    fn enforce_actor_contexts_limit(
        &self,
        actor: &str,
        limit: usize,
    ) -> Result<Option<EnforcementEvent>, SqliteError> {
        // Get all contexts for this actor with usage counts, ordered by usage ASC
        let mut stmt = self.conn.prepare(
            "SELECT DISTINCT json_extract(contexts, '$') as context_array, COUNT(*) as usage_count
             FROM attestations
             WHERE EXISTS (
                 SELECT 1 FROM json_each(attestations.actors) WHERE value = ?1
             )
             GROUP BY context_array
             ORDER BY usage_count ASC",
        )?;

        struct ContextUsage {
            context_array: String,
            _usage_count: i64,
        }

        let contexts: Vec<ContextUsage> = {
            let mut rows = stmt.query(rusqlite::params![actor])?;
            let mut result = Vec::new();
            while let Some(row) = rows.next()? {
                result.push(ContextUsage {
                    context_array: row.get(0)?,
                    _usage_count: row.get(1)?,
                });
            }
            result
        };

        if contexts.len() <= limit {
            return Ok(None);
        }

        let contexts_to_delete = &contexts[..contexts.len() - limit];
        let mut total_deleted = 0usize;

        let mut details = EvictionDetails {
            evicted_actors: Vec::new(),
            evicted_contexts: Vec::new(),
            sample_predicates: Vec::new(),
            sample_subjects: Vec::new(),
            last_seen: None,
        };

        for (i, cu) in contexts_to_delete.iter().enumerate() {
            details.evicted_contexts.push(cu.context_array.clone());

            // Collect sample data from first context being evicted
            if i == 0 {
                let mut sample_stmt = self.conn.prepare(
                    "SELECT predicates, subjects, timestamp FROM attestations
                     WHERE EXISTS (
                         SELECT 1 FROM json_each(attestations.actors) WHERE value = ?1
                     ) AND contexts = ?2
                     LIMIT 3",
                )?;
                let mut rows = sample_stmt.query(rusqlite::params![actor, cu.context_array])?;
                while let Some(row) = rows.next()? {
                    let pred_json: String = row.get(0)?;
                    let subj_json: String = row.get(1)?;
                    let timestamp: String = row.get(2)?;
                    details.sample_predicates.push(pred_json);
                    details.sample_subjects.push(subj_json);
                    if details.last_seen.is_none()
                        || details.last_seen.as_deref() < Some(timestamp.as_str())
                    {
                        details.last_seen = Some(timestamp);
                    }
                }
            }

            // Delete all attestations with this context array for this actor
            let deleted = self.conn.execute(
                "DELETE FROM attestations
                 WHERE EXISTS (
                     SELECT 1 FROM json_each(attestations.actors) WHERE value = ?1
                 ) AND contexts = ?2",
                rusqlite::params![actor, cu.context_array],
            )?;
            total_deleted += deleted;
        }

        if total_deleted == 0 {
            return Ok(None);
        }

        Ok(Some(EnforcementEvent {
            event_type: "actor_contexts_limit".to_string(),
            actor: Some(actor.to_string()),
            context: None,
            entity: None,
            deleted_count: total_deleted,
            limit_value: limit,
            eviction_details: Some(details),
        }))
    }

    /// Enforce entity_actors_limit: keep only N most-recent actors per entity/subject.
    fn enforce_entity_actors_limit(
        &self,
        entity: &str,
        limit: usize,
    ) -> Result<Option<EnforcementEvent>, SqliteError> {
        // Get all actors for this entity with most recent timestamps, ordered ASC
        let mut stmt = self.conn.prepare(
            "SELECT value as actor, MAX(timestamp) as last_seen
             FROM attestations, json_each(actors)
             WHERE EXISTS (
                 SELECT 1 FROM json_each(attestations.subjects) WHERE value = ?1
             )
             GROUP BY actor
             ORDER BY last_seen ASC",
        )?;

        struct ActorInfo {
            actor: String,
            last_seen: String,
        }

        let actors: Vec<ActorInfo> = {
            let mut rows = stmt.query(rusqlite::params![entity])?;
            let mut result = Vec::new();
            while let Some(row) = rows.next()? {
                result.push(ActorInfo {
                    actor: row.get(0)?,
                    last_seen: row.get(1)?,
                });
            }
            result
        };

        if actors.len() <= limit {
            return Ok(None);
        }

        let actors_to_delete = &actors[..actors.len() - limit];
        let mut total_deleted = 0usize;

        let mut details = EvictionDetails {
            evicted_actors: Vec::new(),
            evicted_contexts: Vec::new(),
            sample_predicates: Vec::new(),
            sample_subjects: Vec::new(),
            last_seen: None,
        };

        for (i, ai) in actors_to_delete.iter().enumerate() {
            details.evicted_actors.push(ai.actor.clone());
            if details.last_seen.is_none()
                || details.last_seen.as_deref() < Some(ai.last_seen.as_str())
            {
                details.last_seen = Some(ai.last_seen.clone());
            }

            // Collect sample data from first actor being evicted
            if i == 0 {
                let mut sample_stmt = self.conn.prepare(
                    "SELECT predicates, subjects FROM attestations
                     WHERE EXISTS (
                         SELECT 1 FROM json_each(attestations.actors) WHERE value = ?1
                     ) AND EXISTS (
                         SELECT 1 FROM json_each(attestations.subjects) WHERE value = ?2
                     ) LIMIT 3",
                )?;
                let mut rows = sample_stmt.query(rusqlite::params![ai.actor, entity])?;
                while let Some(row) = rows.next()? {
                    let pred_json: String = row.get(0)?;
                    let subj_json: String = row.get(1)?;
                    details.sample_predicates.push(pred_json);
                    details.sample_subjects.push(subj_json);
                }
            }

            // Delete all attestations by this actor that mention this entity
            let deleted = self.conn.execute(
                "DELETE FROM attestations
                 WHERE EXISTS (
                     SELECT 1 FROM json_each(attestations.actors) WHERE value = ?1
                 ) AND EXISTS (
                     SELECT 1 FROM json_each(attestations.subjects) WHERE value = ?2
                 )",
                rusqlite::params![ai.actor, entity],
            )?;
            total_deleted += deleted;
        }

        if total_deleted == 0 {
            return Ok(None);
        }

        Ok(Some(EnforcementEvent {
            event_type: "entity_actors_limit".to_string(),
            actor: None,
            context: None,
            entity: Some(entity.to_string()),
            deleted_count: total_deleted,
            limit_value: limit,
            eviction_details: Some(details),
        }))
    }

    /// Log an enforcement event to the storage_events table.
    fn log_storage_event(&self, event: &EnforcementEvent) -> Result<(), SqliteError> {
        let timestamp = chrono::Utc::now().to_rfc3339();

        let details_json: Option<String> = event
            .eviction_details
            .as_ref()
            .and_then(|d| serde_json::to_string(d).ok());

        self.conn.execute(
            "INSERT INTO storage_events (event_type, actor, context, entity, deletions_count, limit_value, timestamp, eviction_details)
             VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8)",
            rusqlite::params![
                event.event_type,
                event.actor,
                event.context,
                event.entity,
                event.deleted_count,
                event.limit_value,
                timestamp,
                details_json,
            ],
        )?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use qntx_core::attestation::AttestationBuilder;
    use qntx_core::storage::enforcement::{EnforcementConfig, EnforcementInput};
    use qntx_core::storage::AttestationStore;

    fn make_store() -> SqliteStore {
        SqliteStore::in_memory().unwrap()
    }

    fn insert_attestation(
        store: &mut SqliteStore,
        id: &str,
        actor: &str,
        context: &str,
        subject: &str,
        predicate: &str,
    ) {
        let att = AttestationBuilder::new()
            .id(id)
            .actor(actor)
            .context(context)
            .subject(subject)
            .predicate(predicate)
            .build();
        store.put(att).unwrap();
    }

    #[test]
    fn test_actor_context_limit_no_enforcement_needed() {
        let mut store = make_store();
        insert_attestation(&mut store, "AS-1", "bob", "work", "ALICE", "knows");

        let input = EnforcementInput {
            actors: vec!["bob".to_string()],
            contexts: vec!["work".to_string()],
            subjects: vec!["ALICE".to_string()],
            config: EnforcementConfig::default(), // limit=16
        };

        let events = store.enforce_limits(&input).unwrap();
        assert!(events.is_empty());
    }

    #[test]
    fn test_actor_context_limit_deletes_oldest() {
        let mut store = make_store();

        // Insert 3 attestations, limit to 2
        insert_attestation(&mut store, "AS-1", "bob", "work", "ALICE", "knows");
        insert_attestation(&mut store, "AS-2", "bob", "work", "ALICE", "likes");
        insert_attestation(&mut store, "AS-3", "bob", "work", "ALICE", "trusts");

        let input = EnforcementInput {
            actors: vec!["bob".to_string()],
            contexts: vec!["work".to_string()],
            subjects: vec!["ALICE".to_string()],
            config: EnforcementConfig {
                actor_context_limit: 2,
                actor_contexts_limit: 64,
                entity_actors_limit: 64,
            },
        };

        let events = store.enforce_limits(&input).unwrap();
        assert_eq!(events.len(), 1);
        assert_eq!(events[0].event_type, "actor_context_limit");
        assert_eq!(events[0].deleted_count, 1);
        assert_eq!(events[0].limit_value, 2);

        // Verify count is now at limit
        let count = store.count().unwrap();
        assert_eq!(count, 2);
    }

    #[test]
    fn test_entity_actors_limit() {
        let mut store = make_store();

        // 3 different actors for the same subject
        insert_attestation(&mut store, "AS-1", "actor1", "ctx", "ENTITY", "pred");
        insert_attestation(&mut store, "AS-2", "actor2", "ctx", "ENTITY", "pred");
        insert_attestation(&mut store, "AS-3", "actor3", "ctx", "ENTITY", "pred");

        let input = EnforcementInput {
            actors: vec!["actor3".to_string()],
            contexts: vec!["ctx".to_string()],
            subjects: vec!["ENTITY".to_string()],
            config: EnforcementConfig {
                actor_context_limit: 16,
                actor_contexts_limit: 64,
                entity_actors_limit: 2,
            },
        };

        let events = store.enforce_limits(&input).unwrap();
        // Should have an entity_actors_limit event
        let entity_events: Vec<_> = events
            .iter()
            .filter(|e| e.event_type == "entity_actors_limit")
            .collect();
        assert_eq!(entity_events.len(), 1);
        assert!(entity_events[0].deleted_count >= 1);
    }

    #[test]
    fn test_storage_event_logged() {
        let mut store = make_store();

        // Insert enough to trigger enforcement
        insert_attestation(&mut store, "AS-1", "bob", "work", "ALICE", "knows");
        insert_attestation(&mut store, "AS-2", "bob", "work", "ALICE", "likes");
        insert_attestation(&mut store, "AS-3", "bob", "work", "ALICE", "trusts");

        let input = EnforcementInput {
            actors: vec!["bob".to_string()],
            contexts: vec!["work".to_string()],
            subjects: vec!["ALICE".to_string()],
            config: EnforcementConfig {
                actor_context_limit: 2,
                ..Default::default()
            },
        };

        store.enforce_limits(&input).unwrap();

        // Verify storage_events table has entries
        let count: i64 = store
            .connection()
            .query_row("SELECT COUNT(*) FROM storage_events", [], |row| row.get(0))
            .unwrap();
        assert!(count > 0, "expected storage events to be logged");
    }
}
