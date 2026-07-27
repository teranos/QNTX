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
use std::path::PathBuf;

use serde::{Deserialize, Serialize};

use crate::error::{DuckdbError, Result};

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
    by_hash: HashMap<String, TokenRecord>,
}

impl TokenStore {
    /// Open the store at `location`, loading every token already there.
    pub fn open(location: impl Into<String>) -> Result<Self> {
        let location = location.into();
        let mut store = Self {
            location,
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

    /// Read every token object at the location. A location with no
    /// `access_tokens` prefix is a store that has never issued one.
    fn load(&mut self) -> Result<()> {
        let dir = self.prefix()?;
        let entries = match std::fs::read_dir(&dir) {
            Ok(entries) => entries,
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(()),
            Err(e) => {
                return Err(DuckdbError::Backend(format!(
                    "failed to read access tokens from {}: {e}",
                    dir.display()
                )))
            }
        };

        for entry in entries {
            let path = entry
                .map_err(|e| {
                    DuckdbError::Backend(format!(
                        "failed to read a directory entry under {}: {e}",
                        dir.display()
                    ))
                })?
                .path();
            if path.extension().and_then(|e| e.to_str()) != Some("json") {
                continue;
            }
            let body = std::fs::read(&path).map_err(|e| {
                DuckdbError::Backend(format!(
                    "failed to read token object {}: {e}",
                    path.display()
                ))
            })?;
            let record: TokenRecord = serde_json::from_slice(&body).map_err(|e| {
                DuckdbError::Backend(format!(
                    "failed to parse token object {}: {e}",
                    path.display()
                ))
            })?;
            self.by_hash.insert(record.hash.clone(), record);
        }
        Ok(())
    }

    /// Write one token to its own object, replacing what was there.
    fn write_object(&self, record: &TokenRecord) -> Result<()> {
        let dir = self.prefix()?;
        std::fs::create_dir_all(&dir).map_err(|e| {
            DuckdbError::Backend(format!(
                "failed to create access token prefix {}: {e}",
                dir.display()
            ))
        })?;

        let path = dir.join(format!("{}.json", record.hash));
        let body = serde_json::to_vec(record)?;
        std::fs::write(&path, body).map_err(|e| {
            DuckdbError::Backend(format!(
                "failed to write token object {}: {e}",
                path.display()
            ))
        })
    }

    /// The directory holding token objects.
    ///
    /// `s3://` is the production target (ADR-024) and is not implemented here.
    /// It fails loudly rather than writing somewhere else, because a token
    /// store that silently persists nowhere hands out credentials that stop
    /// working at the next restart.
    fn prefix(&self) -> Result<PathBuf> {
        if self.location.starts_with("s3://")
            || self.location.starts_with("http://")
            || self.location.starts_with("https://")
        {
            return Err(DuckdbError::Backend(format!(
                "access tokens at {} are not implemented — only file:// and local paths are \
                 supported so far; remote locations need DuckDB httpfs, see ADR-024",
                self.location
            )));
        }
        let base = self
            .location
            .strip_prefix("file://")
            .unwrap_or(&self.location);
        Ok(PathBuf::from(base).join("access_tokens"))
    }
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

    /// s3:// is the production target and is not built yet. Failing loudly
    /// beats writing tokens somewhere they will not be found again.
    #[test]
    fn remote_location_fails_loudly() {
        match TokenStore::open("s3://bucket/prefix") {
            Err(DuckdbError::Backend(msg)) => {
                assert!(msg.contains("not implemented"), "unhelpful message: {msg}");
            }
            Err(other) => panic!("expected an unimplemented error, got {other:?}"),
            Ok(store) => panic!("opened a remote location at {}", store.location()),
        }
    }
}
