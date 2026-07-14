//! DuckDB-backed attestation store.
//!
//! Peer of `qntx_sqlite::SqliteStore`. Implements the storage traits from
//! `qntx_core::storage`. See ADR-024 for the design.
//!
//! Current scope: in-memory DuckDB for the trait impl. Parquet flush to
//! `location` is a follow-up commit — the `Location` is stored but not
//! yet used for durability.

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

// qntx-core's storage::error module isn't public, but AttestationStore's trait
// methods return StoreResult<T>. Alias it here to match qntx-sqlite's pattern
// (crates/qntx-sqlite/src/store.rs).
type StoreResult<T> = std::result::Result<T, StoreError>;
use std::collections::HashMap;

/// Convert a Vec<String> to a DuckDB LIST<VARCHAR> parameter.
/// Vec<String> doesn't impl ToSql directly; wrap each element as Value::Text
/// and the whole thing as Value::List.
fn str_list(v: &[String]) -> Value {
    Value::List(v.iter().map(|s| Value::Text(s.clone())).collect())
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

/// Attestation store backed by DuckDB (in-memory for now).
///
/// `location` is retained for the future Parquet flush; the DuckDB connection
/// itself is in-memory until the flush layer lands.
pub struct DuckdbStore {
    location: String,
    conn: duckdb::Connection,
}

impl DuckdbStore {
    /// Open a store at the given location URL.
    /// The DuckDB connection is in-memory; the location will drive Parquet
    /// flush in a subsequent commit. Schema is applied through migrations
    /// (`db/duckdb/migrations/`) — no DDL in application code.
    pub fn open(location: impl Into<String>) -> Result<Self> {
        let conn = duckdb::Connection::open_in_memory()?;
        migrate::migrate(&conn)?;
        Ok(Self {
            location: location.into(),
            conn,
        })
    }

    /// The location URL configured for this store.
    pub fn location(&self) -> &str {
        &self.location
    }

    fn row_to_attestation(
        row: (
            String,
            Value,
            Value,
            Value,
            Value,
            i64,
            String,
            Option<String>,
            i64,
            Option<Vec<u8>>,
            Option<String>,
        ),
    ) -> Result<Attestation> {
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
                 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
                duckdb::params![
                    attestation.id,
                    str_list(&attestation.subjects),
                    str_list(&attestation.predicates),
                    str_list(&attestation.contexts),
                    str_list(&attestation.actors),
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
                    subjects = ?, predicates = ?, contexts = ?, actors = ?,
                    timestamp = ?, source = ?, attributes = ?,
                    signature = ?, signer_did = ?
                 WHERE id = ?",
                duckdb::params![
                    str_list(&attestation.subjects),
                    str_list(&attestation.predicates),
                    str_list(&attestation.contexts),
                    str_list(&attestation.actors),
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
