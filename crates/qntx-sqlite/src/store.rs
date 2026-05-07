//! SQLite storage backend implementing AttestationStore trait

use qntx_core::{
    attestation::{Attestation, AxFilter, AxResult, AxSummary},
    storage::{AttestationStore, QueryStore, StorageStats, StoreError},
};
use rusqlite::{backup, Connection, OptionalExtension};
use std::collections::HashMap;

use crate::error::SqliteError;

/// Raw row tuple from the attestations table, before conversion to Attestation.
type AttestationRow = (
    String,
    String,
    String,
    String,
    String,
    String,
    String,
    Option<String>,
    String,
    Option<Vec<u8>>,
    Option<String>,
);
use crate::json::{
    deserialize_attributes, deserialize_string_vec, serialize_attributes, serialize_string_vec,
    sql_to_timestamp, timestamp_to_sql,
};

use qntx_core::storage::enforcement::{EnforcementConfig, EnforcementInput};

type StoreResult<T> = Result<T, StoreError>;

/// SQLite-backed attestation store (write connection).
///
/// File-backed stores also create a separate `ReadConn` for queries.
/// SQLite WAL mode allows one writer and multiple concurrent readers,
/// so reads never block writes. Each connection is single-threaded
/// (rusqlite uses RefCell), but Go serializes access per-connection
/// with separate mutexes.
pub struct SqliteStore {
    pub(crate) conn: Connection,
    /// Database file path, if file-backed. Used by backup to open a separate read connection.
    pub(crate) db_path: Option<String>,
    /// Enforcement config for bounded storage limits (16/64/64 default).
    /// When set, enforcement runs automatically after every put().
    pub(crate) enforcement_config: Option<EnforcementConfig>,
}

/// Read-only SQLite connection for concurrent queries.
///
/// Separate from `SqliteStore` so the Rust borrow checker (and FFI)
/// can access them independently without creating overlapping references.
pub struct ReadConn {
    pub(crate) conn: Connection,
}

impl SqliteStore {
    /// Create a new SQLite store from a connection
    ///
    /// The connection should already have migrations applied.
    /// Use [`crate::migrate::migrate`] to initialize a fresh database.
    pub fn new(conn: Connection) -> Self {
        Self {
            conn,
            db_path: None,
            enforcement_config: None,
        }
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
    ///
    /// Opens a read-write connection. Use [`open_read_conn`] to create a
    /// separate read-only connection for concurrent queries.
    pub fn open(path: impl AsRef<std::path::Path>) -> crate::error::Result<Self> {
        // Initialize sqlite-vec extension BEFORE creating connection
        crate::vec::init_vec_extension();

        let path_str = path.as_ref().to_string_lossy().to_string();
        let conn = Connection::open(&path)?;

        conn.pragma_update(None, "journal_mode", "WAL")?;
        conn.pragma_update(None, "foreign_keys", "ON")?;
        conn.pragma_update(None, "busy_timeout", "5000")?;
        crate::migrate::migrate(&conn)?;

        Ok(Self {
            conn,
            db_path: Some(path_str),
            enforcement_config: None,
        })
    }

    /// Open a separate read-only connection for concurrent queries.
    /// Only works for file-backed stores.
    pub fn open_read_conn(&self) -> crate::error::Result<ReadConn> {
        let path = self
            .db_path
            .as_deref()
            .ok_or_else(|| crate::error::SqliteError::Migration(
                "read connection requires a file-backed database".into(),
            ))?;
        let conn = Connection::open_with_flags(
            path,
            rusqlite::OpenFlags::SQLITE_OPEN_READ_ONLY | rusqlite::OpenFlags::SQLITE_OPEN_NO_MUTEX,
        )?;
        conn.pragma_update(None, "busy_timeout", "5000")?;
        Ok(ReadConn { conn })
    }

    /// Run PRAGMA integrity_check and return the result lines.
    /// A healthy database returns a single line: "ok".
    pub fn integrity_check(&self) -> StoreResult<Vec<String>> {
        let mut stmt = self
            .conn
            .prepare("PRAGMA integrity_check")
            .map_err(SqliteError::from)?;
        let rows = stmt
            .query_map([], |row| row.get::<_, String>(0))
            .map_err(SqliteError::from)?;
        let mut results = Vec::new();
        for row in rows {
            results.push(row.map_err(SqliteError::from)?);
        }
        Ok(results)
    }

    /// Create a hot backup of the database to the given path.
    /// Opens a separate read-only source connection so the backup does not need
    /// exclusive access to `self.conn` — callers do NOT need to hold the mutex.
    pub fn backup(&self, dest_path: &str) -> StoreResult<()> {
        let src_path = self
            .db_path
            .as_deref()
            .ok_or_else(|| StoreError::Backend("backup requires a file-backed database".into()))?;
        let src = Connection::open_with_flags(
            src_path,
            rusqlite::OpenFlags::SQLITE_OPEN_READ_ONLY | rusqlite::OpenFlags::SQLITE_OPEN_NO_MUTEX,
        )
        .map_err(SqliteError::from)?;
        let mut dest = Connection::open(dest_path).map_err(SqliteError::from)?;
        let b = backup::Backup::new(&src, &mut dest).map_err(SqliteError::from)?;

        loop {
            match b.step(5_000).map_err(SqliteError::from)? {
                backup::StepResult::Done => break,
                backup::StepResult::More => {
                    // Yield briefly so writers aren't starved
                    std::thread::yield_now();
                }
                backup::StepResult::Busy | backup::StepResult::Locked | _ => {
                    // Short backoff — long waits let writers invalidate more pages
                    std::thread::sleep(std::time::Duration::from_millis(5));
                }
            }
        }
        Ok(())
    }

    /// Set enforcement config. When set, enforcement runs after every put().
    pub fn set_enforcement_config(&mut self, config: EnforcementConfig) {
        self.enforcement_config = Some(config);
    }

    /// Get a reference to the underlying write connection
    pub fn connection(&self) -> &Connection {
        &self.conn
    }

    /// Extract a row tuple into an Attestation, converting errors through SqliteError.
    pub fn row_to_attestation(row_data: AttestationRow) -> StoreResult<Attestation> {
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
            signature,
            signer_did,
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
            signature,
            signer_did,
        })
    }

    /// Execute a raw SQL query with parameters, returning attestation rows as JSON.
    ///
    /// The query MUST select the standard attestation columns in order:
    ///   id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at, signature, signer_did
    ///
    /// Parameters are passed as a JSON array of values (strings, numbers, nulls).
    /// This allows Go to keep its query builder while Rust owns the connection.
    pub fn query_attestations_raw(
        &self,
        sql: &str,
        params_json: &str,
    ) -> StoreResult<Vec<Attestation>> {
        let params: Vec<serde_json::Value> = if params_json.is_empty() || params_json == "[]" {
            Vec::new()
        } else {
            serde_json::from_str(params_json)
                .map_err(|e| StoreError::Backend(format!("invalid params JSON: {}", e)))?
        };

        let mut stmt = self.conn.prepare(sql).map_err(SqliteError::from)?;

        // Convert JSON values to rusqlite params
        let param_refs: Vec<Box<dyn rusqlite::types::ToSql>> = params
            .iter()
            .map(|v| -> Box<dyn rusqlite::types::ToSql> {
                match v {
                    serde_json::Value::String(s) => Box::new(s.clone()),
                    serde_json::Value::Number(n) => {
                        if let Some(i) = n.as_i64() {
                            Box::new(i)
                        } else if let Some(f) = n.as_f64() {
                            Box::new(f)
                        } else {
                            Box::new(n.to_string())
                        }
                    }
                    serde_json::Value::Bool(b) => Box::new(*b),
                    serde_json::Value::Null => Box::new(rusqlite::types::Null),
                    _ => Box::new(v.to_string()),
                }
            })
            .collect();

        let param_slice: Vec<&dyn rusqlite::types::ToSql> =
            param_refs.iter().map(|p| p.as_ref()).collect();

        let rows = stmt
            .query_map(param_slice.as_slice(), |row| {
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
            })
            .map_err(SqliteError::from)?;

        let mut attestations = Vec::new();
        for row_result in rows {
            let row_data = row_result.map_err(SqliteError::from)?;
            attestations.push(Self::row_to_attestation(row_data)?);
        }

        Ok(attestations)
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

        let actors = attestation.actors.clone();
        let contexts = attestation.contexts.clone();
        let subjects = attestation.subjects.clone();

        self.conn
            .execute(
                "INSERT INTO attestations (id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at, signature, signer_did)
                 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
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
                    attestation.signature,
                    attestation.signer_did,
                ],
            )
            .map_err(SqliteError::from)?;

        // Populate junction tables for indexed lookups
        for actor in &actors {
            self.conn
                .execute(
                    "INSERT INTO attestation_actors (attestation_id, actor) VALUES (?, ?)",
                    rusqlite::params![attestation.id, actor],
                )
                .map_err(SqliteError::from)?;
        }
        for context in &contexts {
            self.conn
                .execute(
                    "INSERT INTO attestation_contexts (attestation_id, context) VALUES (?, ?)",
                    rusqlite::params![attestation.id, context],
                )
                .map_err(SqliteError::from)?;
        }
        for subject in &subjects {
            self.conn
                .execute(
                    "INSERT INTO attestation_subjects (attestation_id, subject) VALUES (?, ?)",
                    rusqlite::params![attestation.id, subject],
                )
                .map_err(SqliteError::from)?;
        }
        for predicate in &attestation.predicates {
            self.conn
                .execute(
                    "INSERT INTO attestation_predicates (attestation_id, predicate) VALUES (?, ?)",
                    rusqlite::params![attestation.id, predicate],
                )
                .map_err(SqliteError::from)?;
        }

        // Enforce bounded storage limits after every insert
        if let Some(ref config) = self.enforcement_config {
            let input = EnforcementInput {
                actors,
                contexts,
                subjects,
                config: config.clone(),
            };
            if let Err(e) = self.enforce_limits(&input) {
                eprintln!("qntx-sqlite: post-put enforcement failed: {}", e);
            }
        }

        Ok(())
    }

    #[allow(clippy::type_complexity)]
    fn get(&self, id: &str) -> StoreResult<Option<Attestation>> {
        let mut stmt = self
            .conn
            .prepare(
                "SELECT id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at, signature, signer_did
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
            Option<Vec<u8>>,
            Option<String>,
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
                    row.get::<_, Option<Vec<u8>>>(9)?,
                    row.get::<_, Option<String>>(10)?,
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
                 timestamp = ?, source = ?, attributes = ?, signature = ?, signer_did = ?
             WHERE id = ?",
                rusqlite::params![
                    subjects_json,
                    predicates_json,
                    contexts_json,
                    actors_json,
                    timestamp_sql,
                    attestation.source,
                    attributes_json,
                    attestation.signature,
                    attestation.signer_did,
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

/// Build SQL and params for an AxFilter query. Used by both SqliteStore and ReadConn.
pub fn build_query_sql(filter: &AxFilter) -> (String, Vec<String>) {
    let mut sql = String::from(
        "SELECT DISTINCT att.id, att.subjects, att.predicates, att.contexts, att.actors, att.timestamp, att.source, att.attributes, att.created_at, att.signature, att.signer_did \
         FROM attestations att"
    );
    let mut joins = Vec::new();
    let mut conditions = Vec::new();
    let mut params: Vec<String> = Vec::new();

    if !filter.subjects.is_empty() {
        joins.push("JOIN attestation_subjects js ON att.id = js.attestation_id");
        conditions.push(format!(
            "js.subject IN ({})",
            filter.subjects.iter().map(|_| "?").collect::<Vec<_>>().join(", ")
        ));
        params.extend(filter.subjects.iter().cloned());
    }
    if !filter.predicates.is_empty() {
        joins.push("JOIN attestation_predicates jp ON att.id = jp.attestation_id");
        conditions.push(format!(
            "jp.predicate IN ({})",
            filter.predicates.iter().map(|_| "?").collect::<Vec<_>>().join(", ")
        ));
        params.extend(filter.predicates.iter().cloned());
    }
    if !filter.contexts.is_empty() {
        joins.push("JOIN attestation_contexts jc ON att.id = jc.attestation_id");
        conditions.push(format!(
            "jc.context IN ({})",
            filter.contexts.iter().map(|_| "?").collect::<Vec<_>>().join(", ")
        ));
        params.extend(filter.contexts.iter().cloned());
    }
    if !filter.actors.is_empty() {
        joins.push("JOIN attestation_actors ja ON att.id = ja.attestation_id");
        conditions.push(format!(
            "ja.actor IN ({})",
            filter.actors.iter().map(|_| "?").collect::<Vec<_>>().join(", ")
        ));
        params.extend(filter.actors.iter().cloned());
    }
    if let Some(ref source) = filter.source {
        conditions.push("att.source = ?".to_string());
        params.push(source.clone());
    }
    if let Some(start) = filter.time_start {
        conditions.push("att.timestamp >= ?".to_string());
        params.push(crate::json::timestamp_to_sql(start));
    }
    if let Some(end) = filter.time_end {
        conditions.push("att.timestamp <= ?".to_string());
        params.push(crate::json::timestamp_to_sql(end));
    }

    for join in &joins {
        sql.push(' ');
        sql.push_str(join);
    }
    if !conditions.is_empty() {
        sql.push_str(" WHERE ");
        sql.push_str(&conditions.join(" AND "));
    }
    sql.push_str(" ORDER BY att.created_at DESC, att.rowid DESC");
    if let Some(limit) = filter.limit {
        sql.push_str(&format!(" LIMIT {}", limit));
    }

    (sql, params)
}

impl QueryStore for SqliteStore {
    fn query(&self, filter: &AxFilter) -> StoreResult<AxResult> {
        let (sql, params) = build_query_sql(filter);

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
                    row.get::<_, Option<Vec<u8>>>(9)?,
                    row.get::<_, Option<String>>(10)?,
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
            "SELECT DISTINCT predicate FROM attestation_predicates ORDER BY predicate",
        )
    }

    fn contexts(&self) -> StoreResult<Vec<String>> {
        self.query_distinct_values(
            "SELECT DISTINCT context FROM attestation_contexts ORDER BY context",
        )
    }

    fn subjects(&self) -> StoreResult<Vec<String>> {
        self.query_distinct_values(
            "SELECT DISTINCT subject FROM attestation_subjects ORDER BY subject",
        )
    }

    fn actors(&self) -> StoreResult<Vec<String>> {
        self.query_distinct_values("SELECT DISTINCT actor FROM attestation_actors ORDER BY actor")
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
