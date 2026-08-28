//! The system namespace's signer identity for the parquet backend. ADR-026
//! makes the system namespace the node, so this holds one record and only one.

// Namespace is the top-level prefix, so the object lands under
// `<location>/system/` rather than at the root of the location.

// Unlike access tokens, this holds an ed25519 private key. SQLite already keeps
// it unencrypted, so reach changes rather than exposure — a bucket policy
// question, not an application-crypto one.

use serde::{Deserialize, Serialize};

use crate::error::{DuckdbError, Result};
use crate::{is_remote, remote_setup_sql};

/// A node's signer identity, mirroring `nodedid.Identity` in Go.
/// Keys are hex because the object is JSON, and hex round-trips exactly.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct IdentityRecord {
    pub private_key_hex: String,
    pub public_key_hex: String,
    pub did: String,
}

/// The object holding the identity. Named for the row it stands in for —
/// `node_identity` keyed `'self'` (ADR-026).
const IDENTITY_OBJECT: &str = "self.json";

/// The system namespace's identity at a storage location.
pub struct IdentityStore {
    location: String,
    prefix: String,
    conn: duckdb::Connection,
    current: Option<IdentityRecord>,
}

impl IdentityStore {
    /// Open the store at `location`, loading the identity if one is there.
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
            prefix: system_prefix(&location),
            location,
            conn,
            current: None,
        };
        store.load()?;
        Ok(store)
    }

    /// The location URL this store was opened with.
    pub fn location(&self) -> &str {
        &self.location
    }

    /// The stored identity, or `None` when the node has never generated one.
    pub fn current(&self) -> Option<&IdentityRecord> {
        self.current.as_ref()
    }

    /// Replace the stored identity. A node writes this once, at first boot —
    /// rewriting it mints a new DID and orphans every signature under the old.
    pub fn save(&mut self, record: IdentityRecord) -> Result<()> {
        self.write_object(&record)?;
        self.current = Some(record);
        Ok(())
    }

    /// Read the identity object. A path holding nothing is first boot; a path
    /// that could not be read is an error, so the caller can tell them apart.
    fn load(&mut self) -> Result<()> {
        let sql = format!(
            "SELECT private_key_hex, public_key_hex, did \
             FROM read_json('{}/{IDENTITY_OBJECT}', columns = {{ \
                 private_key_hex: 'VARCHAR', public_key_hex: 'VARCHAR', did: 'VARCHAR' }})",
            self.prefix
        );

        // Nothing there is first boot; anything else is a location that could
        // not be read. A node that answers the second with the first mints a
        // second DID and orphans everything signed under the first.
        let mut stmt = match self.conn.prepare(&sql) {
            Ok(stmt) => stmt,
            Err(e) if crate::nothing_matched(&e) => return Ok(()),
            Err(_) => crate::prepare_fresh(
                &self.conn,
                &self.location,
                &sql,
                &format!("failed to read the node identity under {}", self.prefix),
            )?,
        };
        let mut rows = match stmt.query_map([], |row| {
            Ok(IdentityRecord {
                private_key_hex: row.get(0)?,
                public_key_hex: row.get(1)?,
                did: row.get(2)?,
            })
        }) {
            Ok(rows) => rows,
            Err(e) if crate::nothing_matched(&e) => return Ok(()),
            Err(e) => {
                return Err(DuckdbError::Backend(format!(
                    "failed to read the node identity under {}: {e}",
                    self.prefix
                )))
            }
        };

        if let Some(row) = rows.next() {
            self.current = Some(row.map_err(|e| {
                DuckdbError::Backend(format!(
                    "failed to read the node identity under {}: {e}",
                    self.prefix
                ))
            })?);
        }
        Ok(())
    }

    /// Write the identity to its object, replacing what was there.
    fn write_object(&self, record: &IdentityRecord) -> Result<()> {
        if !is_remote(&self.location) {
            std::fs::create_dir_all(&self.prefix)?;
        }

        let path = format!("{}/{IDENTITY_OBJECT}", self.prefix);
        let sql = format!(
            "COPY (SELECT ? AS private_key_hex, ? AS public_key_hex, ? AS did) \
             TO '{path}' (FORMAT JSON)"
        );

        self.conn
            .execute(
                &sql,
                duckdb::params![record.private_key_hex, record.public_key_hex, record.did],
            )
            .map_err(|e| {
                DuckdbError::Backend(format!("failed to write node identity {path}: {e}"))
            })?;
        Ok(())
    }
}

/// Namespace is the top-level prefix, and the system namespace is the node.
fn system_prefix(location: &str) -> String {
    crate::namespace::prefix(location, crate::namespace::SYSTEM, "node_identity")
}

#[cfg(test)]
mod tests {
    use super::*;

    fn record() -> IdentityRecord {
        IdentityRecord {
            private_key_hex: "aa".repeat(64),
            public_key_hex: "bb".repeat(32),
            did: "did:key:ztest".to_string(),
        }
    }

    #[test]
    fn a_fresh_location_holds_no_identity() {
        let dir = tempfile::tempdir().expect("tempdir");
        let store = IdentityStore::open(format!("file://{}", dir.path().display())).expect("open");
        assert!(store.current().is_none());
    }

    #[test]
    fn the_identity_survives_reopening() {
        let dir = tempfile::tempdir().expect("tempdir");
        let location = format!("file://{}", dir.path().display());

        let mut store = IdentityStore::open(&location).expect("open");
        store.save(record()).expect("save");

        let reopened = IdentityStore::open(&location).expect("reopen");
        assert_eq!(reopened.current(), Some(&record()));
    }

    #[test]
    fn the_object_lands_under_the_system_namespace() {
        let dir = tempfile::tempdir().expect("tempdir");
        let mut store =
            IdentityStore::open(format!("file://{}", dir.path().display())).expect("open");
        store.save(record()).expect("save");

        assert!(dir
            .path()
            .join("system")
            .join("node_identity")
            .join(IDENTITY_OBJECT)
            .exists());
    }
}
