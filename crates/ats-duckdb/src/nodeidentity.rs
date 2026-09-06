//! The node's signer identity for the parquet backend: one record, at
//! `<location>/system/`, holding an ed25519 private key in the clear.

use serde::{Deserialize, Serialize};

use crate::error::{DuckdbError, Name, Object, Refusal, Result};
use crate::objects::Objects;

/// A node's signer identity, mirroring `nodedid.Identity` in Go.
/// Keys are hex because the object is JSON, and hex round-trips exactly.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct IdentityRecord {
    pub private_key_hex: String,
    pub public_key_hex: String,
    pub did: String,
}

/// Named for the row it stands in for: `node_identity` keyed `'self'`.
const IDENTITY_OBJECT: &str = "self.json";

/// The system namespace's identity at a storage location.
pub struct IdentityStore {
    location: String,
    prefix: String,
    objects: Objects,
    current: Option<IdentityRecord>,
}

impl IdentityStore {
    /// Open the store at `location`, loading the identity if one is there.
    pub fn open(location: impl Into<String>) -> Result<Self> {
        let location = location.into();
        if location.contains('\'') {
            return Err(DuckdbError::BadName {
                which: Name::Location,
                value: location,
                why: Refusal::CarriesAQuote,
            });
        }

        let mut store = Self {
            prefix: system_prefix(&location),
            objects: Objects::open(&location)?,
            location,
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

    /// Read the identity object. Nothing there is first boot; a path that could
    /// not be read is an error — answering the second with the first mints a
    /// second DID and orphans everything signed under the first.
    fn load(&mut self) -> Result<()> {
        let path = self.path();
        let Some(bytes) = self.objects.get(Object::NodeIdentity, &path)? else {
            return Ok(());
        };
        let record: IdentityRecord =
            serde_json::from_slice(&bytes).map_err(|source| DuckdbError::NotJSON {
                what: Object::NodeIdentity,
                path,
                source,
            })?;
        self.current = Some(record);
        Ok(())
    }

    /// Write the identity to its object, replacing what was there.
    fn write_object(&self, record: &IdentityRecord) -> Result<()> {
        let body = serde_json::to_vec(record).map_err(|source| DuckdbError::NotSerializable {
            what: Object::NodeIdentity,
            id: record.did.clone(),
            source,
        })?;
        self.objects.put(Object::NodeIdentity, &self.path(), body)
    }

    fn path(&self) -> String {
        format!("{}/{IDENTITY_OBJECT}", self.prefix)
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
