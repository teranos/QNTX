//! Creating, listing and deleting namespaces (ADR-026, ADR-027). A namespace is
//! the top-level prefix and nothing else, so creation writes whose it is.

use serde::{Deserialize, Serialize};

use crate::error::{DuckdbError, Result};
use crate::is_remote;
use crate::namespace::{self, DEFAULT, SYSTEM};

/// Who a namespace belongs to. No private key — a namespace is owned, not a
/// signer, and `minted_by`/`owner_did` is the pair access tokens already carry.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Owner {
    pub owner_did: String,
    pub minted_by: String,
    pub created_at: String,
}

/// A namespace as found at a location: its name, its owner when one was
/// recorded, and the kinds it holds.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Namespace {
    pub name: String,
    pub owner: Option<Owner>,
    pub kinds: Vec<String>,
}

/// The object holding ownership, under the namespace it speaks for.
const OWNER_OBJECT: &str = "self.json";

/// The kind ownership lives under, which is also how a glob spots it.
const OWNER_KIND: &str = "namespace";

/// Where ownership lives for `name`.
fn owner_prefix(location: &str, name: &str) -> String {
    namespace::prefix(location, name, OWNER_KIND)
}

/// Namespaces that cannot be deleted, because neither was created (ADR-027).
pub fn is_permanent(name: &str) -> bool {
    name == SYSTEM || name == DEFAULT
}

/// Namespace management at a storage location.
pub struct NamespaceStore {
    location: String,
    conn: duckdb::Connection,
}

impl NamespaceStore {
    /// Open management for the location holding the namespaces.
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
        if let Some(sql) = crate::remote_setup_sql(&location) {
            conn.execute_batch(&sql)?;
        }

        Ok(Self { location, conn })
    }

    /// Every namespace at this location, found through the objects under it —
    /// glob returns files, and a namespace holding no bytes is not on disk.
    pub fn list(&self) -> Result<Vec<Namespace>> {
        let base = namespace::root(&self.location, "");
        let base = base.trim_end_matches('/');

        // An unreachable location and a location holding nothing are different
        // answers, and returning an empty list for both says the second.
        let sql = format!("SELECT DISTINCT file FROM glob('{base}/*/**')");
        let mut stmt = self.conn.prepare(&sql).map_err(|e| {
            DuckdbError::Backend(format!(
                "failed to prepare the namespace glob at {base}: {e}"
            ))
        })?;
        let rows = stmt
            .query_map([], |row| row.get::<_, String>(0))
            .map_err(|e| {
                DuckdbError::Backend(format!("failed to glob namespaces at {base}: {e}"))
            })?;

        let mut found: Vec<(String, Vec<String>)> = Vec::new();
        for row in rows {
            let path = row.map_err(|e| {
                DuckdbError::Backend(format!("failed to list namespaces under {base}: {e}"))
            })?;
            let Some((name, kind)) = split_namespace_kind(base, &path) else {
                continue;
            };
            match found.iter_mut().find(|(n, _)| *n == name) {
                Some((_, kinds)) => {
                    if !kinds.contains(&kind) {
                        kinds.push(kind);
                    }
                }
                None => found.push((name, vec![kind])),
            }
        }

        found.sort_by(|a, b| a.0.cmp(&b.0));
        found
            .into_iter()
            .map(|(name, mut kinds)| {
                kinds.sort();
                // The glob already saw whether the owner object is there, so
                // reading one that is absent would turn "no owner recorded"
                // and "could not read it" back into the same answer.
                let owner = if kinds.iter().any(|k| k == OWNER_KIND) {
                    self.owner(&name)?
                } else {
                    None
                };
                Ok(Namespace { name, owner, kinds })
            })
            .collect()
    }

    /// Who owns `name`, or `None` when nothing recorded it.
    pub fn owner(&self, name: &str) -> Result<Option<Owner>> {
        let prefix = owner_prefix(&self.location, name);
        let sql = format!(
            "SELECT owner_did, minted_by, created_at \
             FROM read_json('{prefix}/{OWNER_OBJECT}', columns = {{ \
                 owner_did: 'VARCHAR', minted_by: 'VARCHAR', created_at: 'VARCHAR' }})"
        );

        let mut stmt = match self.conn.prepare(&sql) {
            Ok(stmt) => stmt,
            Err(_) => return Ok(None),
        };
        let mut rows = match stmt.query_map([], |row| {
            Ok(Owner {
                owner_did: row.get(0)?,
                minted_by: row.get(1)?,
                created_at: row.get(2)?,
            })
        }) {
            Ok(rows) => rows,
            Err(_) => return Ok(None),
        };

        match rows.next() {
            Some(row) => Ok(Some(row.map_err(|e| {
                DuckdbError::Backend(format!("failed to read the owner of {name}: {e}"))
            })?)),
            None => Ok(None),
        }
    }

    /// Create `name` by recording who owns it, which is the write that makes it
    /// exist. A name already carrying an owner is refused, not reassigned.
    pub fn create(&self, name: &str, owner: &Owner) -> Result<()> {
        check_name(name)?;
        if self.owner(name)?.is_some() {
            return Err(DuckdbError::Backend(format!(
                "namespace {name} already exists and already has an owner"
            )));
        }

        let prefix = owner_prefix(&self.location, name);
        if !is_remote(&self.location) {
            std::fs::create_dir_all(&prefix).map_err(|e| {
                DuckdbError::Backend(format!("failed to create the namespace at {prefix}: {e}"))
            })?;
        }

        let path = format!("{prefix}/{OWNER_OBJECT}");
        let sql = format!(
            "COPY (SELECT ? AS owner_did, ? AS minted_by, ? AS created_at) \
             TO '{path}' (FORMAT JSON)"
        );
        self.conn
            .execute(
                &sql,
                duckdb::params![owner.owner_did, owner.minted_by, owner.created_at],
            )
            .map_err(|e| {
                DuckdbError::Backend(format!(
                    "failed to write the owner of {name} to {path}: {e}"
                ))
            })?;
        Ok(())
    }

    /// Delete `name` and everything under it (ADR-027). Local only: half of a
    /// remote delete leaves a namespace that lists but cannot be read.
    pub fn delete(&self, name: &str) -> Result<()> {
        check_name(name)?;
        if is_permanent(name) {
            return Err(DuckdbError::Backend(format!(
                "the {name} namespace cannot be deleted"
            )));
        }
        if is_remote(&self.location) {
            return Err(DuckdbError::Backend(format!(
                "deleting {name} at {} is not supported; remote prefixes are removed \
                 by the object store, not from here",
                self.location
            )));
        }

        let root = namespace::root(&self.location, name);
        std::fs::remove_dir_all(&root)
            .map_err(|e| DuckdbError::Backend(format!("failed to delete {root}: {e}")))?;
        Ok(())
    }
}

/// A namespace is one path segment, so anything that would make it more than
/// that — a separator, a traversal, a quote — is not a name.
fn check_name(name: &str) -> Result<()> {
    let bad = name.is_empty()
        || name == "."
        || name == ".."
        || name.contains('/')
        || name.contains('\\')
        || name.contains('\'')
        || name.starts_with(' ')
        || name.ends_with(' ');
    if bad {
        return Err(DuckdbError::Backend(format!(
            "namespace name {name:?} is not a single path segment"
        )));
    }
    Ok(())
}

/// Pull `<namespace>/<kind>` out of a globbed path, ignoring anything that did
/// not come from under `base`.
fn split_namespace_kind(base: &str, path: &str) -> Option<(String, String)> {
    let rest = path.strip_prefix(base)?.trim_start_matches('/');
    let mut parts = rest.split('/');
    let name = parts.next()?;
    let kind = parts.next()?;
    if name.is_empty() || kind.is_empty() {
        return None;
    }
    Some((name.to_string(), kind.to_string()))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn owner() -> Owner {
        Owner {
            owner_did: "did:key:znode".to_string(),
            minted_by: "https://chaos.social/@groundskeeper".to_string(),
            created_at: "2026-08-17T09:00:00Z".to_string(),
        }
    }

    fn park() -> (tempfile::TempDir, NamespaceStore) {
        let dir = tempfile::tempdir().expect("tempdir");
        let store = NamespaceStore::open(format!("file://{}", dir.path().display())).expect("open");
        (dir, store)
    }

    mod tim {
        use super::*;

        // An empty park has no namespaces, and saying so is not an error.
        #[test]
        fn a_fresh_location_holds_none() {
            let (_dir, store) = park();
            assert_eq!(store.list().expect("list"), Vec::new());
        }

        // A location that cannot be read is a different answer from one holding
        // nothing, and an empty list would say the second.
        #[test]
        fn an_unreadable_location_is_an_error_not_an_empty_park() {
            let store =
                NamespaceStore::open("s3://qntx-no-such-park-here/attestations").expect("open");
            assert!(store.list().is_err());
        }

        // Creating writes whose it is, and that write is what makes it exist.
        #[test]
        fn creating_makes_it_listable() {
            let (_dir, store) = park();
            store.create("playground", &owner()).expect("create");

            let found = store.list().expect("list");
            assert_eq!(found.len(), 1);
            assert_eq!(found[0].name, "playground");
            assert_eq!(found[0].owner, Some(owner()));
        }

        #[test]
        fn deleting_takes_it_away() {
            let (_dir, store) = park();
            store.create("tenniscourt", &owner()).expect("create");
            store.delete("tenniscourt").expect("delete");

            assert_eq!(store.list().expect("list"), Vec::new());
        }
    }

    mod spike {
        use super::*;

        // One owner holds as many as SUPER created — the whole point of the DID
        // being ownership rather than the key.
        #[test]
        fn one_owner_holds_many() {
            let (_dir, store) = park();
            store.create("pond", &owner()).expect("create");
            store.create("playground", &owner()).expect("create");

            let names: Vec<String> = store
                .list()
                .expect("list")
                .into_iter()
                .map(|n| n.name)
                .collect();
            assert_eq!(names, vec!["playground", "pond"]);
        }

        #[test]
        fn a_name_that_is_taken_is_refused() {
            let (_dir, store) = park();
            store.create("pond", &owner()).expect("create");

            assert!(store.create("pond", &owner()).is_err());
        }

        // ADR-027: neither was created, so neither may be deleted.
        #[test]
        fn system_and_default_cannot_be_deleted() {
            let (_dir, store) = park();
            assert!(store.delete(SYSTEM).is_err());
            assert!(store.delete(DEFAULT).is_err());
        }

        // A namespace is one path segment. A name that escapes it would put a
        // namespace somewhere the location does not reach.
        #[test]
        fn a_name_that_is_a_path_is_refused() {
            let (_dir, store) = park();
            assert!(store.create("pond/ducks", &owner()).is_err());
            assert!(store.create("..", &owner()).is_err());
            assert!(store.create("", &owner()).is_err());
        }
    }

    mod jenny {
        use super::*;

        // The namespaces that predate ownership are still real, and hiding them
        // would make the list a record of this feature rather than of the disk.
        #[test]
        fn a_namespace_nobody_declared_still_lists() {
            let (dir, store) = park();
            let kind = dir.path().join("ducks/attestations");
            std::fs::create_dir_all(&kind).expect("mkdir");
            std::fs::write(kind.join("a.parquet"), b"x").expect("write");

            let found = store.list().expect("list");
            assert_eq!(found.len(), 1);
            assert_eq!(found[0].name, "ducks");
            assert_eq!(found[0].owner, None);
            assert_eq!(found[0].kinds, vec!["attestations"]);
        }

        // What a namespace holds is what it is made of, so the kinds come back
        // deduplicated rather than once per object under them.
        #[test]
        fn the_kinds_are_what_it_holds() {
            let (dir, store) = park();
            let root = dir.path().join("playground");
            std::fs::create_dir_all(root.join("attestations")).expect("mkdir");
            std::fs::create_dir_all(root.join("watchers")).expect("mkdir");
            std::fs::write(root.join("attestations/a.parquet"), b"x").expect("write");
            std::fs::write(root.join("attestations/b.parquet"), b"x").expect("write");
            std::fs::write(root.join("watchers/w.parquet"), b"x").expect("write");

            let found = store.list().expect("list");
            assert_eq!(found[0].kinds, vec!["attestations", "watchers"]);
        }
    }
}
