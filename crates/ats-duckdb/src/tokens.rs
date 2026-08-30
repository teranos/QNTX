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
use crate::{is_remote, nothing_matched, remote_setup_sql};

/// Where a token may act.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, Default)]
#[serde(transparent)]
pub struct Namespaces(pub Vec<String>);

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
    /// The token's own `did:key`, derived from the seed the raw token is.
    #[serde(default)]
    pub did: String,
    /// The `root_identities` entry whose session minted this token.
    #[serde(default)]
    pub minted_by: String,

    /// Who that entry reaches (ADR-031), resolved when the token was minted.
    /// A route is a way in; this is the person the token speaks for.
    #[serde(default)]
    pub minted_by_user: String,

    /// What that person calls themselves, for reading in a list.
    #[serde(default)]
    pub minted_by_display_name: String,
    /// Which kind of token this is, chosen at minting.
    #[serde(default)]
    pub level: String,
    /// Where the token may act. Resolving the token discovers this, so the
    /// record does not live under the namespaces it names.
    #[serde(default)]
    pub namespaces: Namespaces,
    /// Predicates this token may read. Empty is none, not all.
    #[serde(default)]
    pub scope_read: Vec<String>,
    /// Predicates this token may write. Empty is none, not all.
    #[serde(default)]
    pub scope_write: Vec<String>,
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
    /// Safe to publish — a DID is a public key, and naming it is what lets a
    /// signature made by this token be traced back to it.
    pub did: String,
    pub minted_by: String,

    /// Who the token speaks for, resolved at minting rather than on use.
    #[serde(default)]
    pub minted_by_user: String,

    #[serde(default)]
    pub minted_by_display_name: String,

    /// Which kind of token this is, so a list can say which it is looking at.
    #[serde(default)]
    pub level: String,

    #[serde(default)]
    pub namespaces: Namespaces,
    pub scope_read: Vec<String>,
    pub scope_write: Vec<String>,
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
            did: record.did.clone(),
            minted_by: record.minted_by.clone(),
            minted_by_user: record.minted_by_user.clone(),
            minted_by_display_name: record.minted_by_display_name.clone(),
            level: record.level.clone(),
            namespaces: record.namespaces.clone(),
            scope_read: record.scope_read.clone(),
            scope_write: record.scope_write.clone(),
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
        self.resolve(hash, now_ms).is_some()
    }

    /// The token this hash names, when it authorizes a request at `now_ms`.
    /// Carries the namespace, scope and minter the middleware routes on.
    pub fn resolve(&self, hash: &str, now_ms: i64) -> Option<&TokenRecord> {
        self.by_hash.get(hash).filter(|t| t.is_usable(now_ms))
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

    /// Replace what this token may read and write (TOKATTEST).
    ///
    /// Both lists are given together because they are one answer to what a
    /// token may touch — setting one and leaving the other is a state nobody
    /// asked for. Empty is none, as it is everywhere else here.
    pub fn set_scope(&mut self, id: &str, read: &[String], write: &[String]) -> Result<bool> {
        self.amend(id, "set scope", |record| {
            record.scope_read = read.to_vec();
            record.scope_write = write.to_vec();
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

        let current = self.by_hash.get(&hash).ok_or_else(|| {
            DuckdbError::Backend(format!("token {id} vanished during {operation}"))
        })?;

        // resolve() authenticates from by_hash, so changing it before the write
        // lands makes a failed revoke report failure and revoke anyway — and a
        // failed enable report failure and enable. Durable first, then in hand.
        let mut updated = current.clone();
        change(&mut updated);
        self.write_object(&updated)?;

        self.by_hash.insert(hash, updated);
        Ok(true)
    }

    /// Record that the token with this hash was used at `now_ms`.
    pub fn touch(&mut self, hash: &str, now_ms: i64) -> Result<bool> {
        let mut record = match self.by_hash.get(hash) {
            Some(record) => record.clone(),
            None => return Ok(false),
        };
        record.last_used_at = Some(now_ms);
        self.write_object(&record)?;

        self.by_hash.insert(hash.to_string(), record);
        Ok(true)
    }

    /// Read every token object at the location in one query.
    ///
    /// `read_json` errors when the glob matches nothing, which is the ordinary
    /// state of a store that has never issued a token — that case is empty,
    /// not broken. Any other failure is real and surfaces.
    fn load(&mut self) -> Result<()> {
        let sql = format!(
            "SELECT id, hash, label, did, minted_by, namespace, \
                    to_json(scope_read), to_json(scope_write), \
                    created_at, expires_at, last_used_at, revoked_at, \
                    minted_by_user, minted_by_display_name, level \
             FROM read_json('{}/*.json', columns = {{ \
                 id: 'VARCHAR', hash: 'VARCHAR', label: 'VARCHAR', \
                 did: 'VARCHAR', minted_by: 'VARCHAR', namespace: 'VARCHAR', \
                 scope_read: 'VARCHAR[]', scope_write: 'VARCHAR[]', \
                 created_at: 'BIGINT', expires_at: 'BIGINT', \
                 last_used_at: 'BIGINT', revoked_at: 'BIGINT', \
                 minted_by_user: 'VARCHAR', minted_by_display_name: 'VARCHAR', \
                 level: 'VARCHAR' }})",
            self.prefix
        );

        // read_json lists the glob while the statement is being prepared, so
        // an empty store is refused here rather than at query time.
        let mut stmt = match self.conn.prepare(&sql) {
            Ok(stmt) => stmt,
            Err(e) if nothing_matched(&e) => return Ok(()),
            Err(_) => crate::prepare_fresh(
                &self.conn,
                &self.location,
                &sql,
                &format!(
                    "failed to prepare the read of the access tokens under {}",
                    self.prefix
                ),
            )?,
        };
        let rows = match stmt.query_map([], |row| {
            Ok(TokenRecord {
                id: row.get(0)?,
                hash: row.get(1)?,
                label: row.get(2)?,
                did: row.get(3)?,
                minted_by: row.get(4)?,
                namespaces: Namespaces(scope_from_json(row.get::<_, String>(5)?)),
                scope_read: scope_from_json(row.get::<_, String>(6)?),
                scope_write: scope_from_json(row.get::<_, String>(7)?),
                created_at: row.get(8)?,
                expires_at: row.get(9)?,
                last_used_at: row.get(10)?,
                revoked_at: row.get(11)?,
                // A token minted before the node wrote down who minted it has
                // no user on its object, and the column reads Null. Empty is
                // devoid: the token still says what it may do, and says
                // nothing about the person. Demanding a value here is a node
                // that will not start over a token it can read perfectly well.
                minted_by_user: row.get::<_, Option<String>>(12)?.unwrap_or_default(),
                minted_by_display_name: row.get::<_, Option<String>>(13)?.unwrap_or_default(),
                // Same object, same reason: the kind is chosen at minting, and
                // a token minted before there were kinds recorded none. Empty
                // is not SUPER — Grant.Scoped reads anything that is not SUPER
                // as scoped, so an unnamed kind keeps the scopes it was given
                // rather than inheriting the one that has none.
                level: row.get::<_, Option<String>>(14)?.unwrap_or_default(),
            })
        }) {
            Ok(rows) => rows,
            Err(e) if nothing_matched(&e) => return Ok(()),
            Err(e) => {
                return Err(DuckdbError::Backend(format!(
                    "failed to read the access tokens under {}: {e}",
                    self.prefix
                )))
            }
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
            std::fs::create_dir_all(&self.prefix)?;
        }

        let path = format!("{}/{}.json", self.prefix, record.hash);
        // The scopes go out as real JSON arrays rather than quoted strings, so
        // the object on disk reads the way a person would write it.
        let sql = format!(
            "COPY (SELECT ? AS id, ? AS hash, ? AS label, \
                          ? AS did, ? AS minted_by, ? AS namespace, \
                          from_json(?, '[\"VARCHAR\"]') AS scope_read, \
                          from_json(?, '[\"VARCHAR\"]') AS scope_write, \
                          ?::BIGINT AS created_at, ?::BIGINT AS expires_at, \
                          ?::BIGINT AS last_used_at, ?::BIGINT AS revoked_at, \
                          ? AS level, ? AS minted_by_user, \
                          ? AS minted_by_display_name) \
             TO '{path}' (FORMAT JSON)"
        );

        self.conn
            .execute(
                &sql,
                duckdb::params![
                    record.id,
                    record.hash,
                    record.label,
                    record.did,
                    record.minted_by,
                    scope_to_json(&record.namespaces.0),
                    scope_to_json(&record.scope_read),
                    scope_to_json(&record.scope_write),
                    record.created_at,
                    record.expires_at,
                    record.last_used_at,
                    record.revoked_at,
                    record.level,
                    record.minted_by_user,
                    record.minted_by_display_name,
                ],
            )
            .map_err(|e| {
                DuckdbError::Backend(format!("failed to write token object {path}: {e}"))
            })?;
        Ok(())
    }
}

/// A scope crosses SQL as JSON text. An unreadable one is an empty scope,
/// which grants nothing — the direction a corrupt record has to fail in.
fn scope_from_json(raw: String) -> Vec<String> {
    serde_json::from_str(&raw).unwrap_or_default()
}

fn scope_to_json(scope: &[String]) -> String {
    serde_json::to_string(scope).unwrap_or_else(|_| "[]".to_string())
}

/// Token objects sit beside `system/node_identity/`: storing them under the
/// namespace they authorize would make authentication enumerate namespaces,
/// because a bearer names none until it has been resolved.
fn token_prefix(location: &str) -> String {
    crate::namespace::prefix(location, crate::namespace::SYSTEM, "access_tokens")
}

#[cfg(test)]
mod tests {
    use super::*;

    fn record(id: &str, hash: &str) -> TokenRecord {
        TokenRecord {
            id: id.to_string(),
            hash: hash.to_string(),
            label: format!("token {id}"),
            did: format!("did:key:z{id}"),
            minted_by: "https://mastodon.example/@tim".to_string(),
            minted_by_user: "US-TIM-7K4M3B9X".to_string(),
            minted_by_display_name: "tim".to_string(),
            level: ATTESTOR.to_string(),
            namespaces: Namespaces(vec![NS.to_string()]),
            scope_read: vec!["reads".to_string()],
            scope_write: vec!["writes".to_string()],
            created_at: 1_700_000_000_000,
            expires_at: None,
            last_used_at: None,
            revoked_at: None,
        }
    }

    const NS: &str = "did:key:ztestnamespace";

    /// A kind, to show one reaching the object and coming back. This crate
    /// carries the level and never reads it: what a kind means lives in
    /// `server/auth/admission.go`.
    const ATTESTOR: &str = "ATTESTOR";

    fn store(dir: &tempfile::TempDir) -> TokenStore {
        TokenStore::open(format!("file://{}", dir.path().display())).unwrap()
    }

    // Resolution happens before a namespace is known, so the objects sit in
    // system and the record carries the namespace it authorizes.
    #[test]
    fn tokens_land_in_the_system_namespace() {
        let dir = tempfile::tempdir().expect("tempdir");
        let mut store = self::store(&dir);
        store.put(record("id", "hash")).expect("put");

        assert!(dir.path().join("system").join("access_tokens").exists());
        assert!(!dir.path().join(NS).exists());
    }

    // A token object written before the node recorded who minted it, and
    // before a token had a kind, is still a token. The columns come back Null
    // and the node has to start on them: refusing here is a node that will not
    // boot over records it can read perfectly well, and every restart reads
    // the same object and dies the same way.
    #[test]
    fn a_token_object_missing_the_later_fields_still_loads() {
        let dir = tempfile::tempdir().unwrap();
        let mut writer = self::store(&dir);
        writer.put(record("AT-OLD", "oldhash")).expect("put");

        // Take the three keys back off the object, which is what one written
        // before they existed looks like. Doing it this way rather than by
        // hand keeps the rest of the object exactly as this crate writes it.
        let path = dir
            .path()
            .join("system")
            .join("access_tokens")
            .join("oldhash.json");
        let raw = std::fs::read_to_string(&path).unwrap();
        let mut object: serde_json::Value = serde_json::from_str(&raw).unwrap();
        let fields = object.as_object_mut().unwrap();
        fields.remove("minted_by_user");
        fields.remove("minted_by_display_name");
        fields.remove("level");
        std::fs::write(&path, serde_json::to_string(&object).unwrap()).unwrap();

        let store = self::store(&dir);
        let found = store
            .resolve("oldhash", 1_700_000_000_001)
            .expect("the old token did not load");

        assert_eq!(found.id, "AT-OLD");
        assert_eq!(found.scope_read, vec!["reads".to_string()]);
        // Empty is devoid. The token says what it may do and says nothing
        // about the person, because nothing about the person was written.
        assert_eq!(found.minted_by_user, "");
        assert_eq!(found.minted_by_display_name, "");
        assert_eq!(found.level, "");
    }

    // A token that resolves to nothing but true tells the middleware which
    // namespace to route to and which predicates to allow — nothing at all.
    #[test]
    fn resolving_carries_the_namespace_and_scope() {
        let dir = tempfile::tempdir().unwrap();
        let mut s = store(&dir);
        s.put(record("t1", "hash-1")).unwrap();

        let found = store(&dir)
            .resolve("hash-1", 1_700_000_001_000)
            .cloned()
            .expect("token resolves");
        assert_eq!(found.namespaces.0, vec![NS.to_string()]);
        assert_eq!(found.minted_by, "https://mastodon.example/@tim");
        assert_eq!(found.did, "did:key:zt1");
        assert_eq!(found.scope_read, vec!["reads".to_string()]);
        assert_eq!(found.scope_write, vec!["writes".to_string()]);
    }

    // A revoked token resolves to nothing, so scope cannot be read off it.
    #[test]
    fn a_revoked_token_resolves_to_nothing() {
        let dir = tempfile::tempdir().unwrap();
        let mut s = store(&dir);
        s.put(record("t1", "hash-1")).unwrap();
        s.revoke("t1", 1_700_000_002_000).unwrap();

        assert!(s.resolve("hash-1", 1_700_000_003_000).is_none());
    }

    // An empty scope grants nothing. A token issued without one must not read
    // as unrestricted, which is the direction this has to fail in.
    #[test]
    fn an_absent_scope_survives_as_empty() {
        let dir = tempfile::tempdir().unwrap();
        let mut s = store(&dir);
        let mut r = record("t1", "hash-1");
        r.scope_read = vec![];
        r.scope_write = vec![];
        s.put(r).unwrap();

        let found = store(&dir)
            .resolve("hash-1", 1_700_000_001_000)
            .cloned()
            .unwrap();
        assert!(found.scope_read.is_empty());
        assert!(found.scope_write.is_empty());
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

    /// Which kind a token is decides what it may do, so a kind that does not
    /// reach the object is a token that comes back as something else.
    #[test]
    fn the_kind_survives_reopen() {
        let dir = tempfile::tempdir().unwrap();
        let mut s = store(&dir);
        s.put(record("t1", "hash-1")).unwrap();

        let reopened = store(&dir);
        let found = reopened.resolve("hash-1", 1_700_000_001_000).unwrap();
        assert_eq!(found.level, ATTESTOR);
        // These two went the same way: written into the record, never into the
        // object, and read back empty on every restart.
        assert_eq!(found.minted_by_user, "US-TIM-7K4M3B9X");
        assert_eq!(found.minted_by_display_name, "tim");
    }

    /// A store that cannot be read must not answer the same as one holding no
    /// tokens — that makes every token stop working with nothing saying why.
    #[test]
    fn an_unreadable_location_is_an_error_not_an_empty_store() {
        assert!(TokenStore::open("s3://qntx-no-such-bucket-here/attestations").is_err());
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

    /// resolve() authenticates from memory, so a write that fails must leave
    /// memory alone. Otherwise revoke answers "failed" and revokes anyway.
    #[test]
    fn a_failed_write_changes_nothing() {
        let dir = tempfile::tempdir().unwrap();
        let mut s = store(&dir);
        s.put(record("t1", "hash-1")).unwrap();
        assert!(s.lookup("hash-1", 1_700_000_001_000));

        // A file where the prefix directory belongs: create_dir_all cannot make
        // it and COPY cannot write under it, which is a park gone read-only.
        std::fs::remove_dir_all(&s.prefix).unwrap();
        std::fs::write(&s.prefix, b"not a directory").unwrap();

        assert!(s.revoke("t1", 1_700_000_002_000).is_err());
        assert!(
            s.lookup("hash-1", 1_700_000_003_000),
            "the revoke failed and revoked it anyway"
        );
    }

    /// The mirror: an enable that could not be written must not enable.
    #[test]
    fn a_failed_enable_leaves_it_revoked() {
        let dir = tempfile::tempdir().unwrap();
        let mut s = store(&dir);
        s.put(record("t1", "hash-1")).unwrap();
        s.revoke("t1", 1_700_000_002_000).unwrap();

        std::fs::remove_dir_all(&s.prefix).unwrap();
        std::fs::write(&s.prefix, b"not a directory").unwrap();

        assert!(s.enable("t1").is_err());
        assert!(
            !s.lookup("hash-1", 1_700_000_003_000),
            "the enable failed and enabled it anyway"
        );
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

    /// TOKATTEST: what a token may touch is changed on the token it already is,
    /// rather than by minting a second one and retiring the first.
    #[test]
    fn scope_is_changed_on_the_token_that_holds_it() {
        let dir = tempfile::tempdir().unwrap();
        let mut s = store(&dir);
        s.put(record("t1", "hash-1")).unwrap();

        assert!(s
            .set_scope("t1", &["deploy".to_string()], &["deploy".to_string()])
            .unwrap());

        let resolved = s.resolve("hash-1", 1_700_000_003_000).unwrap();
        assert_eq!(resolved.scope_read, vec!["deploy".to_string()]);
        assert_eq!(resolved.scope_write, vec!["deploy".to_string()]);
    }

    /// A scope that only lived in memory is a permission a restart hands back.
    #[test]
    fn a_changed_scope_survives_reopen() {
        let dir = tempfile::tempdir().unwrap();
        let mut s = store(&dir);
        s.put(record("t1", "hash-1")).unwrap();
        s.set_scope("t1", &["deploy".to_string()], &[]).unwrap();

        let reopened = store(&dir);
        let resolved = reopened.resolve("hash-1", 1_700_000_003_000).unwrap();
        assert_eq!(resolved.scope_read, vec!["deploy".to_string()]);
        assert!(resolved.scope_write.is_empty());
    }

    /// Naming no token changed nothing, and saying otherwise is a permission
    /// the caller believes they set.
    #[test]
    fn set_scope_reports_unknown_id() {
        let dir = tempfile::tempdir().unwrap();
        let mut s = store(&dir);
        s.put(record("t1", "hash-1")).unwrap();

        assert!(!s.set_scope("nobody", &["deploy".to_string()], &[]).unwrap());
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

    /// Tokens live under their namespace, and `file://` is stripped because
    /// DuckDB wants a bare path for local files.
    #[test]
    fn prefix_is_access_tokens_under_system() {
        assert_eq!(
            token_prefix("file:///var/lib/qntx/parquet"),
            "/var/lib/qntx/parquet/system/access_tokens"
        );
        assert_eq!(
            token_prefix("s3://bucket/prefix"),
            "s3://bucket/prefix/system/access_tokens"
        );
        assert_eq!(
            token_prefix("s3://bucket/prefix/"),
            "s3://bucket/prefix/system/access_tokens"
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
