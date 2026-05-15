//! Bounded storage enforcement for SqliteStore
//!
//! Implements the 32/64/64 enforcement strategy:
//! - N attestations per (actor, context) pair — delete oldest
//! - N contexts per actor — delete least-used contexts
//! - N actors per entity/subject — delete least-recent actors
//!
//! Enforcement checks use junction table COUNT queries with existing indices.
//! Also handles telemetry logging to the storage_events table.

use qntx_core::attestation::Attestation;
use qntx_core::storage::enforcement::{EnforcementEvent, EnforcementInput, EvictionDetails};

use std::collections::HashSet;

use crate::distill;
use crate::error::SqliteError;
use crate::store::put_attestation;
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
        // Use SAVEPOINT instead of BEGIN IMMEDIATE — enforce_limits is called
        // from put(), which may already be inside a transaction (e.g. when Go's
        // database/sql driver calls sql_begin before routing through storage_put).
        // SAVEPOINTs nest safely within existing transactions.
        self.conn.execute_batch("SAVEPOINT enforce_limits")?;

        let result = self.enforce_limits_inner(input);

        match &result {
            Ok(_) => {
                self.conn.execute_batch("RELEASE SAVEPOINT enforce_limits")?;
            }
            Err(_) => {
                let _ = self.conn.execute_batch("ROLLBACK TO SAVEPOINT enforce_limits");
            }
        }

        result
    }

    pub(crate) fn enforce_limits_inner(
        &mut self,
        input: &EnforcementInput,
    ) -> Result<Vec<EnforcementEvent>, SqliteError> {
        let total_start = std::time::Instant::now();
        let mut events = Vec::new();

        // 1. Enforce N attestations per (actor, context) pair
        let t1 = std::time::Instant::now();
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
        let d1 = t1.elapsed();

        // 2. Enforce N contexts per actor
        let t2 = std::time::Instant::now();
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
        let d2 = t2.elapsed();

        // 3. Enforce N actors per entity/subject
        let t3 = std::time::Instant::now();
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
        let d3 = t3.elapsed();

        // Log all events to storage_events table
        for ev in &events {
            if let Err(e) = self.log_storage_event(ev) {
                eprintln!("qntx-sqlite: failed to log storage event: {}", e);
            }
        }

        let total = total_start.elapsed();
        if total.as_millis() > 100 {
            eprintln!(
                "qntx-sqlite: enforce_limits took {}ms (ac={}ms acs={}ms ea={}ms) evictions={}",
                total.as_millis(),
                d1.as_millis(),
                d2.as_millis(),
                d3.as_millis(),
                events.len(),
            );
        }

        Ok(events)
    }

    /// Enforce actor_context_limit: keep only N most recent attestations per (actor, context).
    /// Uses half-bound threshold: triggers at `limit + limit/2`, evicts down to `limit`.
    /// Uses in-memory counter for threshold check (O(1)), falls back to DB COUNT for actual count.
    fn enforce_actor_context_limit(
        &mut self,
        actor: &str,
        context: &str,
        limit: usize,
    ) -> Result<Option<EnforcementEvent>, SqliteError> {
        let threshold = limit + limit / 2;

        // O(1) threshold check from in-memory counter (skip if not initialized — e.g. FlushEnforcement)
        if self.enforcement_counters.initialized {
            let cached_count = self
                .enforcement_counters
                .actor_context
                .get(&(actor.to_string(), context.to_string()))
                .copied()
                .unwrap_or(0);

            if cached_count <= threshold {
                return Ok(None);
            }
        }

        // Get exact count from DB
        let count: i64 = self
            .conn
            .query_row(
                "SELECT COUNT(*) FROM attestation_actors a
                 JOIN attestation_contexts c ON a.attestation_id = c.attestation_id
                 WHERE a.actor = ?1 AND c.context = ?2",
                rusqlite::params![actor, context],
                |row| row.get::<_, i64>(0),
            )
            .unwrap_or(0);

        // Re-check with exact count (counter might be slightly off)
        if (count as usize) <= threshold {
            // Correct the in-memory counter
            self.enforcement_counters
                .actor_context
                .insert((actor.to_string(), context.to_string()), count as usize);
            return Ok(None);
        }

        // +1 accounts for the distill attestation inserted after deletion
        let delete_count = count as usize - limit + 1;

        // Load full attestation data for the eviction batch
        let eviction_batch = self.load_eviction_batch(
            "SELECT att.id, att.subjects, att.predicates, att.contexts, att.actors, att.timestamp, att.source, att.attributes, att.created_at, att.signature, att.signer_did
             FROM attestations att
             JOIN attestation_actors a ON att.id = a.attestation_id
             JOIN attestation_contexts c ON att.id = c.attestation_id
             WHERE a.actor = ?1 AND c.context = ?2
             ORDER BY att.timestamp ASC
             LIMIT ?3",
            rusqlite::params![actor, context, delete_count],
        )?;

        // Build eviction details for the event log
        let details = {
            let oldest = eviction_batch.first().map(|a| {
                chrono::DateTime::from_timestamp_millis(a.timestamp)
                    .unwrap_or_default()
                    .to_rfc3339()
            });
            let mut pred_set = HashSet::new();
            for att in &eviction_batch {
                for p in &att.predicates {
                    pred_set.insert(p.clone());
                }
            }
            let mut predicates: Vec<String> = pred_set.into_iter().collect();
            predicates.sort();
            predicates.truncate(15);
            EvictionDetails {
                evicted_actors: Vec::new(),
                evicted_contexts: Vec::new(),
                predicates,
                last_seen: oldest,
            }
        };

        // Delete oldest attestations first (CASCADE deletes junction rows)
        let _deleted = self.conn.execute(
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

        // Σ Insert sigma after deleting originals.
        if !eviction_batch.is_empty() {
            let sigma = distill::build_distill_attestation(&eviction_batch, context);
            self.distilling = true;
            let insert_result = put_attestation(&self.conn, &sigma);
            self.distilling = false;
            if let Err(e) = insert_result {
                eprintln!(
                    "qntx-sqlite: Σ failed to insert sigma for actor={} context={}: {}",
                    actor, context, e
                );
            }
        }

        // Update in-memory counter: deleted delete_count, inserted 1 distill = net -(delete_count-1)
        self.enforcement_counters
            .decrement_actor_context(actor, context, delete_count.saturating_sub(1));

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
    /// Uses half-bound threshold: triggers at `limit + limit/2`, evicts down to `limit`.
    /// Uses in-memory counter for threshold check (O(1)), falls back to DB query for eviction.
    fn enforce_actor_contexts_limit(
        &mut self,
        actor: &str,
        limit: usize,
    ) -> Result<Option<EnforcementEvent>, SqliteError> {
        let threshold = limit + limit / 2;

        // O(1) threshold check (skip if not initialized — e.g. FlushEnforcement)
        if self.enforcement_counters.initialized {
            let cached_count = self
                .enforcement_counters
                .actor_contexts
                .get(actor)
                .map(|s| s.len())
                .unwrap_or(0);

            if cached_count <= threshold {
                return Ok(None);
            }
        }

        // Counter says threshold exceeded — get exact data from DB for eviction
        struct ContextUsage {
            context: String,
            _usage_count: i64,
        }

        let contexts: Vec<ContextUsage> = {
            let mut stmt = self.conn.prepare(
                "SELECT c.context, COUNT(*) as cnt
                 FROM attestation_contexts c
                 JOIN attestation_actors a ON c.attestation_id = a.attestation_id
                 WHERE a.actor = ?1
                 GROUP BY c.context
                 ORDER BY cnt ASC",
            )?;
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

        // Re-check with exact count
        if contexts.len() <= threshold {
            return Ok(None);
        }

        let contexts_to_delete = &contexts[..contexts.len() - limit];
        let mut total_deleted = 0usize;

        let ctx_list: Vec<&str> = contexts_to_delete
            .iter()
            .map(|cu| cu.context.as_str())
            .collect();
        let placeholders: Vec<String> =
            (0..ctx_list.len()).map(|i| format!("?{}", i + 2)).collect();

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
            let param_refs: Vec<&dyn rusqlite::types::ToSql> =
                params.iter().map(|p| p.as_ref()).collect();
            details.last_seen = stmt
                .query_row(param_refs.as_slice(), |row| row.get(0))
                .ok()
                .flatten();
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
            let param_refs: Vec<&dyn rusqlite::types::ToSql> =
                params.iter().map(|p| p.as_ref()).collect();
            let mut rows = stmt.query(param_refs.as_slice())?;
            details.predicates = collect_distinct_predicates(&mut rows, 15)?;
        }

        for cu in contexts_to_delete.iter() {
            details.evicted_contexts.push(cu.context.clone());

            // Load full attestation data for distillation
            let eviction_batch = self.load_eviction_batch(
                "SELECT att.id, att.subjects, att.predicates, att.contexts, att.actors, att.timestamp, att.source, att.attributes, att.created_at, att.signature, att.signer_did
                 FROM attestations att
                 JOIN attestation_actors a ON att.id = a.attestation_id
                 JOIN attestation_contexts c ON att.id = c.attestation_id
                 WHERE a.actor = ?1 AND c.context = ?2",
                rusqlite::params![actor, cu.context],
            )?;

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

            // Σ Insert sigma after deleting originals
            if !eviction_batch.is_empty() {
                let sigma = distill::build_distill_attestation(&eviction_batch, &cu.context);
                self.distilling = true;
                let insert_result = put_attestation(&self.conn, &sigma);
                self.distilling = false;
                if let Err(e) = insert_result {
                    eprintln!(
                        "qntx-sqlite: Σ failed to insert sigma for actor={} context={}: {}",
                        actor, cu.context, e
                    );
                }
            }

            // Update in-memory counters: remove evicted context
            self.enforcement_counters
                .actor_context
                .remove(&(actor.to_string(), cu.context.clone()));
            if let Some(ctx_set) = self.enforcement_counters.actor_contexts.get_mut(actor) {
                ctx_set.remove(&cu.context);
            }
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
    /// Uses half-bound threshold: triggers at `limit + limit/2`, evicts down to `limit`.
    /// Uses in-memory counter for threshold check (O(1)), falls back to DB query for eviction.
    fn enforce_entity_actors_limit(
        &mut self,
        entity: &str,
        limit: usize,
    ) -> Result<Option<EnforcementEvent>, SqliteError> {
        let threshold = limit + limit / 2;

        // O(1) threshold check (skip if not initialized — e.g. FlushEnforcement)
        if self.enforcement_counters.initialized {
            let cached_count = self
                .enforcement_counters
                .entity_actors
                .get(entity)
                .map(|s| s.len())
                .unwrap_or(0);

            if cached_count <= threshold {
                return Ok(None);
            }
        }

        // Counter says threshold exceeded — get exact data from DB for eviction
        let actors: Vec<String> = {
            let mut stmt = self.conn.prepare(
                "SELECT a.actor, MAX(att.timestamp) as last_seen
                 FROM attestation_actors a
                 JOIN attestation_subjects s ON a.attestation_id = s.attestation_id
                 JOIN attestations att ON att.id = a.attestation_id
                 WHERE s.subject = ?1
                 GROUP BY a.actor
                 ORDER BY last_seen ASC",
            )?;
            let mut rows = stmt.query(rusqlite::params![entity])?;
            let mut result = Vec::new();
            while let Some(row) = rows.next()? {
                result.push(row.get::<_, String>(0)?);
            }
            result
        };

        // Re-check with exact count
        if actors.len() <= threshold {
            return Ok(None);
        }

        let actors_to_delete = &actors[..actors.len() - limit];
        let mut total_deleted = 0usize;

        let actor_list: Vec<&str> = actors_to_delete.iter().map(|a| a.as_str()).collect();
        let placeholders: Vec<String> = (0..actor_list.len())
            .map(|i| format!("?{}", i + 2))
            .collect();

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
            let param_refs: Vec<&dyn rusqlite::types::ToSql> =
                params.iter().map(|p| p.as_ref()).collect();
            details.last_seen = stmt
                .query_row(param_refs.as_slice(), |row| row.get(0))
                .ok()
                .flatten();
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
            let param_refs: Vec<&dyn rusqlite::types::ToSql> =
                params.iter().map(|p| p.as_ref()).collect();
            let mut rows = stmt.query(param_refs.as_slice())?;
            details.predicates = collect_distinct_predicates(&mut rows, 15)?;
        }

        for actor in actors_to_delete.iter() {
            details.evicted_actors.push(actor.clone());

            // Load full attestation data for distillation
            let eviction_batch = self.load_eviction_batch(
                "SELECT att.id, att.subjects, att.predicates, att.contexts, att.actors, att.timestamp, att.source, att.attributes, att.created_at, att.signature, att.signer_did
                 FROM attestations att
                 JOIN attestation_actors a ON att.id = a.attestation_id
                 JOIN attestation_subjects s ON att.id = s.attestation_id
                 WHERE a.actor = ?1 AND s.subject = ?2",
                rusqlite::params![actor, entity],
            )?;

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

            // Σ Insert sigma after deleting originals
            if !eviction_batch.is_empty() {
                let sigma = distill::build_distill_attestation(&eviction_batch, "_distill");
                self.distilling = true;
                let insert_result = put_attestation(&self.conn, &sigma);
                self.distilling = false;
                if let Err(e) = insert_result {
                    eprintln!(
                        "qntx-sqlite: Σ failed to insert sigma for entity={} actor={}: {}",
                        entity, actor, e
                    );
                }
            }

            // Update in-memory counters: remove evicted actor from entity
            if let Some(actor_set) = self.enforcement_counters.entity_actors.get_mut(entity) {
                actor_set.remove(actor);
            }
            // Remove actor_context entries for this actor (all contexts)
            self.enforcement_counters
                .actor_context
                .retain(|(a, _), _| a != actor);
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

    /// Load full attestation data for rows about to be evicted.
    fn load_eviction_batch(
        &self,
        sql: &str,
        params: impl rusqlite::Params,
    ) -> Result<Vec<Attestation>, SqliteError> {
        let mut stmt = self.conn.prepare(sql)?;
        let rows = stmt.query_map(params, |row| {
            Ok((
                row.get::<_, String>(0)?,
                row.get::<_, String>(1)?,
                row.get::<_, String>(2)?,
                row.get::<_, String>(3)?,
                row.get::<_, String>(4)?,
                row.get::<_, String>(5)?,
                row.get::<_, String>(6)?,
                row.get::<_, Option<String>>(7)?,
                row.get::<_, String>(8)?,
                row.get::<_, Option<Vec<u8>>>(9)?,
                row.get::<_, Option<String>>(10)?,
            ))
        })?;

        let mut attestations = Vec::new();
        for row_result in rows {
            let row_data = row_result?;
            match crate::SqliteStore::row_to_attestation(row_data) {
                Ok(att) => attestations.push(att),
                Err(e) => {
                    eprintln!("qntx-sqlite: failed to deserialize eviction row: {}", e);
                }
            }
        }
        Ok(attestations)
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

        // Prune storage_events to 15k rows every ~100 inserts
        let count: i64 = self
            .conn
            .query_row("SELECT COUNT(*) FROM storage_events", [], |row| row.get(0))
            .unwrap_or(0);
        if count > 15100 {
            self.conn.execute(
                "DELETE FROM storage_events WHERE id NOT IN (SELECT id FROM storage_events ORDER BY id DESC LIMIT 15000)",
                [],
            )?;
        }

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
            config: EnforcementConfig::default(), // limit=32
        };

        let events = store.enforce_limits(&input).unwrap();
        assert!(events.is_empty());
    }

    #[test]
    fn test_actor_context_limit_deletes_oldest() {
        let mut store = make_store();

        // Half-bound threshold for limit=2 is 2 + 2/2 = 3.
        // Insert 4 attestations to exceed threshold, expect eviction down to limit=2.
        insert_attestation(&mut store, "AS-1", "bob", "work", "ALICE", "knows");
        insert_attestation(&mut store, "AS-2", "bob", "work", "ALICE", "likes");
        insert_attestation(&mut store, "AS-3", "bob", "work", "ALICE", "trusts");
        insert_attestation(&mut store, "AS-4", "bob", "work", "ALICE", "helps");

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
        assert_eq!(events[0].deleted_count, 3); // 4 - 2 + 1 = 3 (+1 for distill)
        assert_eq!(events[0].limit_value, 2);

        // Verify: 1 original kept + 1 distill attestation = 2 = limit
        let count = store.count().unwrap();
        assert_eq!(count, 2);

        // Verify the distill attestation exists
        let all_atts: Vec<String> = store.ids().unwrap();
        let distill_count = all_atts
            .iter()
            .filter(|id| id.starts_with("AS-distill-"))
            .count();
        assert_eq!(distill_count, 1);
    }

    #[test]
    fn test_entity_actors_limit() {
        let mut store = make_store();

        // Half-bound threshold for limit=2 is 2 + 2/2 = 3.
        // Need 4 actors to exceed threshold.
        insert_attestation(&mut store, "AS-1", "actor1", "ctx", "ENTITY", "pred");
        insert_attestation(&mut store, "AS-2", "actor2", "ctx", "ENTITY", "pred");
        insert_attestation(&mut store, "AS-3", "actor3", "ctx", "ENTITY", "pred");
        insert_attestation(&mut store, "AS-4", "actor4", "ctx", "ENTITY", "pred");

        let input = EnforcementInput {
            actors: vec!["actor4".to_string()],
            contexts: vec!["ctx".to_string()],
            subjects: vec!["ENTITY".to_string()],
            config: EnforcementConfig {
                actor_context_limit: 16,
                actor_contexts_limit: 64,
                entity_actors_limit: 2,
            },
        };

        let events = store.enforce_limits(&input).unwrap();
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

        // Half-bound threshold for limit=2 is 3. Need 4 to trigger.
        insert_attestation(&mut store, "AS-1", "bob", "work", "ALICE", "knows");
        insert_attestation(&mut store, "AS-2", "bob", "work", "ALICE", "likes");
        insert_attestation(&mut store, "AS-3", "bob", "work", "ALICE", "trusts");
        insert_attestation(&mut store, "AS-4", "bob", "work", "ALICE", "helps");

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

    #[test]
    fn test_distill_attestation_created_on_eviction() {
        let mut store = make_store();

        // Insert 4 attestations with attributes, limit=2, threshold=3
        let att1 = AttestationBuilder::new()
            .id("AS-1")
            .actor("bot")
            .context("crawl")
            .subject("X")
            .predicate("crawl-stage-changed")
            .timestamp(1000)
            .attribute("elapsed_ms".to_string(), serde_json::json!(100))
            .attribute("stage".to_string(), serde_json::json!("connecting"))
            .build();
        store.put(att1).unwrap();

        let att2 = AttestationBuilder::new()
            .id("AS-2")
            .actor("bot")
            .context("crawl")
            .subject("X")
            .predicate("crawl-stage-changed")
            .timestamp(2000)
            .attribute("elapsed_ms".to_string(), serde_json::json!(200))
            .attribute("stage".to_string(), serde_json::json!("discovered"))
            .build();
        store.put(att2).unwrap();

        let att3 = AttestationBuilder::new()
            .id("AS-3")
            .actor("bot")
            .context("crawl")
            .subject("X")
            .predicate("announced")
            .timestamp(3000)
            .attribute("elapsed_ms".to_string(), serde_json::json!(50))
            .attribute("stage".to_string(), serde_json::json!("connecting"))
            .build();
        store.put(att3).unwrap();

        let att4 = AttestationBuilder::new()
            .id("AS-4")
            .actor("bot")
            .context("crawl")
            .subject("X")
            .predicate("crawl-stage-changed")
            .timestamp(4000)
            .attribute("elapsed_ms".to_string(), serde_json::json!(150))
            .attribute("stage".to_string(), serde_json::json!("discovered"))
            .build();
        store.put(att4).unwrap();

        // Enforce with limit=2 (threshold=3, count=4 exceeds)
        let input = EnforcementInput {
            actors: vec!["bot".to_string()],
            contexts: vec!["crawl".to_string()],
            subjects: vec!["X".to_string()],
            config: EnforcementConfig {
                actor_context_limit: 2,
                actor_contexts_limit: 64,
                entity_actors_limit: 64,
            },
        };

        let events = store.enforce_limits(&input).unwrap();
        assert_eq!(events.len(), 1);
        assert_eq!(events[0].deleted_count, 3); // 4 - 2 + 1 = 3 (+1 for distill)

        // Find the distill attestation
        let all_ids = store.ids().unwrap();
        let distill_ids: Vec<&String> = all_ids
            .iter()
            .filter(|id| id.starts_with("AS-distill-"))
            .collect();
        assert_eq!(
            distill_ids.len(),
            1,
            "expected exactly one distill attestation"
        );

        let distill_att = store.get(distill_ids[0]).unwrap().unwrap();
        assert_eq!(distill_att.source, "distill");
        assert_eq!(distill_att.actors, vec!["distill"]);
        assert_eq!(distill_att.contexts, vec!["crawl"]);

        // Check _distill metadata
        assert_eq!(
            distill_att.attributes.get("_distill").unwrap(),
            &serde_json::json!(true)
        );
        assert_eq!(
            distill_att.attributes.get("_count").unwrap(),
            &serde_json::json!(3) // batch size: 3 evicted attestations (AS-1, AS-2, AS-3)
        );
        assert_eq!(
            distill_att.attributes.get("_total").unwrap(),
            &serde_json::json!(3) // no prior distills, so _total == _count
        );

        // Check merged elapsed_ms: min=50, max=200, sum=350, count=3 (AS-1 + AS-2 + AS-3)
        let elapsed = distill_att.attributes.get("elapsed_ms").unwrap();
        assert_eq!(elapsed.get("min").unwrap(), &serde_json::json!(50));  // AS-3
        assert_eq!(elapsed.get("max").unwrap(), &serde_json::json!(200)); // AS-2
        assert_eq!(elapsed.get("sum").unwrap(), &serde_json::json!(350)); // 100+200+50
        assert_eq!(elapsed.get("count").unwrap(), &serde_json::json!(3));

        // Check merged stage: AS-1, AS-2, AS-3 have stage values
        let stage = distill_att.attributes.get("stage").unwrap();
        let freqs = stage.get("frequencies").unwrap().as_object().unwrap();
        assert!(freqs.contains_key("connecting"));
        assert!(freqs.contains_key("discovered"));

        // Predicates should include the evicted predicates
        assert!(distill_att
            .predicates
            .iter()
            .any(|p| p == "crawl-stage-changed"));
    }

    #[test]
    fn test_half_bound_no_trigger_at_threshold() {
        let mut store = make_store();

        // Insert exactly threshold (limit=2, threshold=3) — should NOT trigger
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
        assert!(events.is_empty(), "should not trigger at exactly threshold");
        assert_eq!(store.count().unwrap(), 3);
    }
}
