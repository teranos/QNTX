//! Bounded storage enforcement for SqliteStore
//!
//! Implements the 16/64/64 enforcement strategy:
//! - N attestations per (actor, context) pair — delete oldest
//! - N contexts per actor — delete least-used contexts
//! - N actors per entity/subject — delete least-recent actors
//!
//! Enforcement checks use counter tables (enforcement_actor_context, etc.)
//! for O(1) lookups instead of scanning junction tables with JOINs.
//! Counters are maintained incrementally in put().
//!
//! Also handles telemetry logging to the storage_events table.

use qntx_core::storage::enforcement::{EnforcementEvent, EnforcementInput, EvictionDetails};

use std::collections::HashSet;

use crate::error::SqliteError;
use crate::SqliteStore;

/// Extract distinct predicate strings from rows where each row has a JSON array
/// of predicates (e.g. `["knows","likes"]`). Returns up to `cap` unique values.
fn collect_distinct_predicates(
    rows: &mut rusqlite::Rows<'_>,
    cap: usize,
) -> Result<Vec<String>, SqliteError> {
    let mut seen = HashSet::new();
    while let Some(row) = rows.next()? {
        let pred_json: String = row.get(0)?;
        // Each row's predicates column is a JSON array of strings
        if let Ok(preds) = serde_json::from_str::<Vec<String>>(&pred_json) {
            for p in preds {
                if seen.len() >= cap {
                    return Ok(seen.into_iter().collect());
                }
                seen.insert(p);
            }
        }
    }
    let mut result: Vec<String> = seen.into_iter().collect();
    result.sort();
    Ok(result)
}

impl SqliteStore {
    /// Run all enforcement checks for the given actors, contexts, and subjects.
    /// Uses counter tables for O(1) limit checks — only runs expensive DELETE
    /// queries when a limit is actually exceeded.
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
    /// Counter lookup is O(1) — only runs DELETE when limit is exceeded.
    fn enforce_actor_context_limit(
        &self,
        actor: &str,
        context: &str,
        limit: usize,
    ) -> Result<Option<EnforcementEvent>, SqliteError> {
        // O(1) counter lookup instead of COUNT + JOIN
        let count: i64 = self
            .conn
            .query_row(
                "SELECT count FROM enforcement_actor_context WHERE actor = ?1 AND context = ?2",
                rusqlite::params![actor, context],
                |row| row.get::<_, i64>(0),
            )
            .unwrap_or(0);

        if count as usize <= limit {
            return Ok(None);
        }

        let delete_count = count as usize - limit;

        // Collect distinct predicates and oldest timestamp from the exact rows being deleted
        let details = {
            let oldest: Option<String> = self.conn.query_row(
                "SELECT MIN(att.timestamp)
                 FROM attestations att
                 JOIN attestation_actors a ON att.id = a.attestation_id
                 JOIN attestation_contexts c ON att.id = c.attestation_id
                 WHERE att.id IN (
                     SELECT att2.id FROM attestations att2
                     JOIN attestation_actors a2 ON att2.id = a2.attestation_id
                     JOIN attestation_contexts c2 ON att2.id = c2.attestation_id
                     WHERE a2.actor = ?1 AND c2.context = ?2
                     ORDER BY att2.timestamp ASC
                     LIMIT ?3
                 )",
                rusqlite::params![actor, context, delete_count],
                |row| row.get(0),
            ).ok().flatten();

            let mut stmt = self.conn.prepare(
                "SELECT DISTINCT att.predicates
                 FROM attestations att
                 JOIN attestation_actors a ON att.id = a.attestation_id
                 JOIN attestation_contexts c ON att.id = c.attestation_id
                 WHERE att.id IN (
                     SELECT att2.id FROM attestations att2
                     JOIN attestation_actors a2 ON att2.id = a2.attestation_id
                     JOIN attestation_contexts c2 ON att2.id = c2.attestation_id
                     WHERE a2.actor = ?1 AND c2.context = ?2
                     ORDER BY att2.timestamp ASC
                     LIMIT ?3
                 )",
            )?;
            let mut rows = stmt.query(rusqlite::params![actor, context, delete_count])?;
            let predicates = collect_distinct_predicates(&mut rows, 15)?;
            EvictionDetails {
                evicted_actors: Vec::new(),
                evicted_contexts: Vec::new(),
                predicates,
                last_seen: oldest,
            }
        };

        // Delete oldest attestations (CASCADE deletes junction rows)
        let deleted = self.conn.execute(
            "DELETE FROM attestations
             WHERE id IN (
                 SELECT att.id FROM attestations att
                 JOIN attestation_actors a ON att.id = a.attestation_id
                 JOIN attestation_contexts c ON att.id = c.attestation_id
                 WHERE a.actor = ?1 AND c.context = ?2 COLLATE NOCASE
                 ORDER BY att.timestamp ASC
                 LIMIT ?3
             )",
            rusqlite::params![actor, context, delete_count],
        )?;

        // Update counter to reflect deletions
        self.conn.execute(
            "UPDATE enforcement_actor_context SET count = count - ?3
             WHERE actor = ?1 AND context = ?2",
            rusqlite::params![actor, context, deleted],
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
    /// Counter lookup is O(1) — only queries context details when limit is exceeded.
    fn enforce_actor_contexts_limit(
        &self,
        actor: &str,
        limit: usize,
    ) -> Result<Option<EnforcementEvent>, SqliteError> {
        // O(1) counter lookup
        let count: i64 = self
            .conn
            .query_row(
                "SELECT count FROM enforcement_actor_contexts WHERE actor = ?1",
                rusqlite::params![actor],
                |row| row.get::<_, i64>(0),
            )
            .unwrap_or(0);

        if count as usize <= limit {
            return Ok(None);
        }

        // Only now do we need to find which contexts to evict — query the
        // counter table (small) instead of junction tables (huge).
        let mut stmt = self.conn.prepare(
            "SELECT context, count FROM enforcement_actor_context
             WHERE actor = ?1
             ORDER BY count ASC",
        )?;

        struct ContextUsage {
            context: String,
            _usage_count: i64,
        }

        let contexts: Vec<ContextUsage> = {
            let mut rows = stmt.query(rusqlite::params![actor])?;
            let mut result = Vec::new();
            while let Some(row) = rows.next()? {
                result.push(ContextUsage {
                    context: row.get(0)?,
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

        let ctx_list: Vec<&str> = contexts_to_delete.iter().map(|cu| cu.context.as_str()).collect();
        let placeholders: Vec<String> = (0..ctx_list.len()).map(|i| format!("?{}", i + 2)).collect();

        let mut details = EvictionDetails {
            evicted_actors: Vec::new(),
            evicted_contexts: Vec::new(),
            predicates: Vec::new(),
            last_seen: None,
        };

        // Query oldest timestamp from all contexts being evicted
        {
            let query = format!(
                "SELECT MIN(att.timestamp)
                 FROM attestations att
                 JOIN attestation_actors a ON att.id = a.attestation_id
                 JOIN attestation_contexts c ON att.id = c.attestation_id
                 WHERE a.actor = ?1 AND c.context IN ({})",
                placeholders.join(", ")
            );
            let mut stmt = self.conn.prepare(&query)?;
            let mut params: Vec<Box<dyn rusqlite::types::ToSql>> = Vec::new();
            params.push(Box::new(actor.to_string()));
            for ctx in &ctx_list {
                params.push(Box::new(ctx.to_string()));
            }
            let param_refs: Vec<&dyn rusqlite::types::ToSql> = params.iter().map(|p| p.as_ref()).collect();
            details.last_seen = stmt.query_row(param_refs.as_slice(), |row| row.get(0)).ok().flatten();
        }

        // Collect distinct predicates from all contexts being evicted
        {
            let query = format!(
                "SELECT DISTINCT att.predicates
                 FROM attestations att
                 JOIN attestation_actors a ON att.id = a.attestation_id
                 JOIN attestation_contexts c ON att.id = c.attestation_id
                 WHERE a.actor = ?1 AND c.context IN ({})",
                placeholders.join(", ")
            );
            let mut stmt = self.conn.prepare(&query)?;
            let mut params: Vec<Box<dyn rusqlite::types::ToSql>> = Vec::new();
            params.push(Box::new(actor.to_string()));
            for ctx in &ctx_list {
                params.push(Box::new(ctx.to_string()));
            }
            let param_refs: Vec<&dyn rusqlite::types::ToSql> = params.iter().map(|p| p.as_ref()).collect();
            let mut rows = stmt.query(param_refs.as_slice())?;
            details.predicates = collect_distinct_predicates(&mut rows, 15)?;
        }

        for cu in contexts_to_delete.iter() {
            details.evicted_contexts.push(cu.context.clone());

            // Delete all attestations with this context for this actor
            let deleted = self.conn.execute(
                "DELETE FROM attestations
                 WHERE id IN (
                     SELECT a.attestation_id
                     FROM attestation_actors a
                     JOIN attestation_contexts c ON a.attestation_id = c.attestation_id
                     WHERE a.actor = ?1 AND c.context = ?2
                 )",
                rusqlite::params![actor, cu.context],
            )?;
            total_deleted += deleted;

            // Remove counter row for this evicted (actor, context) pair
            self.conn.execute(
                "DELETE FROM enforcement_actor_context WHERE actor = ?1 AND context = ?2",
                rusqlite::params![actor, cu.context],
            )?;
        }

        // Update distinct context count for this actor
        let remaining: i64 = self
            .conn
            .query_row(
                "SELECT COUNT(*) FROM enforcement_actor_context WHERE actor = ?1",
                rusqlite::params![actor],
                |row| row.get(0),
            )
            .unwrap_or(0);
        self.conn.execute(
            "INSERT INTO enforcement_actor_contexts (actor, count) VALUES (?1, ?2)
             ON CONFLICT(actor) DO UPDATE SET count = ?2",
            rusqlite::params![actor, remaining],
        )?;

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
    /// Counter lookup is O(1) — only queries actor details when limit is exceeded.
    fn enforce_entity_actors_limit(
        &self,
        entity: &str,
        limit: usize,
    ) -> Result<Option<EnforcementEvent>, SqliteError> {
        // O(1) counter lookup
        let count: i64 = self
            .conn
            .query_row(
                "SELECT count FROM enforcement_entity_actors WHERE subject = ?1",
                rusqlite::params![entity],
                |row| row.get::<_, i64>(0),
            )
            .unwrap_or(0);

        if count as usize <= limit {
            return Ok(None);
        }

        // Only now query which actors to evict
        let mut stmt = self.conn.prepare(
            "SELECT a.actor, MAX(att.timestamp) as last_seen
             FROM attestation_actors a
             JOIN attestation_subjects s ON a.attestation_id = s.attestation_id
             JOIN attestations att ON att.id = a.attestation_id
             WHERE s.subject = ?1
             GROUP BY a.actor
             ORDER BY last_seen ASC",
        )?;

        let actors: Vec<String> = {
            let mut rows = stmt.query(rusqlite::params![entity])?;
            let mut result = Vec::new();
            while let Some(row) = rows.next()? {
                result.push(row.get::<_, String>(0)?);
            }
            result
        };

        if actors.len() <= limit {
            return Ok(None);
        }

        let actors_to_delete = &actors[..actors.len() - limit];
        let mut total_deleted = 0usize;

        let actor_list: Vec<&str> = actors_to_delete.iter().map(|a| a.as_str()).collect();
        let placeholders: Vec<String> = (0..actor_list.len()).map(|i| format!("?{}", i + 2)).collect();

        let mut details = EvictionDetails {
            evicted_actors: Vec::new(),
            evicted_contexts: Vec::new(),
            predicates: Vec::new(),
            last_seen: None,
        };

        // Query oldest timestamp from all actors being evicted
        {
            let query = format!(
                "SELECT MIN(att.timestamp)
                 FROM attestations att
                 JOIN attestation_actors a ON att.id = a.attestation_id
                 JOIN attestation_subjects s ON att.id = s.attestation_id
                 WHERE s.subject = ?1 AND a.actor IN ({})",
                placeholders.join(", ")
            );
            let mut stmt = self.conn.prepare(&query)?;
            let mut params: Vec<Box<dyn rusqlite::types::ToSql>> = Vec::new();
            params.push(Box::new(entity.to_string()));
            for actor in &actor_list {
                params.push(Box::new(actor.to_string()));
            }
            let param_refs: Vec<&dyn rusqlite::types::ToSql> = params.iter().map(|p| p.as_ref()).collect();
            details.last_seen = stmt.query_row(param_refs.as_slice(), |row| row.get(0)).ok().flatten();
        }

        // Collect distinct predicates from all actors being evicted
        {
            let query = format!(
                "SELECT DISTINCT att.predicates
                 FROM attestations att
                 JOIN attestation_actors a ON att.id = a.attestation_id
                 JOIN attestation_subjects s ON att.id = s.attestation_id
                 WHERE s.subject = ?1 AND a.actor IN ({})",
                placeholders.join(", ")
            );
            let mut stmt = self.conn.prepare(&query)?;
            let mut params: Vec<Box<dyn rusqlite::types::ToSql>> = Vec::new();
            params.push(Box::new(entity.to_string()));
            for actor in &actor_list {
                params.push(Box::new(actor.to_string()));
            }
            let param_refs: Vec<&dyn rusqlite::types::ToSql> = params.iter().map(|p| p.as_ref()).collect();
            let mut rows = stmt.query(param_refs.as_slice())?;
            details.predicates = collect_distinct_predicates(&mut rows, 15)?;
        }

        for actor in actors_to_delete.iter() {
            details.evicted_actors.push(actor.clone());

            // Delete all attestations by this actor that mention this entity
            let deleted = self.conn.execute(
                "DELETE FROM attestations
                 WHERE id IN (
                     SELECT a.attestation_id
                     FROM attestation_actors a
                     JOIN attestation_subjects s ON a.attestation_id = s.attestation_id
                     WHERE a.actor = ?1 AND s.subject = ?2
                 )",
                rusqlite::params![actor, entity],
            )?;
            total_deleted += deleted;

            // Remove detail tracking row
            self.conn.execute(
                "DELETE FROM enforcement_entity_actors_detail WHERE subject = ?1 AND actor = ?2",
                rusqlite::params![entity, actor],
            )?;
        }

        // Update counter to reflect actual remaining actors
        let remaining: i64 = self
            .conn
            .query_row(
                "SELECT COUNT(*) FROM enforcement_entity_actors_detail WHERE subject = ?1",
                rusqlite::params![entity],
                |row| row.get(0),
            )
            .unwrap_or(0);
        self.conn.execute(
            "INSERT INTO enforcement_entity_actors (subject, count) VALUES (?1, ?2)
             ON CONFLICT(subject) DO UPDATE SET count = ?2",
            rusqlite::params![entity, remaining],
        )?;

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
