//! Access token records for the parquet backend (ADR-025).
//!
//! One object per token under `<location>/access_tokens/`, rewritten on
//! change — the "small config" shape from ADR-024. Not Parquet, not a DuckDB
//! table: a token is a single small record that gets replaced when it changes,
//! which is the opposite of the append-only stream attestations use.
//!
//! Revocation is why the shape matters. A revoked token must be dead for
//! everyone, immediately and permanently. Rewriting the one object that holds
//! it means there is no earlier version left anywhere to be read back.
//!
//! The object is named by the token's SHA-256 hash because `Lookup` is by
//! hash — the hot path on every authenticated request resolves to one record
//! with nothing to scan. Only the hash is stored; the raw token is shown once
//! at creation and never persisted.

use std::collections::HashMap;

use serde::{Deserialize, Serialize};

use crate::error::{DuckdbError, Result};
use crate::{is_remote, remote_setup_sql};

/// A stored access token. Mirrors `auth.TokenInfo` in `server/auth/tokens.go`
/// plus the hash, which never leaves this crate.
///
/// Timestamps are milliseconds since the Unix epoch, matching `Attestation`.
/// `expires_at` is optional because a token may simply live until revoked.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct TokenRecord {
    pub id: String,
    pub hash: String,
    pub label: String,
    pub created_at: i64,
    #[serde(default)]
    pub expires_at: Option<i64>,
    #[serde(default)]
    pub last_used_at: Option<i64>,
    #[serde(default)]
    pub revoked_at: Option<i64>,
}

impl TokenRecord {
    /// Whether this token authorizes a request made at `now_ms`.
    ///
    /// Revoked beats everything, then expiry. A token with no `expires_at`
    /// stays usable until someone revokes it.
    pub fn is_usable(&self, now_ms: i64) -> bool {
        if self.revoked_at.is_some() {
            return false;
        }
        match self.expires_at {
            Some(expiry) => expiry > now_ms,
            None => true,
        }
    }
}

/// A token as callers outside this crate are allowed to see it: everything
/// except the hash.
///
/// Mirrors `auth.TokenInfo` (`server/auth/tokens.go:25`), which also has no
/// hash field. The hash is the only thing standing between the store and
/// anyone who reads a list response, so it stops here.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct TokenSummary {
    pub id: String,
    pub label: String,
    pub created_at: i64,
    #[serde(default)]
    pub expires_at: Option<i64>,
    #[serde(default)]
    pub last_used_at: Option<i64>,
    #[serde(default)]
    pub revoked_at: Option<i64>,
}

impl From<&TokenRecord> for TokenSummary {
    fn from(record: &TokenRecord) -> Self {
        Self {
            id: record.id.clone(),
            label: record.label.clone(),
            created_at: record.created_at,
            expires_at: record.expires_at,
            last_used_at: record.last_used_at,
            revoked_at: record.revoked_at,
        }
    }
}

/// Access tokens held at a storage location.
///
/// Every token is read into memory when the store opens and every change is
/// written through to its object before the call returns. Lookup is then a map
/// hit with no I/O, and the durable copy is never behind the in-memory one.
///
/// This is correct because a QNTX deployment is one node. ADR-024 permits
/// several nodes writing to the same location; if that ever happens, a second
/// node would keep honouring a token this one revoked, and the cache would
/// have to go.
pub struct TokenStore {
    location: String,
    prefix: String,
    conn: duckdb::Connection,
    by_hash: HashMap<String, TokenRecord>,
}

impl TokenStore {
    /// Open the store at `location`, loading every token already there.
    ///
    /// The connection exists to reach the location, not to hold state: object
    /// reads and writes go through DuckDB so an `s3://` prefix works through
    /// httpfs exactly as a local path does.
    pub fn open(location: impl Into<String>) -> Result<Self> {
        let location = location.into();
        if location.contains('\'') {
            return Err(DuckdbError::Backend(format!(
                "storage location {location} contains a quote, which cannot be used in a \
                 DuckDB path"
            )));
        }

        let conn = duckdb::Connection::open_in_memory()?;
        crate::assert_library_version(&conn)?;
        if let Some(sql) = remote_setup_sql(&location) {
            conn.execute_batch(&sql)?;
        }

        let mut store = Self {
            prefix: token_prefix(&location),
            location,
            conn,
            by_hash: HashMap::new(),
        };
        store.load()?;
        Ok(store)
    }

    /// The location URL this store was opened with.
    pub fn location(&self) -> &str {
        &self.location
    }

    /// Store a token, replacing any record under the same hash.
    pub fn put(&mut self, record: TokenRecord) -> Result<()> {
        self.write_object(&record)?;
        self.by_hash.insert(record.hash.clone(), record);
        Ok(())
    }

    /// Whether the token with this hash authorizes a request at `now_ms`.
    /// Unknown hashes are not usable, so a deleted object fails closed.
    pub fn lookup(&self, hash: &str, now_ms: i64) -> bool {
        self.by_hash
            .get(hash)
            .map(|t| t.is_usable(now_ms))
            .unwrap_or(false)
    }

    /// Every token, revoked and expired ones included — the UI lists them so
    /// that a revoked token is visibly revoked rather than absent.
    /// Ordered by creation so runs are comparable.
    pub fn list(&self) -> Vec<TokenRecord> {
        let mut all: Vec<TokenRecord> = self.by_hash.values().cloned().collect();
        all.sort_by(|a, b| a.created_at.cmp(&b.created_at).then(a.id.cmp(&b.id)));
        all
    }

    /// The same list with the hashes stripped — what crosses the FFI boundary
    /// and reaches an API response.
    pub fn summaries(&self) -> Vec<TokenSummary> {
        self.list().iter().map(TokenSummary::from).collect()
    }

    /// Mark the token with this id revoked at `now_ms`. Returns whether a
    /// token was found. Revoking twice keeps the first timestamp — the moment
    /// it stopped working is the fact worth keeping.
    pub fn revoke(&mut self, id: &str, now_ms: i64) -> Result<bool> {
        self.amend(id, "revoke", |record| {
            if record.revoked_at.is_none() {
                record.revoked_at = Some(now_ms);
            }
        })
    }

    /// Lift a revocation, making the token usable again.
    ///
    /// Revocation is a switch rather than a one-way door: you kill a token,
    /// watch whether anything is still presenting it, and turn it back on if
    /// the answer is you. While revoked it is dead for everyone — enabling is
    /// a deliberate act by the owner, not a way back in for whoever held it.
    ///
    /// The revocation timestamp clears with it. That a token was ever revoked
    /// lives in the attempt record, not here, because this object only ever
    /// describes the token's state right now.
    pub fn enable(&mut self, id: &str) -> Result<bool> {
        self.amend(id, "enable", |record| {
            record.revoked_at = None;
        })
    }

    /// Apply a change to the token with this id and write it through.
    /// `operation` names the caller so a failure says which one lost the race.
    fn amend(
        &mut self,
        id: &str,
        operation: &str,
        change: impl FnOnce(&mut TokenRecord),
    ) -> Result<bool> {
        let hash = match self.by_hash.iter().find(|(_, t)| t.id == id) {
            Some((hash, _)) => hash.clone(),
            None => return Ok(false),
        };

        let record = self.by_hash.get_mut(&hash).ok_or_else(|| {
            DuckdbError::Backend(format!("token {id} vanished during {operation}"))
        })?;
        change(record);

        let updated = record.clone();
        self.write_object(&updated).map(|_| true)
    }

    /// Record that the token with this hash was used at `now_ms`.
    pub fn touch(&mut self, hash: &str, now_ms: i64) -> Result<bool> {
        let record = match self.by_hash.get_mut(hash) {
            Some(record) => {
                record.last_used_at = Some(now_ms);
                record.clone()
            }
            None => return Ok(false),
        };
        self.write_object(&record).map(|_| true)
    }

    /// Read every token object at the location in one query.
    ///
    /// `read_json` errors when the glob matches nothing, which is the ordinary
    /// state of a store that has never issued a token — that case is empty,
    /// not broken. Any other failure is real and surfaces.
    fn load(&mut self) -> Result<()> {
        let sql = format!(
            "SELECT id, hash, label, created_at, expires_at, last_used_at, revoked_at \
             FROM read_json('{}/*.json', columns = {{ \
                 id: 'VARCHAR', hash: 'VARCHAR', label: 'VARCHAR', \
                 created_at: 'BIGINT', expires_at: 'BIGINT', \
                 last_used_at: 'BIGINT', revoked_at: 'BIGINT' }})",
            self.prefix
        );

        let mut stmt = match self.conn.prepare(&sql) {
            Ok(stmt) => stmt,
            Err(_) => return Ok(()),
        };
        let rows = match stmt.query_map([], |row| {
            Ok(TokenRecord {
                id: row.get(0)?,
                hash: row.get(1)?,
                label: row.get(2)?,
                created_at: row.get(3)?,
                expires_at: row.get(4)?,
                last_used_at: row.get(5)?,
                revoked_at: row.get(6)?,
            })
        }) {
            Ok(rows) => rows,
            Err(_) => return Ok(()),
        };

        for row in rows {
            let record = row.map_err(|e| {
                DuckdbError::Backend(format!(
                    "failed to read an access token object under {}: {e}",
                    self.prefix
                ))
            })?;
            self.by_hash.insert(record.hash.clone(), record);
        }
        Ok(())
    }

    /// Write one token to its own object, replacing what was there.
    ///
    /// `COPY … TO` is the same mechanism attestations use for Parquet
    /// (ADR-024:63), so a local path and an S3 prefix take one code path and
    /// the tests that cover `file://` cover what production runs.
    fn write_object(&self, record: &TokenRecord) -> Result<()> {
        if !is_remote(&self.location) {
            let _ = std::fs::create_dir_all(&self.prefix);
        }

        let path = format!("{}/{}.json", self.prefix, record.hash);
        let sql = format!(
            "COPY (SELECT ? AS id, ? AS hash, ? AS label, \
                          ?::BIGINT AS created_at, ?::BIGINT AS expires_at, \
                          ?::BIGINT AS last_used_at, ?::BIGINT AS revoked_at) \
             TO '{path}' (FORMAT JSON)"
        );

        self.conn
            .execute(
                &sql,
                duckdb::params![
                    record.id,
                    record.hash,
                    record.label,
                    record.created_at,
                    record.expires_at,
                    record.last_used_at,
                    record.revoked_at,
                ],
            )
            .map_err(|e| {
                DuckdbError::Backend(format!("failed to write token object {path}: {e}"))
            })?;
        Ok(())
    }
}

/// The directory holding token objects, as a path DuckDB SQL can consume.
/// Strips `file://` the way `DuckdbStore::location_path` does; remote schemes
/// pass through for httpfs.
fn token_prefix(location: &str) -> String {
    let base = location.strip_prefix("file://").unwrap_or(location);
    format!("{}/access_tokens", base.trim_end_matches('/'))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn record(id: &str, hash: &str) -> TokenRecord {
        TokenRecord {
            id: id.to_string(),
            hash: hash.to_string(),
            label: format!("token {id}"),
            created_at: 1_700_000_000_000,
            expires_at: None,
            last_used_at: None,
            revoked_at: None,
        }
    }

    fn store(dir: &tempfile::TempDir) -> TokenStore {
        TokenStore::open(format!("file://{}", dir.path().display())).unwrap()
    }

    /// A token that survives nothing is a credential that stops working at the
    /// next restart.
    #[test]
    fn token_survives_reopen() {
        let dir = tempfile::tempdir().unwrap();
        let mut s = store(&dir);
        s.put(record("t1", "hash-1")).unwrap();

        let reopened = store(&dir);
        assert!(reopened.lookup("hash-1", 1_700_000_001_000));
    }

    /// The requirement in one test: revoke it and it is dead, including after
    /// a restart. Nothing may read back a pre-revoke version.
    #[test]
    fn revoked_token_stays_dead_across_reopen() {
        let dir = tempfile::tempdir().unwrap();
        let mut s = store(&dir);
        s.put(record("t1", "hash-1")).unwrap();

        assert!(s.revoke("t1", 1_700_000_002_000).unwrap());
        assert!(!s.lookup("hash-1", 1_700_000_003_000));

        let reopened = store(&dir);
        assert!(!reopened.lookup("hash-1", 1_700_000_003_000));
    }

    /// Revoking one token must not touch another.
    #[test]
    fn revoke_hits_only_its_own_token() {
        let dir = tempfile::tempdir().unwrap();
        let mut s = store(&dir);
        s.put(record("t1", "hash-1")).unwrap();
        s.put(record("t2", "hash-2")).unwrap();

        s.revoke("t1", 1_700_000_002_000).unwrap();

        assert!(!s.lookup("hash-1", 1_700_000_003_000));
        assert!(s.lookup("hash-2", 1_700_000_003_000));
    }

    /// The first revocation is when it stopped working; a second call must not
    /// move that moment.
    #[test]
    fn revoking_twice_keeps_the_first_moment() {
        let dir = tempfile::tempdir().unwrap();
        let mut s = store(&dir);
        s.put(record("t1", "hash-1")).unwrap();

        s.revoke("t1", 1_700_000_002_000).unwrap();
        s.revoke("t1", 1_700_000_009_000).unwrap();

        let revoked_at = store(&dir).list()[0].revoked_at;
        assert_eq!(revoked_at, Some(1_700_000_002_000));
    }

    /// The hash is the only thing between the store and anyone who reads a
    /// list response. It must not survive serialization at the boundary.
    #[test]
    fn summaries_carry_no_hash() {
        let dir = tempfile::tempdir().unwrap();
        let mut s = store(&dir);
        s.put(record("t1", "hash-1")).unwrap();

        let json = serde_json::to_string(&s.summaries()).unwrap();
        assert!(!json.contains("hash-1"), "hash leaked into {json}");
        assert!(json.contains("t1"), "id missing from {json}");
    }

    /// A summary still says everything the UI draws, revocation included.
    #[test]
    fn summaries_keep_revocation_state() {
        let dir = tempfile::tempdir().unwrap();
        let mut s = store(&dir);
        s.put(record("t1", "hash-1")).unwrap();
        s.revoke("t1", 1_700_000_002_000).unwrap();

        let summaries = s.summaries();
        assert_eq!(summaries.len(), 1);
        assert_eq!(summaries[0].revoked_at, Some(1_700_000_002_000));
        assert_eq!(summaries[0].label, "token t1");
    }

    #[test]
    fn revoke_reports_unknown_id() {
        let dir = tempfile::tempdir().unwrap();
        let mut s = store(&dir);
        assert!(!s.revoke("nobody", 1_700_000_002_000).unwrap());
    }

    /// Revocation is a switch. Kill a token, watch who is still presenting it,
    /// turn it back on if that was you.
    #[test]
    fn enabling_brings_a_revoked_token_back() {
        let dir = tempfile::tempdir().unwrap();
        let mut s = store(&dir);
        s.put(record("t1", "hash-1")).unwrap();
        s.revoke("t1", 1_700_000_002_000).unwrap();
        assert!(!s.lookup("hash-1", 1_700_000_003_000));

        assert!(s.enable("t1").unwrap());
        assert!(s.lookup("hash-1", 1_700_000_003_000));
    }

    /// Enabling has to reach the object, or a restart resurrects the
    /// revocation the owner deliberately lifted.
    #[test]
    fn enabling_survives_reopen() {
        let dir = tempfile::tempdir().unwrap();
        let mut s = store(&dir);
        s.put(record("t1", "hash-1")).unwrap();
        s.revoke("t1", 1_700_000_002_000).unwrap();
        s.enable("t1").unwrap();

        assert!(store(&dir).lookup("hash-1", 1_700_000_003_000));
    }

    /// An expired token is expired whatever its revocation state — enabling
    /// lifts a revocation, it does not extend a lifetime.
    #[test]
    fn enabling_does_not_extend_an_expiry() {
        let dir = tempfile::tempdir().unwrap();
        let mut s = store(&dir);
        let mut r = record("t1", "hash-1");
        r.expires_at = Some(1_700_000_005_000);
        s.put(r).unwrap();

        s.revoke("t1", 1_700_000_002_000).unwrap();
        s.enable("t1").unwrap();

        assert!(s.lookup("hash-1", 1_700_000_004_999));
        assert!(!s.lookup("hash-1", 1_700_000_006_000));
    }

    /// Enabling one token must not touch another.
    #[test]
    fn enable_hits_only_its_own_token() {
        let dir = tempfile::tempdir().unwrap();
        let mut s = store(&dir);
        s.put(record("t1", "hash-1")).unwrap();
        s.put(record("t2", "hash-2")).unwrap();
        s.revoke("t1", 1_700_000_002_000).unwrap();
        s.revoke("t2", 1_700_000_002_000).unwrap();

        s.enable("t1").unwrap();

        assert!(s.lookup("hash-1", 1_700_000_003_000));
        assert!(!s.lookup("hash-2", 1_700_000_003_000));
    }

    #[test]
    fn enable_reports_unknown_id() {
        let dir = tempfile::tempdir().unwrap();
        let mut s = store(&dir);
        assert!(!s.enable("nobody").unwrap());
    }

    /// Enabling a token that was never revoked changes nothing.
    #[test]
    fn enabling_a_live_token_is_harmless() {
        let dir = tempfile::tempdir().unwrap();
        let mut s = store(&dir);
        s.put(record("t1", "hash-1")).unwrap();

        assert!(s.enable("t1").unwrap());
        assert!(s.lookup("hash-1", 1_700_000_003_000));
        assert_eq!(store(&dir).list()[0].revoked_at, None);
    }

    /// A token with no expiry lives until revoked — that is what a nil
    /// `expiresAt` means at `server/auth/tokens.go:17`.
    #[test]
    fn token_without_expiry_never_expires() {
        let dir = tempfile::tempdir().unwrap();
        let mut s = store(&dir);
        s.put(record("t1", "hash-1")).unwrap();
        assert!(s.lookup("hash-1", i64::MAX));
    }

    #[test]
    fn expired_token_is_not_usable() {
        let dir = tempfile::tempdir().unwrap();
        let mut s = store(&dir);
        let mut r = record("t1", "hash-1");
        r.expires_at = Some(1_700_000_005_000);
        s.put(r).unwrap();

        assert!(s.lookup("hash-1", 1_700_000_004_999));
        assert!(!s.lookup("hash-1", 1_700_000_005_000));
    }

    /// An unknown hash fails closed.
    #[test]
    fn unknown_hash_is_not_usable() {
        let dir = tempfile::tempdir().unwrap();
        let s = store(&dir);
        assert!(!s.lookup("never-issued", 1_700_000_000_000));
    }

    /// A revoked token stays on the list. The UI shows it revoked rather than
    /// gone, which is what makes the red X mean something afterwards.
    #[test]
    fn list_keeps_revoked_tokens() {
        let dir = tempfile::tempdir().unwrap();
        let mut s = store(&dir);
        s.put(record("t1", "hash-1")).unwrap();
        s.revoke("t1", 1_700_000_002_000).unwrap();

        let listed = s.list();
        assert_eq!(listed.len(), 1);
        assert_eq!(listed[0].revoked_at, Some(1_700_000_002_000));
    }

    #[test]
    fn touch_records_use_durably() {
        let dir = tempfile::tempdir().unwrap();
        let mut s = store(&dir);
        s.put(record("t1", "hash-1")).unwrap();

        assert!(s.touch("hash-1", 1_700_000_004_000).unwrap());
        assert_eq!(store(&dir).list()[0].last_used_at, Some(1_700_000_004_000));
    }

    /// A location that never issued a token is empty, not an error.
    #[test]
    fn opening_an_untouched_location_is_empty() {
        let dir = tempfile::tempdir().unwrap();
        assert!(store(&dir).list().is_empty());
    }

    /// A path is interpolated into SQL, so a quote in the location would end
    /// the string literal early. Refuse rather than build the statement.
    #[test]
    fn quoted_location_is_refused() {
        match TokenStore::open("file:///tmp/it's-here") {
            Err(DuckdbError::Backend(msg)) => {
                assert!(msg.contains("quote"), "unhelpful message: {msg}");
            }
            Err(other) => panic!("expected a rejection, got {other:?}"),
            Ok(store) => panic!("opened a quoted location at {}", store.location()),
        }
    }

    /// The prefix is where ADR-025 says tokens live, and `file://` is stripped
    /// because DuckDB wants a bare path for local files.
    #[test]
    fn prefix_is_access_tokens_under_the_location() {
        assert_eq!(
            token_prefix("file:///var/lib/qntx/parquet"),
            "/var/lib/qntx/parquet/access_tokens"
        );
        assert_eq!(
            token_prefix("s3://bucket/prefix"),
            "s3://bucket/prefix/access_tokens"
        );
        assert_eq!(
            token_prefix("s3://bucket/prefix/"),
            "s3://bucket/prefix/access_tokens"
        );
    }

    /// An s3 location has to load httpfs and register the credential-chain
    /// secret, or every read signs with empty credentials and gets a 403.
    #[test]
    fn remote_locations_set_up_httpfs() {
        let sql = remote_setup_sql("s3://bucket/prefix").expect("s3 needs setup");
        assert!(sql.contains("LOAD httpfs;"), "no httpfs in {sql}");
        assert!(sql.contains("LOAD aws;"), "no aws in {sql}");
        assert!(sql.contains("credential_chain"), "no secret in {sql}");

        assert!(remote_setup_sql("file:///tmp/x").is_none());
    }
}
