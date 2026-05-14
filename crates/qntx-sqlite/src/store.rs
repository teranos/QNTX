//! SQLite storage backend implementing AttestationStore trait

use qntx_core::{
    attestation::{Attestation, AxFilter, AxResult, AxSummary},
    storage::{AttestationStore, QueryStore, StorageStats, StoreError},
};
use rusqlite::{backup, Connection, OptionalExtension};
use std::collections::{HashMap, HashSet};

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
    /// When set, enforcement runs every 50 puts using junction table COUNT queries.
    pub(crate) enforcement_config: Option<EnforcementConfig>,
    /// When true, put() skips enforcement to prevent infinite loops during distillation.
    pub(crate) distilling: bool,
    /// Counter for amortized enforcement: only run enforcement every N puts.
    put_count: u64,
    /// In-memory enforcement counters for O(1) threshold checks.
    /// Populated lazily from DB on first access, then maintained on put/delete.
    pub(crate) enforcement_counters: EnforcementCounters,
}

/// In-memory counters for O(1) enforcement threshold checks.
/// Avoids expensive COUNT queries with JOINs on every put().
#[derive(Default)]
pub struct EnforcementCounters {
    /// (actor, context) → attestation count
    pub(crate) actor_context: HashMap<(String, String), usize>,
    /// actor → set of distinct contexts
    pub(crate) actor_contexts: HashMap<String, HashSet<String>>,
    /// subject → set of distinct actors
    pub(crate) entity_actors: HashMap<String, HashSet<String>>,
    /// Whether counters have been populated from DB
    pub(crate) initialized: bool,
}

impl EnforcementCounters {
    /// Check if any counter exceeds its half-bound threshold.
    /// O(1) — just checks the in-memory maps for the attestation's dimensions.
    pub fn any_threshold_exceeded(
        &self,
        config: &qntx_core::storage::enforcement::EnforcementConfig,
    ) -> bool {
        let ac_threshold = config.actor_context_limit + config.actor_context_limit / 2;
        for count in self.actor_context.values() {
            if *count > ac_threshold {
                return true;
            }
        }

        let acs_threshold = config.actor_contexts_limit + config.actor_contexts_limit / 2;
        for contexts in self.actor_contexts.values() {
            if contexts.len() > acs_threshold {
                return true;
            }
        }

        let ea_threshold = config.entity_actors_limit + config.entity_actors_limit / 2;
        for actors in self.entity_actors.values() {
            if actors.len() > ea_threshold {
                return true;
            }
        }

        false
    }

    /// Decrement actor_context counter after enforcement deletes.
    pub fn decrement_actor_context(&mut self, actor: &str, context: &str, count: usize) {
        if let Some(val) = self.actor_context.get_mut(&(actor.to_string(), context.to_string())) {
            *val = val.saturating_sub(count);
        }
    }
}

/// Read-only SQLite connection for concurrent queries.
///
/// Separate from `SqliteStore` so the Rust borrow checker (and FFI)
/// can access them independently without creating overlapping references.
#[allow(dead_code)]
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
            distilling: false,
            put_count: 0,
            enforcement_counters: EnforcementCounters::default(),
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

        // Initialize flight recorder next to the database file
        let recorder_path = format!("{}.flight", path_str);
        crate::flight_recorder::init(&recorder_path);
        let conn = Connection::open(&path)?;

        conn.pragma_update(None, "journal_mode", "WAL")?;
        conn.pragma_update(None, "foreign_keys", "ON")?;
        conn.pragma_update(None, "busy_timeout", "5000")?;
        conn.pragma_update(None, "mmap_size", "0")?;
        // WAL always memory-maps the -shm index file for reader/writer coordination.
        // With many connections sharing the same mmap, auto-checkpoints during writes
        // can invalidate the mapping → SIGBUS at _platform_memmove with page-aligned
        // fault addresses. Disable auto-checkpoint; put() runs PASSIVE checkpoints
        // every 5000 writes instead (PASSIVE never truncates WAL or -shm).
        conn.pragma_update(None, "wal_autocheckpoint", "0")?;
        crate::migrate::migrate(&conn)?;

        Ok(Self {
            conn,
            db_path: Some(path_str),
            enforcement_config: None,
            distilling: false,
            put_count: 0,
            enforcement_counters: EnforcementCounters::default(),
        })
    }

    /// Open a separate read-only connection for concurrent queries.
    /// Only works for file-backed stores.
    pub fn open_read_conn(&self) -> crate::error::Result<ReadConn> {
        let path = self.db_path.as_deref().ok_or_else(|| {
            crate::error::SqliteError::Migration(
                "read connection requires a file-backed database".into(),
            )
        })?;
        let conn = Connection::open_with_flags(
            path,
            rusqlite::OpenFlags::SQLITE_OPEN_READ_ONLY | rusqlite::OpenFlags::SQLITE_OPEN_NO_MUTEX,
        )?;
        conn.pragma_update(None, "busy_timeout", "5000")?;
        conn.pragma_update(None, "mmap_size", "0")?;
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
    /// Flushes WAL to the main DB first via TRUNCATE checkpoint, then opens a
    /// separate read-only source connection — callers do NOT need to hold the mutex.
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

/// Insert an attestation through any Connection (shared by SqliteStore and WriteConn).
/// Handles the main INSERT, junction tables, and enforcement counter updates.
pub(crate) fn put_attestation(conn: &Connection, attestation: &Attestation) -> StoreResult<()> {
    let subjects_json = serialize_string_vec(&attestation.subjects)?;
    let predicates_json = serialize_string_vec(&attestation.predicates)?;
    let contexts_json = serialize_string_vec(&attestation.contexts)?;
    let actors_json = serialize_string_vec(&attestation.actors)?;
    let attributes_json = serialize_attributes(&attestation.attributes)?;

    let timestamp_sql = timestamp_to_sql(attestation.timestamp);
    let created_at_sql = timestamp_to_sql(attestation.created_at);

    crate::flight_recorder::record_fmt("put:insert_main", &attestation.id);
    conn.execute(
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
    crate::flight_recorder::record_fmt("put:junction_actors", &attestation.id);
    for actor in &attestation.actors {
        conn.execute(
            "INSERT INTO attestation_actors (attestation_id, actor) VALUES (?, ?)",
            rusqlite::params![attestation.id, actor],
        )
        .map_err(SqliteError::from)?;
    }
    crate::flight_recorder::record_fmt("put:junction_contexts", &attestation.id);
    for context in &attestation.contexts {
        conn.execute(
            "INSERT INTO attestation_contexts (attestation_id, context) VALUES (?, ?)",
            rusqlite::params![attestation.id, context],
        )
        .map_err(SqliteError::from)?;
    }
    crate::flight_recorder::record_fmt("put:junction_subjects", &attestation.id);
    for subject in &attestation.subjects {
        conn.execute(
            "INSERT INTO attestation_subjects (attestation_id, subject) VALUES (?, ?)",
            rusqlite::params![attestation.id, subject],
        )
        .map_err(SqliteError::from)?;
    }
    crate::flight_recorder::record_fmt("put:junction_predicates", &attestation.id);
    for predicate in &attestation.predicates {
        conn.execute(
            "INSERT INTO attestation_predicates (attestation_id, predicate) VALUES (?, ?)",
            rusqlite::params![attestation.id, predicate],
        )
        .map_err(SqliteError::from)?;
    }

    crate::flight_recorder::record_fmt("put:done", &attestation.id);
    Ok(())
}

impl AttestationStore for SqliteStore {
    fn put(&mut self, attestation: Attestation) -> StoreResult<()> {
        if self.exists(&attestation.id)? {
            return Err(StoreError::AlreadyExists(attestation.id.clone()));
        }

        let actors = attestation.actors.clone();
        let contexts = attestation.contexts.clone();
        let subjects = attestation.subjects.clone();

        put_attestation(&self.conn, &attestation)?;

        // Update in-memory counters and check thresholds.
        // Skip enforcement when distilling to prevent infinite loops (distill insert → enforce → distill).
        if !self.distilling {
            self.enforcement_counters.initialized = true;

            // Update counters for every put
            for actor in &actors {
                for context in &contexts {
                    *self
                        .enforcement_counters
                        .actor_context
                        .entry((actor.clone(), context.clone()))
                        .or_insert(0) += 1;
                }
                self.enforcement_counters
                    .actor_contexts
                    .entry(actor.clone())
                    .or_default()
                    .extend(contexts.iter().cloned());
            }
            for subject in &subjects {
                self.enforcement_counters
                    .entity_actors
                    .entry(subject.clone())
                    .or_default()
                    .extend(actors.iter().cloned());
            }

            // Only run enforcement when a threshold is exceeded (O(1) check)
            if let Some(ref config) = self.enforcement_config {
                let needs_enforcement = self.enforcement_counters.any_threshold_exceeded(config);
                if needs_enforcement {
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
            }
        }

        // Periodic PASSIVE checkpoint — moves WAL pages to the main DB without
        // truncating WAL or -shm. Safe with concurrent readers (PASSIVE skips
        // pages that readers hold). Every 5000 puts keeps WAL bounded at ~20MB.
        self.put_count += 1;
        if self.put_count.is_multiple_of(5000) {
            let _ = self.conn.execute_batch("PRAGMA wal_checkpoint(PASSIVE)");
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
    // DISTINCT is only needed when JOINs are present (multi-value junction
    // tables can produce duplicate attestation rows). Without JOINs,
    // attestations.id is already unique and DISTINCT forces a full-table
    // dedup scan that blocks LIMIT short-circuit (876K rows → ~14 min).
    let has_joins = !filter.subjects.is_empty()
        || !filter.predicates.is_empty()
        || !filter.contexts.is_empty()
        || !filter.actors.is_empty();
    let distinct = if has_joins { "DISTINCT " } else { "" };
    let mut sql = format!(
        "SELECT {}att.id, att.subjects, att.predicates, att.contexts, att.actors, att.timestamp, att.source, att.attributes, att.created_at, att.signature, att.signer_did \
         FROM attestations att",
        distinct
    );
    let mut joins = Vec::new();
    let mut conditions = Vec::new();
    let mut params: Vec<String> = Vec::new();

    if !filter.subjects.is_empty() {
        joins.push("JOIN attestation_subjects js ON att.id = js.attestation_id");
        conditions.push(format!(
            "js.subject IN ({})",
            filter
                .subjects
                .iter()
                .map(|_| "?")
                .collect::<Vec<_>>()
                .join(", ")
        ));
        params.extend(filter.subjects.iter().cloned());
    }
    if !filter.predicates.is_empty() {
        joins.push("JOIN attestation_predicates jp ON att.id = jp.attestation_id");
        conditions.push(format!(
            "jp.predicate IN ({})",
            filter
                .predicates
                .iter()
                .map(|_| "?")
                .collect::<Vec<_>>()
                .join(", ")
        ));
        params.extend(filter.predicates.iter().cloned());
    }
    if !filter.contexts.is_empty() {
        joins.push("JOIN attestation_contexts jc ON att.id = jc.attestation_id");
        conditions.push(format!(
            "jc.context IN ({})",
            filter
                .contexts
                .iter()
                .map(|_| "?")
                .collect::<Vec<_>>()
                .join(", ")
        ));
        params.extend(filter.contexts.iter().cloned());
    }
    if !filter.actors.is_empty() {
        joins.push("JOIN attestation_actors ja ON att.id = ja.attestation_id");
        conditions.push(format!(
            "ja.actor IN ({})",
            filter
                .actors
                .iter()
                .map(|_| "?")
                .collect::<Vec<_>>()
                .join(", ")
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
