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
pub mod objects;
pub mod schedules;
pub mod tokens;
pub mod users;
pub mod watchers;

// FFI module for CGO integration.
#[cfg(feature = "ffi")]
pub mod ffi;

pub use error::{DuckdbError, Name, Object, Refusal, Result};

use ats::attestation::Attestation;
use ats::storage::{AttestationStore, StoreError};
use duckdb::types::Value;
use serde::{Deserialize, Serialize};

use crate::objects::{Asked, Objects, Request};

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

/// A path is about to be a string inside SQL, where a quote would end that
/// string early. Paths come back from a listing of the location, so this
/// answers for any path the location holds.
fn refuse_quoted(path: &str) -> Result<()> {
    if path.contains('\'') {
        return Err(DuckdbError::BadName {
            which: Name::Location,
            value: path.to_string(),
            why: Refusal::CarriesAQuote,
        });
    }
    Ok(())
}

/// Whether the location holds nothing under `prefix`. The `*` is what makes it
/// a question: without a wildcard, glob hands the pattern back unexamined.
pub(crate) fn holds_nothing(conn: &duckdb::Connection, prefix: &str) -> Result<bool> {
    let sql = format!("SELECT count(*) FROM glob('{prefix}/*')");
    let count: i64 = conn
        .query_row(&sql, [], |row| row.get(0))
        .map_err(|source| DuckdbError::Read {
            what: Object::Namespaces,
            under: prefix.to_string(),
            source,
        })?;
    Ok(count == 0)
}

/// A read failed and the location holds nothing, so it answered empty. stderr
/// because nothing in the workspace installs a tracing subscriber.
pub(crate) fn took_as_empty(what: &Object, under: &str, e: &duckdb::Error) {
    eprintln!("ats-duckdb: {what} under {under}: the location holds nothing, so this read answered empty: {e}");
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
    what: Object,
) -> Result<Option<duckdb::Statement<'a>>> {
    let first = match conn.prepare(sql) {
        Ok(stmt) => return Ok(Some(stmt)),
        Err(e) => e,
    };

    if let Err(source) = resolve_credentials_again(conn, location) {
        return Err(DuckdbError::ReadThenNoCredentials {
            what,
            under: prefix.to_string(),
            first: Box::new(first),
            source: Box::new(source),
        });
    }

    match conn.prepare(sql) {
        Ok(stmt) => Ok(Some(stmt)),
        Err(again) => {
            if !holds_nothing(conn, prefix)? {
                return Err(DuckdbError::ReadTwice {
                    what,
                    under: prefix.to_string(),
                    first: Box::new(first),
                    source: Box::new(again),
                });
            }
            took_as_empty(&what, prefix, &again);
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
pub(crate) fn resolve_credentials_again(
    conn: &duckdb::Connection,
    location: &str,
) -> std::result::Result<(), duckdb::Error> {
    match remote_setup_sql(location) {
        Some(sql) => conn.execute_batch(&sql),
        None => Ok(()),
    }
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
                other => Err(DuckdbError::ColumnShape {
                    expected: "VARCHAR in list",
                    got: other,
                }),
            })
            .collect(),
        Value::Null => Ok(Vec::new()),
        other => Err(DuckdbError::ColumnShape {
            expected: "LIST<VARCHAR>",
            got: other,
        }),
    }
}

/// Filter shape accepted by `DuckdbStore::query`. Mirrors the JSON that
/// `ats/storage/sqlitecgo/storage_cgo.go:GetAttestations` sends to the SQLite
/// FFI, so the Go-side wrapper can serialize the same struct for either
/// backend. Each list field is OR-logic within, all fields are AND'd together
/// (matches `ats.AttestationFilter` semantics in `ats/store.go:69-79`).
#[derive(Debug, Default, Deserialize)]
#[serde(deny_unknown_fields)]
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
/// library on the host is a different release, the bindings describe an ABI
/// that is not there. That failure is silent at compile and link time.
///
/// `flake.nix` pins libduckdb to this version through its nixpkgs revision.
/// Changing any one of the three — this constant, the crate version in
/// Cargo.toml, the flake revision — without the others is the bug this guards
/// against.
///
/// Why 1.4.3 rather than something newer: the nixpkgs revision carrying a
/// later DuckDB also carries a glibc newer than the deployment target's, and
/// libduckdb then cannot load there. See flake.nix.
const EXPECTED_DUCKDB_VERSION: &str = "v1.4.3";

/// Compare the linked library against [`EXPECTED_DUCKDB_VERSION`].
///
/// Called on every store open. A mismatch is fatal rather than a warning: a
/// warning is a thing nobody reads until they are already debugging the
/// corruption it predicted.
pub(crate) fn assert_library_version(conn: &duckdb::Connection) -> Result<()> {
    let actual: String = conn.query_row("SELECT version()", [], |row| row.get(0))?;
    // duckdb-rs in crates/ats-duckdb/Cargo.toml and libduckdb pinned by the
    // nixpkgs-duckdb input in flake.nix: bump them together or not at all.
    if actual != EXPECTED_DUCKDB_VERSION {
        return Err(DuckdbError::VersionMismatch {
            linked: actual,
            expected: EXPECTED_DUCKDB_VERSION.to_string(),
        });
    }
    Ok(())
}

/// Every column of an attestation, in the order the migration declares them.
/// One list, so a file written by `compact` carries the shape the read paths
/// select.
const COLUMNS: &str = "id, subjects, predicates, contexts, actors, timestamp, \
                       source, attributes, created_at, signature, signer_did";

/// How many Parquet files a namespace may hold before `compact_when_crowded`
/// merges them.
///
/// A read opens every file under the prefix, and against S3 that is a round
/// trip each, so this number is the read cost. Raising it makes reads dearer
/// and merges rarer.
const COMPACT_AT: usize = 16;

/// How many files one merge takes at most.
///
/// A merge holds the store for as long as it takes to read what it merges and
/// delete it, and every other read and write waits on that, so this number is
/// the longest that wait gets. A namespace holding more files is worked off
/// over several runs, each leaving fewer than it found.
const MERGE_AT_MOST: usize = 250;

/// Where the record of a compaction underway sits: beside the files it is
/// merging, under a name `parquet_glob` skips, so a reader opens the files
/// alone.
const COMPACTION_OBJECT: &str = "compaction.json";

/// What one merge did: how many files it replaced, and how many bytes the file
/// it wrote holds. A merge rewrites what the namespace holds, so the bytes are
/// the price of a cheap read (ADR-024, Compaction). Zero of each is no merge.
#[derive(Debug, Default, Clone, Copy, PartialEq, Eq)]
pub struct Merged {
    pub files: usize,
    pub bytes: u64,
}

/// A compaction that has begun: the file it will write, and the files that
/// file replaces. Written first and removed once the sources are gone, so it
/// is present exactly while a run is in flight.
#[derive(Serialize, Deserialize)]
struct Compaction {
    merged: String,
    sources: Vec<String>,
}

pub struct DuckdbStore {
    location: String,
    /// Where this store's attestations live. Namespace is the top-level
    /// prefix, so a store reaches its own namespace and no other.
    prefix: String,
    conn: duckdb::Connection,
    /// The location's own client, which is what issues the deletes compaction
    /// ends with. DuckDB's httpfs reads and writes objects.
    objects: Objects,
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
        let prefix = namespace::prefix(&location, namespace.as_ref(), namespace::ATTESTATIONS);
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
        let store = Self {
            objects: Objects::open(&location)?,
            location,
            prefix,
            conn,
        };
        // Here is before any read reaches the state an interrupted run left.
        store.finish_any_compaction()?;
        Ok(store)
    }

    /// The source a read unions the buffer with: the buffer alone when the
    /// location holds no file, and the buffer beside the files named one by
    /// one when it does.
    ///
    /// Naming them is the listing `parquet_files` already takes, and it stands
    /// in for two — the count that asked whether any file exists, and the glob
    /// `read_parquet` expands against the location itself. Compaction names
    /// the files it merges this way already.
    ///
    /// The credentials the `read_parquet` runs under are resolved here, the
    /// same call compaction makes before its own statement.
    fn read_source(&self, columns: &str) -> Result<String> {
        let files = self.parquet_files()?;
        if files.is_empty() {
            return Ok("attestations".to_string());
        }
        for path in &files {
            refuse_quoted(path)?;
        }
        resolve_credentials_again(&self.conn, &self.location)?;
        // What this read is about to cost inside DuckDB: a round trip per file
        // it is handed (ADR-024, Consequences). Counted from the list we name
        // for it, because httpfs holds its own client and reaches nothing here.
        self.objects
            .noted(&Object::Attestations, Request::Get, files.len() as u64);
        let held = files
            .iter()
            .map(|path| format!("'{path}'"))
            .collect::<Vec<_>>()
            .join(", ");
        Ok(format!(
            "(SELECT {columns} FROM attestations \
             UNION ALL SELECT {columns} FROM read_parquet([{held}]))"
        ))
    }

    /// Flush the in-memory `attestations` table to a new Parquet file at
    /// `<location>/attestations/<millis>-<uuid>.parquet` and clear the buffer.
    /// A no-op when the buffer is empty. Answers how many rows it wrote, which
    /// is how a caller learns a file was added.
    pub fn flush(&self) -> Result<usize> {
        let count: i64 = self
            .conn
            .query_row("SELECT COUNT(*) FROM attestations", [], |row| row.get(0))?;
        if count == 0 {
            return Ok(0);
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
        let file = format!("{}/{}.parquet", self.prefix, self.stamp()?);
        self.conn.execute_batch(&format!(
            "BEGIN TRANSACTION;
             COPY attestations TO '{}' (FORMAT PARQUET);
             DELETE FROM attestations;
             COMMIT;",
            file
        ))?;
        Ok(count as usize)
    }

    /// Write the attestations handed in as one new Parquet file, and answer
    /// how many rows it holds. A no-op for none.
    ///
    /// The landing file is the buffer (ADR-037): it already refused a
    /// duplicate id, so nothing here reads the files to ask again. The rows
    /// pass through a table of their own, emptied before it is filled, so a
    /// write that failed leaves nothing a retry would write twice.
    pub fn write_file(&self, attestations: &[Attestation]) -> Result<usize> {
        if attestations.is_empty() {
            return Ok(0);
        }
        resolve_credentials_again(&self.conn, &self.location)?;
        if !is_remote(&self.location) {
            std::fs::create_dir_all(&self.prefix)?;
        }
        self.conn.execute_batch(
            "CREATE TEMP TABLE IF NOT EXISTS handed AS SELECT * FROM attestations LIMIT 0;
             DELETE FROM handed;",
        )?;
        for attestation in attestations {
            self.insert("handed", attestation)?;
        }
        let file = format!("{}/{}.parquet", self.prefix, self.stamp()?);
        self.conn.execute_batch(&format!(
            "COPY (SELECT {COLUMNS} FROM handed) TO '{file}' (FORMAT PARQUET);
             DELETE FROM handed;"
        ))?;
        Ok(attestations.len())
    }

    /// One attestation into `table`, which has the attestations table's shape.
    fn insert(&self, table: &str, attestation: &Attestation) -> Result<()> {
        let attributes_json = if attestation.attributes.is_empty() {
            None
        } else {
            Some(serde_json::to_string(&attestation.attributes)?)
        };
        self.conn.execute(
            &format!(
                "INSERT INTO {table}
                 ({COLUMNS})
                 VALUES (
                     ?,
                     CAST(? AS VARCHAR[]),
                     CAST(? AS VARCHAR[]),
                     CAST(? AS VARCHAR[]),
                     CAST(? AS VARCHAR[]),
                     ?, ?, ?, ?, ?, ?
                 )"
            ),
            duckdb::params![
                attestation.id,
                str_list_json(&attestation.subjects)?,
                str_list_json(&attestation.predicates)?,
                str_list_json(&attestation.contexts)?,
                str_list_json(&attestation.actors)?,
                attestation.timestamp,
                attestation.source,
                attributes_json,
                attestation.created_at,
                attestation.signature,
                attestation.signer_did,
            ],
        )?;
        Ok(())
    }

    /// Compaction as ADR-024 declares it: on a threshold, per namespace.
    pub fn compact_when_crowded(&self) -> Result<Merged> {
        self.finish_any_compaction()?;
        if self.parquet_files()?.len() < COMPACT_AT {
            return Ok(Merged::default());
        }
        self.compact()
    }

    /// How many Parquet files this namespace holds. Against S3, one listing.
    pub fn file_count(&self) -> Result<usize> {
        Ok(self.parquet_files()?.len())
    }

    /// The requests this namespace has made of its location since it opened.
    ///
    /// What the record costs is a count of requests and not of statements
    /// (ADR-024, Consequences), and nothing before this could say the first
    /// number. The `read_parquet` DuckDB runs is its own client and is not
    /// counted here; everything this crate asks for is.
    pub fn asked(&self) -> Vec<Asked> {
        self.objects.asked()
    }

    /// The record is written first and removed last, so every state a crash
    /// leaves is one `finish_any_compaction` can read the record to name and
    /// finish. Every row stays held throughout.
    ///
    /// This works on files. Rows in the buffer reach one through `flush`.
    pub fn compact(&self) -> Result<Merged> {
        let mut sources = self.parquet_files()?;
        // Oldest first, so a run leaves the newest behind for the next one.
        sources.truncate(MERGE_AT_MOST);
        if sources.len() < 2 {
            return Ok(Merged::default());
        }

        for path in &sources {
            refuse_quoted(path)?;
        }

        // The credentials the COPY below signs with are resolved by this call.
        resolve_credentials_again(&self.conn, &self.location)?;
        if !is_remote(&self.location) {
            std::fs::create_dir_all(&self.prefix)?;
        }

        let merged = format!("{}/{}.parquet", self.prefix, self.stamp()?);
        self.objects.put(
            Object::Compaction,
            &self.compaction_object(),
            serde_json::to_vec(&Compaction {
                merged: merged.clone(),
                sources: sources.clone(),
            })?,
        )?;

        // A merge reads every source through DuckDB, one round trip each, the
        // same as a read does. Counted against compaction rather than the
        // reads, because it is compaction that chose to open them.
        self.objects
            .noted(&Object::Compaction, Request::Get, sources.len() as u64);

        // The files this run named, so a file written since the listing stays
        // where it is.
        let held = sources
            .iter()
            .map(|path| format!("'{path}'"))
            .collect::<Vec<_>>()
            .join(", ");
        self.conn.execute_batch(&format!(
            "COPY (SELECT {COLUMNS} FROM read_parquet([{held}])) TO '{merged}' (FORMAT PARQUET)"
        ))?;
        let bytes = self.objects.size(Object::ParquetFiles, &merged)?;

        for path in &sources {
            self.objects.delete(Object::ParquetFiles, path)?;
        }
        self.objects
            .delete(Object::Compaction, &self.compaction_object())?;
        Ok(Merged {
            files: sources.len(),
            bytes,
        })
    }

    /// Finish the compaction a dead process left behind.
    ///
    /// The merged file decides. Absent, the sources are still the whole store.
    /// Whole, its rows are held twice and the sources go. Partial, every read
    /// would trip over it and it goes.
    ///
    /// Idempotent, so a run that dies here is finished by the next one.
    fn finish_any_compaction(&self) -> Result<()> {
        let object = self.compaction_object();
        let Some(bytes) = self.objects.get(Object::Compaction, &object)? else {
            return Ok(());
        };
        let underway: Compaction =
            serde_json::from_slice(&bytes).map_err(|source| DuckdbError::NotJSON {
                what: Object::Compaction,
                path: object.clone(),
                source,
            })?;

        refuse_quoted(&underway.merged)?;
        if self.parquet_files()?.contains(&underway.merged) {
            if self.reads_whole(&underway.merged) {
                for path in &underway.sources {
                    self.objects.delete(Object::ParquetFiles, path)?;
                }
            } else {
                self.objects
                    .delete(Object::ParquetFiles, &underway.merged)?;
            }
        }
        self.objects.delete(Object::Compaction, &object)
    }

    /// Whether the Parquet file at `path` is whole. A footer is what a count
    /// needs and what an interrupted write lacks, so the answer costs the
    /// metadata alone.
    fn reads_whole(&self, path: &str) -> bool {
        self.conn
            .query_row(
                &format!("SELECT count(*) FROM read_parquet('{path}')"),
                [],
                |row| row.get::<_, i64>(0),
            )
            .is_ok()
    }

    /// Every Parquet file under the prefix, in write order: the names carry
    /// the millisecond, so sorting them is sorting by when.
    ///
    /// Listed through the location's own client, which answers with the paths
    /// a delete needs. `glob` answers with a count.
    fn parquet_files(&self) -> Result<Vec<String>> {
        let mut files: Vec<String> = self
            .objects
            .list(Object::ParquetFiles, &self.prefix)?
            .into_iter()
            .filter(|path| path.ends_with(".parquet"))
            .collect();
        files.sort();
        Ok(files)
    }

    fn compaction_object(&self) -> String {
        format!("{}/{COMPACTION_OBJECT}", self.prefix)
    }

    /// The millisecond and a uuid: a name no other write takes.
    fn stamp(&self) -> Result<String> {
        let ms = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map_err(DuckdbError::ClockBeforeEpoch)?
            .as_millis();
        Ok(format!("{ms}-{}", uuid::Uuid::new_v4()))
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

        let source = self.read_source(COLUMNS)?;

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

        let source = self.read_source(COLUMNS)?;

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

        self.insert("attestations", &attestation)
            .map_err(|e| StoreError::Backend(e.sacred_json("")))
    }

    fn get(&self, id: &str) -> StoreResult<Option<Attestation>> {
        // Buffer and files, for the reason query gives: a flushed attestation
        // is not in the buffer, and "not in the buffer" is not "does not exist".
        let source = self
            .read_source(COLUMNS)
            .map_err(|e| StoreError::Backend(e.sacred_json("")))?;
        let sql = format!("SELECT {COLUMNS} FROM {source} WHERE id = ? LIMIT 1");

        let mut stmt = self
            .conn
            .prepare(&sql)
            .map_err(|e| StoreError::Backend(DuckdbError::from(e).sacred_json("")))?;

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
                Self::row_to_attestation(r).map_err(|e| StoreError::Backend(e.sacred_json("")))?,
            )),
            Err(duckdb::Error::QueryReturnedNoRows) => Ok(None),
            Err(e) => Err(StoreError::Backend(DuckdbError::from(e).sacred_json(""))),
        }
    }

    fn delete(&mut self, id: &str) -> StoreResult<bool> {
        let rows = self
            .conn
            .execute("DELETE FROM attestations WHERE id = ?", [id])
            .map_err(|e| StoreError::Backend(DuckdbError::from(e).sacred_json("")))?;
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
                    .map_err(|e| StoreError::Backend(DuckdbError::from(e).sacred_json("")))?,
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
                        .map_err(|e| StoreError::Backend(DuckdbError::from(e).sacred_json("")))?,
                    str_list_json(&attestation.predicates)
                        .map_err(|e| StoreError::Backend(DuckdbError::from(e).sacred_json("")))?,
                    str_list_json(&attestation.contexts)
                        .map_err(|e| StoreError::Backend(DuckdbError::from(e).sacred_json("")))?,
                    str_list_json(&attestation.actors)
                        .map_err(|e| StoreError::Backend(DuckdbError::from(e).sacred_json("")))?,
                    attestation.timestamp,
                    attestation.source,
                    attributes_json,
                    attestation.signature,
                    attestation.signer_did,
                    attestation.id,
                ],
            )
            .map_err(|e| StoreError::Backend(DuckdbError::from(e).sacred_json("")))?;
        Ok(())
    }

    /// Count buffer and files. The default trait implementation takes the
    /// length of `ids`, which reads the buffer alone, and flush empties the
    /// buffer every few seconds.
    fn count(&self) -> StoreResult<usize> {
        // flush copies the buffer into a file and empties it in one
        // transaction, so no row is in both and UNION ALL does not double.
        let source = self
            .read_source("id")
            .map_err(|e| StoreError::Backend(e.sacred_json("")))?;
        let sql = format!("SELECT count(*) FROM {source}");
        let total: i64 = self
            .conn
            .query_row(&sql, [], |row| row.get(0))
            .map_err(|e| StoreError::Backend(DuckdbError::from(e).sacred_json("")))?;
        Ok(total as usize)
    }

    fn ids(&self) -> StoreResult<Vec<String>> {
        let mut stmt = self
            .conn
            .prepare("SELECT id FROM attestations ORDER BY created_at DESC")
            .map_err(|e| StoreError::Backend(DuckdbError::from(e).sacred_json("")))?;

        let rows = stmt
            .query_map([], |row| row.get::<_, String>(0))
            .map_err(|e| StoreError::Backend(DuckdbError::from(e).sacred_json("")))?;

        let mut ids = Vec::new();
        for row in rows {
            ids.push(row.map_err(|e| StoreError::Backend(DuckdbError::from(e).sacred_json("")))?);
        }
        Ok(ids)
    }

    fn clear(&mut self) -> StoreResult<()> {
        self.conn
            .execute("DELETE FROM attestations", [])
            .map_err(|e| StoreError::Backend(DuckdbError::from(e).sacred_json("")))?;
        Ok(())
    }
}

impl Drop for DuckdbStore {
    fn drop(&mut self) {
        // Best-effort final flush so buffered attestations reach durable
        // storage on shutdown. Errors are logged, not surfaced — Drop can't
        // return them, and refusing to drop would leak the connection.
        if let Err(e) = self.flush() {
            eprintln!("ats-duckdb: final flush failed: {}", e.sacred_json(""));
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

    /// One attestation per flush, so the store holds `n` files.
    fn written_one_at_a_time(store: &mut DuckdbStore, n: usize) {
        for i in 0..n {
            store.put(sample_attestation(&format!("AS-{i}"))).unwrap();
            store.flush().unwrap();
        }
    }

    #[test]
    fn flush_says_how_many_rows_it_wrote() {
        let dir = tempfile::tempdir().unwrap();
        let mut store = store(&dir);
        assert_eq!(store.flush().unwrap(), 0);
        store.put(sample_attestation("AS-1")).unwrap();
        store.put(sample_attestation("AS-2")).unwrap();
        assert_eq!(store.flush().unwrap(), 2);
        assert_eq!(store.flush().unwrap(), 0);
    }

    // Rows handed in become one file, readable like any flushed one, and the
    // buffer is not what they passed through.
    #[test]
    fn rows_handed_in_are_one_file() {
        let dir = tempfile::tempdir().unwrap();
        let mut store = store(&dir);
        store.put(sample_attestation("AS-buffered")).unwrap();

        let handed = vec![sample_attestation("AS-1"), sample_attestation("AS-2")];
        assert_eq!(store.write_file(&handed).unwrap(), 2);

        assert_eq!(store.parquet_files().unwrap().len(), 1);
        assert_eq!(store.get("AS-2").unwrap().unwrap().id, "AS-2");
        assert_eq!(
            store.flush().unwrap(),
            1,
            "the buffer held its own row only"
        );
        assert_eq!(store.count().unwrap(), 3);
    }

    #[test]
    fn nothing_handed_in_writes_no_file() {
        let dir = tempfile::tempdir().unwrap();
        let store = store(&dir);
        assert_eq!(store.write_file(&[]).unwrap(), 0);
        assert!(store.parquet_files().unwrap().is_empty());
    }

    // A second write does not carry the first one's rows.
    #[test]
    fn each_write_holds_only_what_it_was_handed() {
        let dir = tempfile::tempdir().unwrap();
        let store = store(&dir);
        store.write_file(&[sample_attestation("AS-1")]).unwrap();
        store.write_file(&[sample_attestation("AS-2")]).unwrap();
        assert_eq!(store.parquet_files().unwrap().len(), 2);
        assert_eq!(store.count().unwrap(), 2);
    }

    // Many files become one, and every row survives.
    #[test]
    fn compaction_leaves_one_file_holding_everything() {
        let dir = tempfile::tempdir().unwrap();
        let mut store = store(&dir);
        written_one_at_a_time(&mut store, 5);
        assert_eq!(store.parquet_files().unwrap().len(), 5);

        let merged = store.compact().unwrap();
        assert_eq!(merged.files, 5);

        let files = store.parquet_files().unwrap();
        assert_eq!(files.len(), 1);
        assert_eq!(store.file_count().unwrap(), 1);
        assert_eq!(store.count().unwrap(), 5);
        assert_eq!(store.get("AS-3").unwrap().unwrap().id, "AS-3");

        // The bytes are the merged file's own, as the filesystem counts them.
        let on_disk = std::fs::metadata(files[0].trim_start_matches("file://"))
            .unwrap()
            .len();
        assert_eq!(merged.bytes, on_disk);
    }

    // A row written after a merge is held alongside the merged file.
    #[test]
    fn writes_after_a_compaction_are_held_too() {
        let dir = tempfile::tempdir().unwrap();
        let mut store = store(&dir);
        written_one_at_a_time(&mut store, 3);
        store.compact().unwrap();

        store.put(sample_attestation("AS-late")).unwrap();
        store.flush().unwrap();

        assert_eq!(store.parquet_files().unwrap().len(), 2);
        assert_eq!(store.count().unwrap(), 4);
        assert_eq!(store.get("AS-late").unwrap().unwrap().id, "AS-late");
    }

    // The threshold is what a merge waits for.
    #[test]
    fn a_store_that_is_not_crowded_is_left_alone() {
        let dir = tempfile::tempdir().unwrap();
        let mut store = store(&dir);
        written_one_at_a_time(&mut store, COMPACT_AT - 1);
        assert_eq!(store.compact_when_crowded().unwrap(), Merged::default());
        assert_eq!(store.parquet_files().unwrap().len(), COMPACT_AT - 1);

        store.put(sample_attestation("AS-one-more")).unwrap();
        store.flush().unwrap();
        assert_eq!(store.compact_when_crowded().unwrap().files, COMPACT_AT);
        assert_eq!(store.parquet_files().unwrap().len(), 1);
        assert_eq!(store.count().unwrap(), COMPACT_AT);
    }

    // A merge wants two files, and answers zero below that.
    #[test]
    fn a_store_with_one_file_has_nothing_to_merge() {
        let dir = tempfile::tempdir().unwrap();
        let mut store = store(&dir);
        assert_eq!(store.compact().unwrap(), Merged::default());
        written_one_at_a_time(&mut store, 1);
        assert_eq!(store.compact().unwrap(), Merged::default());
    }

    // A crash between the merge and its deletes: every row held twice, and
    // opening the store is what finishes it.
    #[test]
    fn an_interrupted_compaction_finishes_on_open() {
        let dir = tempfile::tempdir().unwrap();
        let mut first = store(&dir);
        written_one_at_a_time(&mut first, 4);
        let sources = first.parquet_files().unwrap();

        // What compact does, stopped after the record was written.
        let merged = format!("{}/{}.parquet", first.prefix, first.stamp().unwrap());
        let held = sources
            .iter()
            .map(|p| format!("'{p}'"))
            .collect::<Vec<_>>()
            .join(", ");
        first
            .conn
            .execute_batch(&format!(
                "COPY (SELECT {COLUMNS} FROM read_parquet([{held}])) TO '{merged}' (FORMAT PARQUET)"
            ))
            .unwrap();
        first
            .objects
            .put(
                Object::Compaction,
                &first.compaction_object(),
                serde_json::to_vec(&Compaction {
                    merged,
                    sources: sources.clone(),
                })
                .unwrap(),
            )
            .unwrap();
        assert_eq!(first.count().unwrap(), 8, "every row is held twice");
        drop(first);

        let reopened = store(&dir);
        assert_eq!(reopened.parquet_files().unwrap().len(), 1);
        assert_eq!(reopened.count().unwrap(), 4);
    }

    // A crash during the merge write: a partial file every read would trip
    // over, and the sources are still the store.
    #[test]
    fn a_compaction_whose_merge_stopped_partway_drops_it() {
        let dir = tempfile::tempdir().unwrap();
        let mut first = store(&dir);
        written_one_at_a_time(&mut first, 4);
        let sources = first.parquet_files().unwrap();

        let half = format!("{}/{}.parquet", first.prefix, first.stamp().unwrap());
        first
            .objects
            .put(
                Object::ParquetFiles,
                &half,
                b"PAR1 and then nothing".to_vec(),
            )
            .unwrap();
        first
            .objects
            .put(
                Object::Compaction,
                &first.compaction_object(),
                serde_json::to_vec(&Compaction {
                    merged: half.clone(),
                    sources: sources.clone(),
                })
                .unwrap(),
            )
            .unwrap();
        drop(first);

        let reopened = store(&dir);
        assert_eq!(reopened.parquet_files().unwrap(), sources);
        assert_eq!(reopened.count().unwrap(), 4);
    }

    // A crash before the merge write: the sources are the whole store, and
    // the record is what keeps them.
    #[test]
    fn a_compaction_whose_merge_never_landed_keeps_its_sources() {
        let dir = tempfile::tempdir().unwrap();
        let mut first = store(&dir);
        written_one_at_a_time(&mut first, 4);
        let sources = first.parquet_files().unwrap();

        first
            .objects
            .put(
                Object::Compaction,
                &first.compaction_object(),
                serde_json::to_vec(&Compaction {
                    merged: format!("{}/never-written.parquet", first.prefix),
                    sources: sources.clone(),
                })
                .unwrap(),
            )
            .unwrap();
        drop(first);

        let reopened = store(&dir);
        assert_eq!(reopened.parquet_files().unwrap(), sources);
        assert_eq!(reopened.count().unwrap(), 4);
    }
}
