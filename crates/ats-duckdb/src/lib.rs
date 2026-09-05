#![cfg_attr(
    test,
    allow(
        clippy::unwrap_used,
        clippy::expect_used,
        clippy::panic,
        clippy::indexing_slicing,
        clippy::string_slice
    )
)]
//! DuckDB-backed attestation store.
//!
//! Peer of `ats_sqlite::SqliteStore`. Implements the storage traits from
//! `ats::storage`. See ADR-024 for the design.

pub mod error;
pub mod json;
pub mod migrate;
pub mod namespace;
pub mod namespace_store;
pub mod nodeidentity;
pub mod schedules;
pub mod tokens;
pub mod users;
pub mod watchers;

// FFI module for CGO integration.
#[cfg(feature = "ffi")]
pub mod ffi;

pub use error::{DuckdbError, Result};

use ats::attestation::Attestation;
use ats::storage::{AttestationStore, StoreError};
use duckdb::types::Value;
use serde::Deserialize;

// ats's storage::error module isn't public, but AttestationStore's trait
// methods return StoreResult<T>. Alias it here to match ats-sqlite's pattern
// (crates/ats-sqlite/src/store.rs).
type StoreResult<T> = std::result::Result<T, StoreError>;
use std::collections::HashMap;

/// Column tuple returned from the `attestations` table `SELECT` in
/// query paths. Mirrors the migration schema at
/// `db/duckdb/migrations/001_create_attestations_table.sql`. Aliased to
/// keep `row_to_attestation`'s signature readable and satisfy clippy's
/// `type_complexity` lint.
type AttestationRow = (
    String,          // id
    Value,           // subjects
    Value,           // predicates
    Value,           // contexts
    Value,           // actors
    i64,             // timestamp
    String,          // source
    Option<String>,  // attributes_json
    i64,             // created_at
    Option<Vec<u8>>, // signature
    Option<String>,  // signer_did
);

/// DuckDB extensions to `INSTALL` + `LOAD` for the given location URL.
/// Scheme is the trigger.
///
/// `s3://` returns `aws` + `httpfs`. `httpfs` is the network layer; `aws`
/// registers the AWS SDK credential provider chain, which reads
/// `~/.aws/credentials` including `aws_session_token`, so short-lived STS
/// creds from an SSM-managed instance / EC2 instance profile / ECS task
/// role / EKS IRSA all flow through unchanged. Without `aws`, `httpfs`
/// only looks at `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` /
/// `AWS_SESSION_TOKEN` in the process env and signs with empty creds,
/// which S3 rejects 403.
///
/// `http://` and `https://` return `httpfs` only — no cloud auth in the
/// picture. `file://` and local paths return an empty slice.
///
/// Future schemes fit the same shape: `gs://` would return `httpfs` +
/// `gcs`, `azure://` would return `httpfs` + `azure`.
fn remote_extensions(location: &str) -> &'static [&'static str] {
    if location.starts_with("s3://") {
        &["aws", "httpfs"]
    } else if location.starts_with("http://") || location.starts_with("https://") {
        &["httpfs"]
    } else {
        &[]
    }
}

/// True when a location URL lives outside the local filesystem (i.e. any
/// scheme that needs at least one DuckDB extension loaded).
pub(crate) fn is_remote(location: &str) -> bool {
    !remote_extensions(location).is_empty()
}

/// Whether the location holds nothing under `prefix`. The `*` is what makes it
/// a question: without a wildcard, glob hands the pattern back unexamined.
pub(crate) fn holds_nothing(conn: &duckdb::Connection, prefix: &str) -> Result<bool> {
    let sql = format!("SELECT count(*) FROM glob('{prefix}/*')");
    let count: i64 = conn
        .query_row(&sql, [], |row| row.get(0))
        .map_err(|e| DuckdbError::Backend(format!("failed to look at what {prefix} holds: {e}")))?;
    Ok(count == 0)
}

/// A read failed and the location holds nothing, so it answered empty. stderr
/// because nothing in the workspace installs a tracing subscriber.
pub(crate) fn took_as_empty(what: &str, e: &duckdb::Error) {
    eprintln!("ats-duckdb: {what}: the location holds nothing, so this read answered empty: {e}");
}

/// Prepare a read. `Ok(None)` is the location holding nothing to read.
///
/// Credentials are resolved again before the location is asked whether it is
/// empty, so an expired token is refreshed rather than read as an empty store.
pub(crate) fn prepare_or_empty<'a>(
    conn: &'a duckdb::Connection,
    location: &str,
    prefix: &str,
    sql: &str,
    what: &str,
) -> Result<Option<duckdb::Statement<'a>>> {
    let first = match conn.prepare(sql) {
        Ok(stmt) => return Ok(Some(stmt)),
        Err(e) => e,
    };

    if let Err(e) = resolve_credentials_again(conn, location) {
        return Err(DuckdbError::Backend(format!(
            "{what}: {first}; and the credentials could not be resolved again: {e}"
        )));
    }

    match conn.prepare(sql) {
        Ok(stmt) => Ok(Some(stmt)),
        Err(again) => {
            if !holds_nothing(conn, prefix)? {
                return Err(DuckdbError::Backend(format!(
                    "{what}: {again} (also failed before the credentials were resolved again: {first})"
                )));
            }
            took_as_empty(what, &again);
            Ok(None)
        }
    }
}

/// The SQL that makes a remote location reachable: install and load the
/// extensions its scheme needs, and for `s3://` create the secret that wires
/// the AWS credential provider chain into httpfs. `None` for a local path.
///
/// Shared by every store that touches the location, so a new scheme is handled
/// once in `remote_extensions` rather than in each of them.
pub(crate) fn remote_setup_sql(location: &str) -> Option<String> {
    let extensions = remote_extensions(location);
    if extensions.is_empty() {
        return None;
    }
    let mut sql: String = extensions
        .iter()
        .map(|e| format!("INSTALL {e}; LOAD {e};"))
        .collect();
    if location.starts_with("s3://") {
        sql.push_str("CREATE OR REPLACE SECRET qntx_s3 (TYPE s3, PROVIDER credential_chain);");
    }
    Some(sql)
}

/// Re-resolve the credential provider chain into the secret.
///
/// The chain is read once, when the connection is opened. On a host whose
/// identity is an STS role that returns a token with a fixed expiry, and the
/// connection outlives it — every request after that instant is signed with a
/// dead token and S3 answers ExpiredToken, which arrives as an HTTP 400.
///
/// So a remote call that failed is worth one more attempt with the current
/// credentials before it is reported as the location being unreachable.
pub(crate) fn resolve_credentials_again(
    conn: &duckdb::Connection,
    location: &str,
) -> std::result::Result<(), duckdb::Error> {
    match remote_setup_sql(location) {
        Some(sql) => conn.execute_batch(&sql),
        None => Ok(()),
    }
}

/// Read a location, resolving the credentials again and trying once more if it
/// fails. `what` names the read, so a failure says which one it was.
///
/// The whole read and not the prepare. A store here keeps one connection for
/// the life of the process, and an expired token is answered by the object
/// store — which is reached when the rows are pulled, not when the statement
/// is built. Guarding only the prepare left the retry somewhere it never fired.
pub(crate) fn rows_fresh<T>(
    conn: &duckdb::Connection,
    location: &str,
    sql: &str,
    what: &str,
    row: impl Fn(&duckdb::Row<'_>) -> std::result::Result<T, duckdb::Error> + Copy,
) -> Result<Vec<T>> {
    let first = match read_rows(conn, sql, row) {
        Ok(rows) => return Ok(rows),
        Err(e) => e,
    };
    if let Err(e) = resolve_credentials_again(conn, location) {
        return Err(DuckdbError::Backend(format!(
            "{what}: {first}; and the credentials could not be resolved again: {e}"
        )));
    }
    read_rows(conn, sql, row).map_err(|e| {
        DuckdbError::Backend(format!(
            "{what}: {e} (also failed before the credentials were resolved again: {first})"
        ))
    })
}

/// One attempt: build it, run it, and pull every row.
fn read_rows<T>(
    conn: &duckdb::Connection,
    sql: &str,
    row: impl Fn(&duckdb::Row<'_>) -> std::result::Result<T, duckdb::Error>,
) -> std::result::Result<Vec<T>, duckdb::Error> {
    let mut stmt = conn.prepare(sql)?;
    let mapped = stmt.query_map([], |r| row(r))?;
    mapped.collect()
}

/// Convert a Vec<String> to a JSON-serialized string bindable as a DuckDB
/// parameter. Paired with `CAST(? AS VARCHAR[])` in SQL to reconstitute the
/// LIST<VARCHAR> column value.
///
/// Why not Value::List: duckdb-rs 1.4.3 exposes `Value::List` on the read
/// path (queries return it) but does not support binding it as a query
/// parameter — attempting to do so raises "binding List parameters is not yet
/// supported". JSON round-trip via CAST is the current workaround.
fn str_list_json(v: &[String]) -> serde_json::Result<String> {
    serde_json::to_string(v)
}

/// Convert a DuckDB Value read back from a LIST<VARCHAR> cell into Vec<String>.
fn value_to_string_vec(v: Value) -> Result<Vec<String>> {
    match v {
        Value::List(items) | Value::Array(items) => items
            .into_iter()
            .map(|item| match item {
                Value::Text(s) => Ok(s),
                other => Err(DuckdbError::Backend(format!(
                    "expected VARCHAR in list, got {:?}",
                    other
                ))),
            })
            .collect(),
        Value::Null => Ok(Vec::new()),
        other => Err(DuckdbError::Backend(format!(
            "expected LIST<VARCHAR>, got {:?}",
            other
        ))),
    }
}

/// Filter shape accepted by `DuckdbStore::query`. Mirrors the JSON that
/// `ats/storage/sqlitecgo/storage_cgo.go:GetAttestations` sends to the SQLite
/// FFI, so the Go-side wrapper can serialize the same struct for either
/// backend. Each list field is OR-logic within, all fields are AND'd together
/// (matches `ats.AttestationFilter` semantics in `ats/store.go:69-79`).
#[derive(Debug, Default, Deserialize)]
pub struct QueryFilter {
    #[serde(default)]
    pub subjects: Vec<String>,
    #[serde(default)]
    pub predicates: Vec<String>,
    #[serde(default)]
    pub contexts: Vec<String>,
    #[serde(default)]
    pub actors: Vec<String>,
    #[serde(default)]
    pub source: String,
    #[serde(default)]
    pub time_start: Option<i64>,
    #[serde(default)]
    pub time_end: Option<i64>,
    #[serde(default)]
    pub limit: i64,
}

/// Attestation store backed by DuckDB against Parquet files at `location`.
/// The DuckDB release `libduckdb-sys` generated its bindings against.
///
/// This is not a preference. The duckdb crate at 1.4.3 was built against
/// DuckDB v1.4.3, and the process links libduckdb dynamically — so if the
/// library on the box is a different release, the bindings describe an ABI
/// that is not there. That failure is silent at compile and link time.
///
/// `flake.nix` pins libduckdb to this version through its nixpkgs revision.
/// Changing any one of the three — this constant, the crate version in
/// Cargo.toml, the flake revision — without the others is the bug this guards
/// against.
///
/// Why 1.4.3 rather than something newer: the nixpkgs revision carrying a
/// later DuckDB also carries a glibc newer than the deployment box's, and
/// libduckdb then cannot load there. See flake.nix.
const EXPECTED_DUCKDB_VERSION: &str = "v1.4.3";

/// Compare the linked library against [`EXPECTED_DUCKDB_VERSION`].
///
/// Called on every store open. A mismatch is fatal rather than a warning: a
/// warning is a thing nobody reads until they are already debugging the
/// corruption it predicted.
pub(crate) fn assert_library_version(conn: &duckdb::Connection) -> Result<()> {
    let actual: String = conn.query_row("SELECT version()", [], |row| row.get(0))?;
    if actual != EXPECTED_DUCKDB_VERSION {
        return Err(DuckdbError::Backend(format!(
            "linked libduckdb is {actual}, bindings were generated against {EXPECTED_DUCKDB_VERSION} \
             (duckdb-rs in crates/ats-duckdb/Cargo.toml, libduckdb pinned by the nixpkgs-duckdb \
             input in flake.nix) — these must match; bump them together or not at all"
        )));
    }
    Ok(())
}

pub struct DuckdbStore {
    location: String,
    /// Where this store's attestations live. Namespace is the top-level
    /// prefix, so a store reaches its own namespace and no other.
    prefix: String,
    conn: duckdb::Connection,
}

impl DuckdbStore {
    /// Open a store at the given location URL. Schema is applied through
    /// migrations at `db/duckdb/migrations/` — no DDL in application code.
    /// Loads the DuckDB extensions returned by `remote_extensions(location)`
    /// — scheme-driven, see that function's doc comment.
    ///
    /// Historical attestations are read straight from the Parquet files at
    /// query time. They are deliberately not loaded back into the buffer:
    /// the buffer is what `flush` copies out and then clears, so anything
    /// hydrated into it would be written a second time on the next flush.
    pub fn open(location: impl Into<String>, namespace: impl AsRef<str>) -> Result<Self> {
        let location = location.into();
        let prefix = namespace::prefix(&location, namespace.as_ref(), "attestations");
        let conn = duckdb::Connection::open_in_memory()?;
        assert_library_version(&conn)?;
        migrate::migrate(&conn)?;
        // For s3:// locations, the aws extension alone does not enable
        // credential resolution. Per the DuckDB 1.2 aws-extension docs
        // (https://duckdb.org/docs/1.2/extensions/aws.html), a secret with
        // PROVIDER credential_chain is required — that's what wires the
        // AWS SDK credential provider (env, ~/.aws/credentials, IAM role,
        // STS session token) into httpfs. Without it httpfs signs with empty
        // creds and S3 returns 403. See `remote_setup_sql`.
        if let Some(sql) = remote_setup_sql(&location) {
            conn.execute_batch(&sql)?;
        }
        Ok(Self {
            location,
            prefix,
            conn,
        })
    }

    /// The glob every flushed attestation lands under.
    fn parquet_glob(&self) -> String {
        format!("{}/*.parquet", self.prefix)
    }

    /// How many Parquet files the location holds. `glob` answers zero for an
    /// empty prefix instead of erroring, which is what lets a caller tell
    /// "nothing written yet" apart from "could not look".
    /// Against S3 the glob is a live ListObjectsV2, so a throttle, a timeout or
    /// an expired credential arrives here. Answering zero for those sends
    /// `query` to the buffer alone, which `flush` empties every five seconds.
    fn parquet_file_count(&self) -> Result<i64> {
        let glob = self.parquet_glob();
        let sql = format!("SELECT count(*) FROM glob('{}')", glob);
        let first = match self.conn.query_row(&sql, [], |row| row.get(0)) {
            Ok(count) => return Ok(count),
            Err(e) => e,
        };

        if let Err(e) = resolve_credentials_again(&self.conn, &self.location) {
            return Err(DuckdbError::Backend(format!(
                "failed to count the Parquet files at {glob}: {first}; \
                 and the credentials could not be resolved again: {e}"
            )));
        }

        self.conn
            .query_row(&sql, [], |row| row.get(0))
            .map_err(|e| {
                DuckdbError::Backend(format!(
                    "failed to count the Parquet files at {glob}: {e} \
                 (also failed before the credentials were resolved again: {first})"
                ))
            })
    }

    /// Flush the in-memory `attestations` table to a new Parquet file at
    /// `<location>/attestations/<millis>-<uuid>.parquet` and clear the buffer.
    /// A no-op when the buffer is empty.
    pub fn flush(&self) -> Result<()> {
        let count: i64 = self
            .conn
            .query_row("SELECT COUNT(*) FROM attestations", [], |row| row.get(0))?;
        if count == 0 {
            return Ok(());
        }
        // `CREATE OR REPLACE SECRET` invokes the AWS SDK credential
        // provider chain; the credentials used by the following `COPY`
        // are resolved at this call.
        if self.location.starts_with("s3://") {
            self.conn.execute_batch(
                "CREATE OR REPLACE SECRET qntx_s3 (TYPE s3, PROVIDER credential_chain);",
            )?;
        }
        if !is_remote(&self.location) {
            std::fs::create_dir_all(&self.prefix)?;
        }
        let ms = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map_err(|e| DuckdbError::Backend(format!("the clock is before the unix epoch: {e}")))?
            .as_millis();
        let file = format!("{}/{}-{}.parquet", self.prefix, ms, uuid::Uuid::new_v4());
        self.conn.execute_batch(&format!(
            "BEGIN TRANSACTION;
             COPY attestations TO '{}' (FORMAT PARQUET);
             DELETE FROM attestations;
             COMMIT;",
            file
        ))?;
        Ok(())
    }

    /// The location URL configured for this store.
    pub fn location(&self) -> &str {
        &self.location
    }

    /// Filter query over the in-memory attestations table.
    ///
    /// SQL shape (built dynamically from the filter):
    ///   SELECT ... FROM attestations
    ///   [WHERE cond1 AND cond2 AND ...]
    ///   ORDER BY timestamp DESC
    ///   [LIMIT N]
    ///
    /// Each list filter (subjects, predicates, contexts, actors) becomes
    /// `list_has_any(<col>, CAST(? AS VARCHAR[]))` with the parameter bound as
    /// a JSON-serialized string — same shape as the write path, forced by
    /// duckdb-rs 1.4.3 not supporting `Value::List` as a bind parameter
    /// (see `str_list_json` doc comment).
    ///
    /// Semantics match `ats.AttestationFilter` (Go, `ats/store.go:69-79`):
    /// OR within a list field, AND between fields.
    pub fn query(&self, filter: &QueryFilter) -> Result<Vec<Attestation>> {
        // Read the buffer and the Parquet files together. `flush` clears the
        // buffer after copying it out, so the buffer alone answers only for
        // writes since the last flush — every attestation older than five
        // seconds would be invisible, which is every attestation.
        const COLUMNS: &str = "id, subjects, predicates, contexts, actors, timestamp, \
                               source, attributes, created_at, signature, signer_did";

        let source = if self.parquet_file_count()? > 0 {
            format!(
                "(SELECT {c} FROM attestations UNION ALL SELECT {c} FROM read_parquet('{g}'))",
                c = COLUMNS,
                g = self.parquet_glob()
            )
        } else {
            "attestations".to_string()
        };

        let mut sql = format!("SELECT {} FROM {}", COLUMNS, source);
        let mut conds: Vec<&'static str> = Vec::new();
        let mut binds: Vec<Value> = Vec::new();

        if !filter.subjects.is_empty() {
            conds.push("list_has_any(subjects, CAST(? AS VARCHAR[]))");
            binds.push(Value::Text(str_list_json(&filter.subjects)?));
        }
        if !filter.predicates.is_empty() {
            conds.push("list_has_any(predicates, CAST(? AS VARCHAR[]))");
            binds.push(Value::Text(str_list_json(&filter.predicates)?));
        }
        if !filter.contexts.is_empty() {
            conds.push("list_has_any(contexts, CAST(? AS VARCHAR[]))");
            binds.push(Value::Text(str_list_json(&filter.contexts)?));
        }
        if !filter.actors.is_empty() {
            conds.push("list_has_any(actors, CAST(? AS VARCHAR[]))");
            binds.push(Value::Text(str_list_json(&filter.actors)?));
        }
        if !filter.source.is_empty() {
            conds.push("source = ?");
            binds.push(Value::Text(filter.source.clone()));
        }
        if let Some(ts) = filter.time_start {
            conds.push("timestamp >= ?");
            binds.push(Value::BigInt(ts));
        }
        if let Some(te) = filter.time_end {
            conds.push("timestamp <= ?");
            binds.push(Value::BigInt(te));
        }

        if !conds.is_empty() {
            sql.push_str(" WHERE ");
            sql.push_str(&conds.join(" AND "));
        }
        sql.push_str(" ORDER BY timestamp DESC");
        if filter.limit > 0 {
            // limit is a validated integer — inline safely.
            sql.push_str(&format!(" LIMIT {}", filter.limit));
        }

        let mut stmt = self.conn.prepare(&sql)?;
        let rows = stmt.query_map(duckdb::params_from_iter(binds.iter()), |row| {
            Ok((
                row.get::<_, String>(0)?,
                row.get::<_, Value>(1)?,
                row.get::<_, Value>(2)?,
                row.get::<_, Value>(3)?,
                row.get::<_, Value>(4)?,
                row.get::<_, i64>(5)?,
                row.get::<_, String>(6)?,
                row.get::<_, Option<String>>(7)?,
                row.get::<_, i64>(8)?,
                row.get::<_, Option<Vec<u8>>>(9)?,
                row.get::<_, Option<String>>(10)?,
            ))
        })?;

        let mut out = Vec::new();
        for r in rows {
            let tuple = r?;
            out.push(Self::row_to_attestation(tuple)?);
        }
        Ok(out)
    }

    /// Many ids in one statement. `get` pays a ListObjectsV2 to count files and
    /// a `read_parquet` plan every call, so resolving a page of fires one id at
    /// a time is that cost once per fire.
    pub fn get_many(&self, ids: &[String]) -> Result<Vec<Attestation>> {
        if ids.is_empty() {
            return Ok(Vec::new());
        }
        const COLUMNS: &str = "id, subjects, predicates, contexts, actors, timestamp, \
                               source, attributes, created_at, signature, signer_did";

        let source = if self.parquet_file_count()? > 0 {
            format!(
                "(SELECT {c} FROM attestations UNION ALL SELECT {c} FROM read_parquet('{g}'))",
                c = COLUMNS,
                g = self.parquet_glob()
            )
        } else {
            "attestations".to_string()
        };

        // One placeholder per id, so ids stay bound rather than inlined.
        let placeholders = vec!["?"; ids.len()].join(", ");
        let sql = format!("SELECT {COLUMNS} FROM {source} WHERE id IN ({placeholders})");

        let mut stmt = self.conn.prepare(&sql)?;
        let rows = stmt.query_map(duckdb::params_from_iter(ids.iter()), |row| {
            Ok((
                row.get::<_, String>(0)?,
                row.get::<_, Value>(1)?,
                row.get::<_, Value>(2)?,
                row.get::<_, Value>(3)?,
                row.get::<_, Value>(4)?,
                row.get::<_, i64>(5)?,
                row.get::<_, String>(6)?,
                row.get::<_, Option<String>>(7)?,
                row.get::<_, i64>(8)?,
                row.get::<_, Option<Vec<u8>>>(9)?,
                row.get::<_, Option<String>>(10)?,
            ))
        })?;

        let mut out = Vec::new();
        for r in rows {
            let tuple = r?;
            out.push(Self::row_to_attestation(tuple)?);
        }
        Ok(out)
    }

    fn row_to_attestation(row: AttestationRow) -> Result<Attestation> {
        let (
            id,
            subjects_v,
            predicates_v,
            contexts_v,
            actors_v,
            timestamp,
            source,
            attributes_json,
            created_at,
            signature,
            signer_did,
        ) = row;

        let attributes: HashMap<String, serde_json::Value> = match attributes_json {
            Some(s) if !s.is_empty() && s != "null" => serde_json::from_str(&s)?,
            _ => HashMap::new(),
        };

        Ok(Attestation {
            id,
            subjects: value_to_string_vec(subjects_v)?,
            predicates: value_to_string_vec(predicates_v)?,
            contexts: value_to_string_vec(contexts_v)?,
            actors: value_to_string_vec(actors_v)?,
            timestamp,
            source,
            attributes,
            created_at,
            signature,
            signer_did,
        })
    }
}

impl AttestationStore for DuckdbStore {
    fn put(&mut self, attestation: Attestation) -> StoreResult<()> {
        if self.exists(&attestation.id)? {
            return Err(StoreError::AlreadyExists(attestation.id.clone()));
        }

        let attributes_json = if attestation.attributes.is_empty() {
            None
        } else {
            Some(
                serde_json::to_string(&attestation.attributes)
                    .map_err(|e| StoreError::Backend(format!("{}", e)))?,
            )
        };

        self.conn
            .execute(
                "INSERT INTO attestations
                 (id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at, signature, signer_did)
                 VALUES (
                     ?,
                     CAST(? AS VARCHAR[]),
                     CAST(? AS VARCHAR[]),
                     CAST(? AS VARCHAR[]),
                     CAST(? AS VARCHAR[]),
                     ?, ?, ?, ?, ?, ?
                 )",
                duckdb::params![
                    attestation.id,
                    str_list_json(&attestation.subjects)
                        .map_err(|e| StoreError::Backend(format!("{}", e)))?,
                    str_list_json(&attestation.predicates)
                        .map_err(|e| StoreError::Backend(format!("{}", e)))?,
                    str_list_json(&attestation.contexts)
                        .map_err(|e| StoreError::Backend(format!("{}", e)))?,
                    str_list_json(&attestation.actors)
                        .map_err(|e| StoreError::Backend(format!("{}", e)))?,
                    attestation.timestamp,
                    attestation.source,
                    attributes_json,
                    attestation.created_at,
                    attestation.signature,
                    attestation.signer_did,
                ],
            )
            .map_err(|e| StoreError::Backend(format!("{}", e)))?;
        Ok(())
    }

    fn get(&self, id: &str) -> StoreResult<Option<Attestation>> {
        // Buffer and files, for the reason query gives: a flushed attestation
        // is not in the buffer, and "not in the buffer" is not "does not exist".
        const COLUMNS: &str = "id, subjects, predicates, contexts, actors, timestamp, \
                               source, attributes, created_at, signature, signer_did";
        let files = self
            .parquet_file_count()
            .map_err(|e| StoreError::Backend(format!("{}", e)))?;
        let sql = if files > 0 {
            format!(
                "SELECT {c} FROM (SELECT {c} FROM attestations \
                 UNION ALL SELECT {c} FROM read_parquet('{g}')) WHERE id = ? LIMIT 1",
                c = COLUMNS,
                g = self.parquet_glob()
            )
        } else {
            format!("SELECT {} FROM attestations WHERE id = ?", COLUMNS)
        };

        let mut stmt = self
            .conn
            .prepare(&sql)
            .map_err(|e| StoreError::Backend(format!("{}", e)))?;

        let row = stmt.query_row([id], |row| {
            Ok((
                row.get::<_, String>(0)?,
                row.get::<_, Value>(1)?,
                row.get::<_, Value>(2)?,
                row.get::<_, Value>(3)?,
                row.get::<_, Value>(4)?,
                row.get::<_, i64>(5)?,
                row.get::<_, String>(6)?,
                row.get::<_, Option<String>>(7)?,
                row.get::<_, i64>(8)?,
                row.get::<_, Option<Vec<u8>>>(9)?,
                row.get::<_, Option<String>>(10)?,
            ))
        });

        match row {
            Ok(r) => Ok(Some(
                Self::row_to_attestation(r).map_err(|e| StoreError::Backend(format!("{}", e)))?,
            )),
            Err(duckdb::Error::QueryReturnedNoRows) => Ok(None),
            Err(e) => Err(StoreError::Backend(format!("{}", e))),
        }
    }

    fn delete(&mut self, id: &str) -> StoreResult<bool> {
        let rows = self
            .conn
            .execute("DELETE FROM attestations WHERE id = ?", [id])
            .map_err(|e| StoreError::Backend(format!("{}", e)))?;
        Ok(rows > 0)
    }

    fn update(&mut self, attestation: Attestation) -> StoreResult<()> {
        if !self.exists(&attestation.id)? {
            return Err(StoreError::NotFound(attestation.id.clone()));
        }

        let attributes_json = if attestation.attributes.is_empty() {
            None
        } else {
            Some(
                serde_json::to_string(&attestation.attributes)
                    .map_err(|e| StoreError::Backend(format!("{}", e)))?,
            )
        };

        self.conn
            .execute(
                "UPDATE attestations SET
                    subjects   = CAST(? AS VARCHAR[]),
                    predicates = CAST(? AS VARCHAR[]),
                    contexts   = CAST(? AS VARCHAR[]),
                    actors     = CAST(? AS VARCHAR[]),
                    timestamp  = ?,
                    source     = ?,
                    attributes = ?,
                    signature  = ?,
                    signer_did = ?
                 WHERE id = ?",
                duckdb::params![
                    str_list_json(&attestation.subjects)
                        .map_err(|e| StoreError::Backend(format!("{}", e)))?,
                    str_list_json(&attestation.predicates)
                        .map_err(|e| StoreError::Backend(format!("{}", e)))?,
                    str_list_json(&attestation.contexts)
                        .map_err(|e| StoreError::Backend(format!("{}", e)))?,
                    str_list_json(&attestation.actors)
                        .map_err(|e| StoreError::Backend(format!("{}", e)))?,
                    attestation.timestamp,
                    attestation.source,
                    attributes_json,
                    attestation.signature,
                    attestation.signer_did,
                    attestation.id,
                ],
            )
            .map_err(|e| StoreError::Backend(format!("{}", e)))?;
        Ok(())
    }

    /// Count buffer and files. The default trait implementation takes the
    /// length of `ids`, which reads the buffer alone, and flush empties the
    /// buffer every few seconds.
    fn count(&self) -> StoreResult<usize> {
        let files = self
            .parquet_file_count()
            .map_err(|e| StoreError::Backend(format!("{}", e)))?;
        // flush copies the buffer into a file and empties it in one
        // transaction, so no row is in both and UNION ALL does not double.
        let sql = if files > 0 {
            format!(
                "SELECT count(*) FROM (SELECT id FROM attestations \
                 UNION ALL SELECT id FROM read_parquet('{g}'))",
                g = self.parquet_glob()
            )
        } else {
            "SELECT count(*) FROM attestations".to_string()
        };
        let total: i64 = self
            .conn
            .query_row(&sql, [], |row| row.get(0))
            .map_err(|e| StoreError::Backend(format!("{}", e)))?;
        Ok(total as usize)
    }

    fn ids(&self) -> StoreResult<Vec<String>> {
        let mut stmt = self
            .conn
            .prepare("SELECT id FROM attestations ORDER BY created_at DESC")
            .map_err(|e| StoreError::Backend(format!("{}", e)))?;

        let rows = stmt
            .query_map([], |row| row.get::<_, String>(0))
            .map_err(|e| StoreError::Backend(format!("{}", e)))?;

        let mut ids = Vec::new();
        for row in rows {
            ids.push(row.map_err(|e| StoreError::Backend(format!("{}", e)))?);
        }
        Ok(ids)
    }

    fn clear(&mut self) -> StoreResult<()> {
        self.conn
            .execute("DELETE FROM attestations", [])
            .map_err(|e| StoreError::Backend(format!("{}", e)))?;
        Ok(())
    }
}

impl Drop for DuckdbStore {
    fn drop(&mut self) {
        // Best-effort final flush so buffered attestations reach durable
        // storage on shutdown. Errors are logged, not surfaced — Drop can't
        // return them, and refusing to drop would leak the connection.
        if let Err(e) = self.flush() {
            eprintln!("ats-duckdb: final flush failed: {}", e);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use ats::attestation::Attestation;
    use std::collections::HashMap;

    fn sample_attestation(id: &str) -> Attestation {
        Attestation {
            id: id.to_string(),
            subjects: vec!["ALICE".to_string()],
            predicates: vec!["knows".to_string()],
            contexts: vec!["work".to_string()],
            actors: vec!["human:bob".to_string()],
            timestamp: 1_700_000_000_000,
            source: "test".to_string(),
            attributes: HashMap::new(),
            created_at: 1_700_000_000_000,
            signature: None,
            signer_did: None,
        }
    }

    /// A location of this test's own. These all shared one path under /tmp,
    /// so the first run left AS-1 behind and every run after it failed
    /// AlreadyExists — including the ones that never asked for a duplicate.
    fn at(dir: &tempfile::TempDir) -> String {
        format!("file://{}", dir.path().display())
    }

    fn store(dir: &tempfile::TempDir) -> DuckdbStore {
        DuckdbStore::open(at(dir), namespace::DEFAULT).unwrap()
    }

    #[test]
    fn open_creates_schema() {
        let dir = tempfile::tempdir().unwrap();
        assert_eq!(store(&dir).location(), at(&dir));
    }

    #[test]
    fn put_and_get_round_trip() {
        let dir = tempfile::tempdir().unwrap();
        let mut store = store(&dir);
        let a = sample_attestation("AS-1");
        store.put(a.clone()).unwrap();

        let got = store.get("AS-1").unwrap().unwrap();
        assert_eq!(got.id, "AS-1");
        assert_eq!(got.subjects, vec!["ALICE"]);
        assert_eq!(got.predicates, vec!["knows"]);
        assert_eq!(got.actors, vec!["human:bob"]);
    }

    #[test]
    fn get_missing_returns_none() {
        let dir = tempfile::tempdir().unwrap();
        assert!(store(&dir).get("AS-missing").unwrap().is_none());
    }

    /// Flushed attestations are still held. Counting the buffer alone reports
    /// whatever arrived since the last flush, which is seconds of history.
    #[test]
    fn count_spans_buffer_and_files() {
        let dir = tempfile::tempdir().unwrap();
        let mut store = store(&dir);
        store.put(sample_attestation("AS-1")).unwrap();
        store.put(sample_attestation("AS-2")).unwrap();
        store.flush().unwrap();
        store.put(sample_attestation("AS-3")).unwrap();

        assert_eq!(store.count().unwrap(), 3);
    }

    #[test]
    fn count_after_flush_with_empty_buffer() {
        let dir = tempfile::tempdir().unwrap();
        let mut store = store(&dir);
        store.put(sample_attestation("AS-1")).unwrap();
        store.flush().unwrap();

        assert_eq!(store.count().unwrap(), 1);
    }

    #[test]
    fn put_duplicate_rejects() {
        let dir = tempfile::tempdir().unwrap();
        let mut store = store(&dir);
        let a = sample_attestation("AS-1");
        store.put(a.clone()).unwrap();
        match store.put(a) {
            Err(StoreError::AlreadyExists(_)) => {}
            other => panic!("expected AlreadyExists, got {:?}", other),
        }
    }

    #[test]
    fn delete_removes() {
        let dir = tempfile::tempdir().unwrap();
        let mut store = store(&dir);
        store.put(sample_attestation("AS-1")).unwrap();
        assert!(store.delete("AS-1").unwrap());
        assert!(store.get("AS-1").unwrap().is_none());
    }

    #[test]
    fn ids_lists_stored() {
        let dir = tempfile::tempdir().unwrap();
        let mut store = store(&dir);
        store.put(sample_attestation("AS-1")).unwrap();
        store.put(sample_attestation("AS-2")).unwrap();
        let ids = store.ids().unwrap();
        assert_eq!(ids.len(), 2);
    }

    #[test]
    fn clear_wipes() {
        let dir = tempfile::tempdir().unwrap();
        let mut store = store(&dir);
        store.put(sample_attestation("AS-1")).unwrap();
        store.clear().unwrap();
        assert_eq!(store.count().unwrap(), 0);
    }
}
