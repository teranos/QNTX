//! SQLite storage backend implementing AttestationStore trait

use qntx_core::{
    attestation::{Attestation, AxFilter, AxResult, AxSummary},
    storage::{AttestationStore, QueryStore, StorageStats, StoreError},
};
use rusqlite::{Connection, OptionalExtension};
use std::collections::HashMap;

use crate::error::SqliteError;
use crate::json::{
    deserialize_attributes, deserialize_string_vec, serialize_attributes, serialize_string_vec,
    sql_to_timestamp, timestamp_to_sql,
};

type StoreResult<T> = Result<T, StoreError>;

/// SQLite-backed attestation store
pub struct SqliteStore {
    conn: Connection,
}

impl SqliteStore {
    /// Create a new SQLite store from a connection
    ///
    /// The connection should already have migrations applied.
    /// Use [`crate::migrate::migrate`] to initialize a fresh database.
    pub fn new(conn: Connection) -> Self {
        Self { conn }
    }

    /// Create a new in-memory SQLite store (for testing)
    pub fn in_memory() -> crate::error::Result<Self> {
        // Initialize sqlite-vec extension BEFORE creating connection
        crate::vec::init_vec_extension();

        let conn = Connection::open_in_memory()?;
        crate::migrate::migrate(&conn)?;
        Ok(Self::new(conn))
    }

    /// Create a new file-backed SQLite store
    pub fn open(path: impl AsRef<std::path::Path>) -> crate::error::Result<Self> {
        // Initialize sqlite-vec extension BEFORE creating connection
        crate::vec::init_vec_extension();

        let conn = Connection::open(path)?;

        conn.execute("PRAGMA foreign_keys = ON", [])?;
        crate::migrate::migrate(&conn)?;
        Ok(Self::new(conn))
    }

    /// Get a reference to the underlying connection
    pub fn connection(&self) -> &Connection {
        &self.conn
    }

    /// Helper to extract a row into an Attestation, converting errors through SqliteError.
    fn row_to_attestation(
        row_data: (
            String,
            String,
            String,
            String,
            String,
            String,
            String,
            Option<String>,
            String,
        ),
    ) -> StoreResult<Attestation> {
        let (
            id,
            subjects_json,
            predicates_json,
            contexts_json,
            actors_json,
            timestamp_str,
            source,
            attributes_json,
            created_at_str,
        ) = row_data;

        let subjects = deserialize_string_vec(&subjects_json)?;
        let predicates = deserialize_string_vec(&predicates_json)?;
        let contexts = deserialize_string_vec(&contexts_json)?;
        let actors = deserialize_string_vec(&actors_json)?;
        let attributes = deserialize_attributes(attributes_json)?;
        let timestamp = sql_to_timestamp(&timestamp_str)?;
        let created_at = sql_to_timestamp(&created_at_str)?;

        Ok(Attestation {
            id,
            subjects,
            predicates,
            contexts,
            actors,
            timestamp,
            source,
            attributes,
            created_at,
        })
    }

    /// Helper to query rows from a prepared statement.
    fn query_distinct_values(&self, sql: &str) -> StoreResult<Vec<String>> {
        let mut stmt = self.conn.prepare(sql).map_err(SqliteError::from)?;

        let values = stmt
            .query_map([], |row| row.get::<_, String>(0))
            .map_err(SqliteError::from)?
            .collect::<Result<Vec<String>, rusqlite::Error>>()
            .map_err(SqliteError::from)?;

        Ok(values)
    }
}

impl AttestationStore for SqliteStore {
    fn put(&mut self, attestation: Attestation) -> StoreResult<()> {
        if self.exists(&attestation.id)? {
            return Err(StoreError::AlreadyExists(attestation.id.clone()));
        }

        let subjects_json = serialize_string_vec(&attestation.subjects)?;
        let predicates_json = serialize_string_vec(&attestation.predicates)?;
        let contexts_json = serialize_string_vec(&attestation.contexts)?;
        let actors_json = serialize_string_vec(&attestation.actors)?;
        let attributes_json = serialize_attributes(&attestation.attributes)?;

        let timestamp_sql = timestamp_to_sql(attestation.timestamp);
        let created_at_sql = timestamp_to_sql(attestation.created_at);

        self.conn
            .execute(
                "INSERT INTO attestations (id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at)
                 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
                rusqlite::params![
                    attestation.id,
                    subjects_json,
                    predicates_json,
                    contexts_json,
                    actors_json,
                    timestamp_sql,
                    attestation.source,
                    attributes_json,
                    created_at_sql,
                ],
            )
            .map_err(SqliteError::from)?;

        Ok(())
    }

    #[allow(clippy::type_complexity)]
    fn get(&self, id: &str) -> StoreResult<Option<Attestation>> {
        let mut stmt = self
            .conn
            .prepare(
                "SELECT id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at
                 FROM attestations
                 WHERE id = ?",
            )
            .map_err(SqliteError::from)?;

        let result: Option<(
            String,
            String,
            String,
            String,
            String,
            String,
            String,
            Option<String>,
            String,
        )> = stmt
            .query_row([id], |row| {
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
                ))
            })
            .optional()
            .map_err(SqliteError::from)?;

        match result {
            None => Ok(None),
            Some(row_data) => Self::row_to_attestation(row_data).map(Some),
        }
    }

    fn delete(&mut self, id: &str) -> StoreResult<bool> {
        let rows_affected = self
            .conn
            .execute("DELETE FROM attestations WHERE id = ?", [id])
            .map_err(SqliteError::from)?;

        Ok(rows_affected > 0)
    }

    fn update(&mut self, attestation: Attestation) -> StoreResult<()> {
        if !self.exists(&attestation.id)? {
            return Err(StoreError::NotFound(attestation.id.clone()));
        }

        let subjects_json = serialize_string_vec(&attestation.subjects)?;
        let predicates_json = serialize_string_vec(&attestation.predicates)?;
        let contexts_json = serialize_string_vec(&attestation.contexts)?;
        let actors_json = serialize_string_vec(&attestation.actors)?;
        let attributes_json = serialize_attributes(&attestation.attributes)?;

        let timestamp_sql = timestamp_to_sql(attestation.timestamp);

        self.conn
            .execute(
                "UPDATE attestations
             SET subjects = ?, predicates = ?, contexts = ?, actors = ?,
                 timestamp = ?, source = ?, attributes = ?
             WHERE id = ?",
                rusqlite::params![
                    subjects_json,
                    predicates_json,
                    contexts_json,
                    actors_json,
                    timestamp_sql,
                    attestation.source,
                    attributes_json,
                    attestation.id,
                ],
            )
            .map_err(SqliteError::from)?;

        Ok(())
    }

    fn ids(&self) -> StoreResult<Vec<String>> {
        self.query_distinct_values("SELECT id FROM attestations ORDER BY created_at DESC")
    }

    fn clear(&mut self) -> StoreResult<()> {
        self.conn
            .execute("DELETE FROM attestations", [])
            .map_err(SqliteError::from)?;

        Ok(())
    }
}

impl QueryStore for SqliteStore {
    fn query(&self, filter: &AxFilter) -> StoreResult<AxResult> {
        // Build dynamic SQL query based on filter
        let mut sql = String::from(
            "SELECT id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at \
             FROM attestations WHERE 1=1"
        );
        let mut params: Vec<String> = Vec::new();

        // Filter by subjects (JSON array contains check)
        if !filter.subjects.is_empty() {
            sql.push_str(&format!(
                " AND EXISTS (SELECT 1 FROM json_each(subjects) WHERE value IN ({}))",
                filter
                    .subjects
                    .iter()
                    .map(|_| "?")
                    .collect::<Vec<_>>()
                    .join(", ")
            ));
            params.extend(filter.subjects.iter().cloned());
        }

        // Filter by predicates
        if !filter.predicates.is_empty() {
            sql.push_str(&format!(
                " AND EXISTS (SELECT 1 FROM json_each(predicates) WHERE value IN ({}))",
                filter
                    .predicates
                    .iter()
                    .map(|_| "?")
                    .collect::<Vec<_>>()
                    .join(", ")
            ));
            params.extend(filter.predicates.iter().cloned());
        }

        // Filter by contexts
        if !filter.contexts.is_empty() {
            sql.push_str(&format!(
                " AND EXISTS (SELECT 1 FROM json_each(contexts) WHERE value IN ({}))",
                filter
                    .contexts
                    .iter()
                    .map(|_| "?")
                    .collect::<Vec<_>>()
                    .join(", ")
            ));
            params.extend(filter.contexts.iter().cloned());
        }

        // Filter by actors
        if !filter.actors.is_empty() {
            sql.push_str(&format!(
                " AND EXISTS (SELECT 1 FROM json_each(actors) WHERE value IN ({}))",
                filter
                    .actors
                    .iter()
                    .map(|_| "?")
                    .collect::<Vec<_>>()
                    .join(", ")
            ));
            params.extend(filter.actors.iter().cloned());
        }

        // Filter by time range
        if let Some(start) = filter.time_start {
            sql.push_str(" AND timestamp >= ?");
            params.push(crate::json::timestamp_to_sql(start));
        }
        if let Some(end) = filter.time_end {
            sql.push_str(" AND timestamp <= ?");
            params.push(crate::json::timestamp_to_sql(end));
        }

        // Apply ordering and limit
        sql.push_str(" ORDER BY created_at DESC");
        if let Some(limit) = filter.limit {
            sql.push_str(&format!(" LIMIT {}", limit));
        }

        // Execute query
        let mut stmt = self.conn.prepare(&sql).map_err(SqliteError::from)?;

        let param_refs: Vec<&dyn rusqlite::ToSql> =
            params.iter().map(|p| p as &dyn rusqlite::ToSql).collect();

        let rows = stmt
            .query_map(&param_refs[..], |row| {
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
                ))
            })
            .map_err(SqliteError::from)?;

        let mut attestations = Vec::new();
        for row_result in rows {
            let row_data = row_result.map_err(SqliteError::from)?;
            attestations.push(Self::row_to_attestation(row_data)?);
        }

        // Build summary
        let summary = build_summary(&attestations);

        Ok(AxResult {
            attestations,
            conflicts: Vec::new(), // TODO: implement conflict detection
            summary,
        })
    }

    fn predicates(&self) -> StoreResult<Vec<String>> {
        self.query_distinct_values(
            "SELECT DISTINCT value FROM attestations, json_each(predicates) ORDER BY value",
        )
    }

    fn contexts(&self) -> StoreResult<Vec<String>> {
        self.query_distinct_values(
            "SELECT DISTINCT value FROM attestations, json_each(contexts) ORDER BY value",
        )
    }

    fn subjects(&self) -> StoreResult<Vec<String>> {
        self.query_distinct_values(
            "SELECT DISTINCT value FROM attestations, json_each(subjects) ORDER BY value",
        )
    }

    fn actors(&self) -> StoreResult<Vec<String>> {
        self.query_distinct_values(
            "SELECT DISTINCT value FROM attestations, json_each(actors) ORDER BY value",
        )
    }

    fn stats(&self) -> StoreResult<StorageStats> {
        Ok(StorageStats {
            total_attestations: self.count()?,
            unique_subjects: self.subjects()?.len(),
            unique_predicates: self.predicates()?.len(),
            unique_contexts: self.contexts()?.len(),
            unique_actors: self.actors()?.len(),
        })
    }
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
