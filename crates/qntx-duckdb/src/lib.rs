//! DuckDB-backed attestation store.
//!
//! Peer of `qntx_sqlite::SqliteStore`. Implements the storage traits from
//! `qntx_core::storage`. See ADR-024 for the design.

pub mod error;
pub mod json;
pub mod migrate;

// FFI module for CGO integration.
#[cfg(feature = "ffi")]
pub mod ffi;

pub use error::{DuckdbError, Result};

use duckdb::types::Value;
use qntx_core::attestation::Attestation;
use qntx_core::storage::{AttestationStore, StoreError};
use serde::Deserialize;

// qntx-core's storage::error module isn't public, but AttestationStore's trait
// methods return StoreResult<T>. Alias it here to match qntx-sqlite's pattern
// (crates/qntx-sqlite/src/store.rs).
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
fn is_remote(location: &str) -> bool {
    !remote_extensions(location).is_empty()
}

/// Convert a Vec<String> to a JSON-serialized string bindable as a DuckDB
/// parameter. Paired with `CAST(? AS VARCHAR[])` in SQL to reconstitute the
/// LIST<VARCHAR> column value.
///
/// Why not Value::List: duckdb-rs v1.10504.0 exposes `Value::List` on the read
/// path (queries return it) but does not support binding it as a query
/// parameter — attempting to do so raises "binding List parameters is not yet
/// supported". JSON round-trip via CAST is the current workaround.
fn str_list_json(v: &[String]) -> String {
    serde_json::to_string(v).unwrap_or_else(|_| "[]".to_string())
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
pub struct DuckdbStore {
    location: String,
    conn: duckdb::Connection,
}

impl DuckdbStore {
    /// Open a store at the given location URL. Schema is applied through
    /// migrations at `db/duckdb/migrations/` — no DDL in application code.
    /// Loads the DuckDB extensions returned by `remote_extensions(location)`
    /// — scheme-driven, see that function's doc comment.
    ///
    /// If Parquet files already exist under `<location>/attestations/`,
    /// they are loaded back into the in-memory buffer table so historical
    /// attestations remain queryable across process restarts.
    pub fn open(location: impl Into<String>) -> Result<Self> {
        let location = location.into();
        let conn = duckdb::Connection::open_in_memory()?;
        migrate::migrate(&conn)?;
        let exts = remote_extensions(&location);
        if !exts.is_empty() {
            let mut sql: String = exts
                .iter()
                .map(|e| format!("INSTALL {e}; LOAD {e};"))
                .collect();
            // For s3:// locations, the aws extension alone does not enable
            // credential resolution. Per the DuckDB 1.2 aws-extension docs
            // (https://duckdb.org/docs/1.2/extensions/aws.html), a secret with
            // PROVIDER credential_chain is required — that's what wires the
            // AWS SDK credential provider (env, ~/.aws/credentials, IAM role,
            // STS session token) into httpfs. Without this line httpfs signs
            // with empty creds and S3 returns 403.
            if location.starts_with("s3://") {
                sql.push_str(
                    "CREATE OR REPLACE SECRET qntx_s3 (TYPE s3, PROVIDER credential_chain);",
                );
            }
            conn.execute_batch(&sql)?;
        }
        let store = Self { location, conn };
        store.load_existing_parquet()?;
        Ok(store)
    }

    /// Load any pre-existing Parquet files under `<location>/attestations/`
    /// into the in-memory `attestations` table. Silently no-ops when the
    /// prefix is empty (typical first-boot case).
    fn load_existing_parquet(&self) -> Result<()> {
        let glob = format!("{}/attestations/*.parquet", self.location_path());
        // read_parquet errors when zero files match; treat that as empty state.
        let sql = format!(
            "INSERT INTO attestations SELECT * FROM read_parquet('{}')",
            glob
        );
        let _ = self.conn.execute_batch(&sql);
        Ok(())
    }

    /// Flush the in-memory `attestations` table to a new Parquet file at
    /// `<location>/attestations/<millis>-<uuid>.parquet` and clear the buffer.
    /// A no-op when the buffer is empty.
    pub fn flush(&self) -> Result<()> {
        let count: i64 = self
            .conn
            .query_row("SELECT COUNT(*) FROM attestations", [], |row| row.get(0))
            .unwrap_or(0);
        if count == 0 {
            return Ok(());
        }
        let base = self.location_path();
        if !is_remote(&self.location) {
            let _ = std::fs::create_dir_all(format!("{}/attestations", base));
        }
        let ms = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map(|d| d.as_millis())
            .unwrap_or(0);
        let file = format!(
            "{}/attestations/{}-{}.parquet",
            base,
            ms,
            uuid::Uuid::new_v4()
        );
        self.conn.execute_batch(&format!(
            "BEGIN TRANSACTION;
             COPY attestations TO '{}' (FORMAT PARQUET);
             DELETE FROM attestations;
             COMMIT;",
            file
        ))?;
        Ok(())
    }

    /// The location as a path DuckDB SQL can consume. Strips the `file://`
    /// prefix (DuckDB expects bare paths for local files); passes remote
    /// schemes through unchanged.
    fn location_path(&self) -> String {
        self.location
            .strip_prefix("file://")
            .map(String::from)
            .unwrap_or_else(|| self.location.clone())
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
    /// duckdb-rs v1.10504.0 not supporting `Value::List` as a bind parameter
    /// (see `str_list_json` doc comment).
    ///
    /// Semantics match `ats.AttestationFilter` (Go, `ats/store.go:69-79`):
    /// OR within a list field, AND between fields.
    pub fn query(&self, filter: &QueryFilter) -> Result<Vec<Attestation>> {
        let mut sql = String::from(
            "SELECT id, subjects, predicates, contexts, actors, timestamp, source, \
             attributes, created_at, signature, signer_did FROM attestations",
        );
        let mut conds: Vec<&'static str> = Vec::new();
        let mut binds: Vec<Value> = Vec::new();

        if !filter.subjects.is_empty() {
            conds.push("list_has_any(subjects, CAST(? AS VARCHAR[]))");
            binds.push(Value::Text(str_list_json(&filter.subjects)));
        }
        if !filter.predicates.is_empty() {
            conds.push("list_has_any(predicates, CAST(? AS VARCHAR[]))");
            binds.push(Value::Text(str_list_json(&filter.predicates)));
        }
        if !filter.contexts.is_empty() {
            conds.push("list_has_any(contexts, CAST(? AS VARCHAR[]))");
            binds.push(Value::Text(str_list_json(&filter.contexts)));
        }
        if !filter.actors.is_empty() {
            conds.push("list_has_any(actors, CAST(? AS VARCHAR[]))");
            binds.push(Value::Text(str_list_json(&filter.actors)));
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
                    str_list_json(&attestation.subjects),
                    str_list_json(&attestation.predicates),
                    str_list_json(&attestation.contexts),
                    str_list_json(&attestation.actors),
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
        let mut stmt = self
            .conn
            .prepare(
                "SELECT id, subjects, predicates, contexts, actors, timestamp, source,
                        attributes, created_at, signature, signer_did
                 FROM attestations WHERE id = ?",
            )
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
                    str_list_json(&attestation.subjects),
                    str_list_json(&attestation.predicates),
                    str_list_json(&attestation.contexts),
                    str_list_json(&attestation.actors),
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
            eprintln!("qntx-duckdb: final flush failed: {}", e);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use qntx_core::attestation::Attestation;
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

    #[test]
    fn open_creates_schema() {
        let store = DuckdbStore::open("file:///tmp/qntx-duckdb-test").unwrap();
        assert_eq!(store.location(), "file:///tmp/qntx-duckdb-test");
    }

    #[test]
    fn put_and_get_round_trip() {
        let mut store = DuckdbStore::open("file:///tmp/qntx-duckdb-test").unwrap();
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
        let store = DuckdbStore::open("file:///tmp/qntx-duckdb-test").unwrap();
        assert!(store.get("AS-missing").unwrap().is_none());
    }

    #[test]
    fn put_duplicate_rejects() {
        let mut store = DuckdbStore::open("file:///tmp/qntx-duckdb-test").unwrap();
        let a = sample_attestation("AS-1");
        store.put(a.clone()).unwrap();
        match store.put(a) {
            Err(StoreError::AlreadyExists(_)) => {}
            other => panic!("expected AlreadyExists, got {:?}", other),
        }
    }

    #[test]
    fn delete_removes() {
        let mut store = DuckdbStore::open("file:///tmp/qntx-duckdb-test").unwrap();
        store.put(sample_attestation("AS-1")).unwrap();
        assert!(store.delete("AS-1").unwrap());
        assert!(store.get("AS-1").unwrap().is_none());
    }

    #[test]
    fn ids_lists_stored() {
        let mut store = DuckdbStore::open("file:///tmp/qntx-duckdb-test").unwrap();
        store.put(sample_attestation("AS-1")).unwrap();
        store.put(sample_attestation("AS-2")).unwrap();
        let ids = store.ids().unwrap();
        assert_eq!(ids.len(), 2);
    }

    #[test]
    fn clear_wipes() {
        let mut store = DuckdbStore::open("file:///tmp/qntx-duckdb-test").unwrap();
        store.put(sample_attestation("AS-1")).unwrap();
        store.clear().unwrap();
        assert_eq!(store.count().unwrap(), 0);
    }
}
