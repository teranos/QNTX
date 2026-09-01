//! What a namespace is, and what a location holds (ADR-026). A namespace is
//! defined by its `ns.toml`, which root writes and no deployment manages.

use serde::{Deserialize, Serialize};

use crate::error::{DuckdbError, Result};
use crate::is_remote;
use crate::namespace;

/// What `ns.toml` says. The owner is an identity inside QNTX; the DID you show
/// to prove you reach that identity is outside QNTX and is not written here.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Definition {
    pub owner: String,
    pub enabled: bool,
    pub created_at: String,
}

/// A namespace as found at a location: its name, what its `ns.toml` says when
/// it has one, and the kinds it holds.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Namespace {
    pub name: String,
    pub definition: Option<Definition>,
    pub kinds: Vec<String>,
}

/// The file a namespace is defined by, at the root of the namespace.
const NS_FILE: &str = "ns.toml";

/// Where the definition of `name` lives.
fn ns_file(location: &str, name: &str) -> String {
    format!("{}/{NS_FILE}", namespace::root(location, name))
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

    /// Every namespace at this location: the ones defined by an `ns.toml`, and
    /// the ones that only hold objects, which are real and so are listed.
    pub fn list(&self) -> Result<Vec<Namespace>> {
        let base = namespace::root(&self.location, "");
        let base = base.trim_end_matches('/');

        let mut found = self.kinds_held(base)?;
        for (name, definition) in self.definitions(base)? {
            match found.iter_mut().find(|n| n.name == name) {
                Some(existing) => existing.definition = Some(definition),
                None => found.push(Namespace {
                    name,
                    definition: Some(definition),
                    kinds: Vec::new(),
                }),
            }
        }

        found.sort_by(|a, b| a.name.cmp(&b.name));
        for ns in &mut found {
            ns.kinds.sort();
        }
        Ok(found)
    }

    /// What each namespace holds, from the objects under it. An unreachable
    /// location and a location holding nothing are different answers, and an
    /// empty list for both says the second.
    fn kinds_held(&self, base: &str) -> Result<Vec<Namespace>> {
        let sql = format!("SELECT DISTINCT file FROM glob('{base}/*/**')");
        let mut stmt = crate::prepare_fresh(
            &self.conn,
            &self.location,
            &sql,
            &format!("failed to prepare the namespace glob at {base}"),
        )?;
        let rows = stmt
            .query_map([], |row| row.get::<_, String>(0))
            .map_err(|e| {
                DuckdbError::Backend(format!("failed to glob namespaces at {base}: {e}"))
            })?;

        let mut found: Vec<Namespace> = Vec::new();
        for row in rows {
            let path = row.map_err(|e| {
                DuckdbError::Backend(format!("failed to list namespaces under {base}: {e}"))
            })?;
            let Some((name, kind)) = split_namespace_kind(base, &path) else {
                continue;
            };
            match found.iter_mut().find(|n| n.name == name) {
                Some(existing) => {
                    if !existing.kinds.contains(&kind) {
                        existing.kinds.push(kind);
                    }
                }
                None => found.push(Namespace {
                    name,
                    definition: None,
                    kinds: vec![kind],
                }),
            }
        }
        Ok(found)
    }

    /// Every `ns.toml` under `base`, read in one go. A location defining none
    /// answers no rows, so absence needs no error to carry it.
    fn definitions(&self, base: &str) -> Result<Vec<(String, Definition)>> {
        let pattern = format!("{base}/*/{NS_FILE}");
        let sql = format!("SELECT filename, content FROM read_text('{pattern}')");
        let mut stmt = crate::prepare_fresh(
            &self.conn,
            &self.location,
            &sql,
            &format!("failed to prepare the read of every {NS_FILE} under {base}"),
        )?;
        let rows = stmt
            .query_map([], |row| {
                Ok((row.get::<_, String>(0)?, row.get::<_, String>(1)?))
            })
            .map_err(|e| {
                DuckdbError::Backend(format!("failed to read every {NS_FILE} under {base}: {e}"))
            })?;

        let mut defined = Vec::new();
        for row in rows {
            let (path, content) = row.map_err(|e| {
                DuckdbError::Backend(format!("failed to read a {NS_FILE} under {base}: {e}"))
            })?;
            let Some(name) = namespace_of(base, &path) else {
                continue;
            };
            defined.push((name, parse(&path, &content)?));
        }
        Ok(defined)
    }

    /// What `name`'s `ns.toml` says. `None` is the file not being there, which
    /// is a lookup answering, not a lookup failing.
    pub fn definition(&self, name: &str) -> Result<Option<Definition>> {
        let path = ns_file(&self.location, name);
        let sql = format!("SELECT content FROM read_text('{path}')");
        let mut stmt = crate::prepare_fresh(
            &self.conn,
            &self.location,
            &sql,
            &format!("failed to prepare the read of {path}"),
        )?;
        let mut rows = stmt
            .query_map([], |row| row.get::<_, String>(0))
            .map_err(|e| DuckdbError::Backend(format!("failed to read {path}: {e}")))?;

        match rows.next() {
            Some(row) => {
                let content =
                    row.map_err(|e| DuckdbError::Backend(format!("failed to read {path}: {e}")))?;
                Ok(Some(parse(&path, &content)?))
            }
            None => Ok(None),
        }
    }

    /// Create `name` by writing the file that defines it. A name that is
    /// already defined is refused, not taken over.
    pub fn create(&self, name: &str, definition: &Definition) -> Result<()> {
        check_name(name)?;
        if self.definition(name)?.is_some() {
            return Err(DuckdbError::Backend(format!(
                "namespace {name} already exists and already has an owner"
            )));
        }

        let body = render(definition)?;
        let root = namespace::root(&self.location, name);
        if !is_remote(&self.location) {
            std::fs::create_dir_all(&root).map_err(|e| {
                DuckdbError::Backend(format!("failed to create the namespace at {root}: {e}"))
            })?;
        }

        // DuckDB writes no TOML, so the file goes out as the one row of a CSV
        // with nothing quoted, delimited or escaped: the bytes as rendered,
        // plus the newline CSV ends a row with.
        let path = ns_file(&self.location, name);
        let sql = format!(
            "COPY (SELECT ? AS body) TO '{path}' \
             (FORMAT csv, HEADER false, QUOTE '', DELIMITER '', ESCAPE '')"
        );
        self.conn
            .execute(&sql, duckdb::params![body])
            .map_err(|e| {
                DuckdbError::Backend(format!("failed to write {path}, defining {name}: {e}"))
            })?;
        Ok(())
    }
}

/// Read what a `ns.toml` says. The path is in the message because the file is
/// hand-written, and whoever wrote it needs to be told which one is wrong.
fn parse(path: &str, content: &str) -> Result<Definition> {
    toml::from_str(content)
        .map_err(|e| DuckdbError::Backend(format!("failed to read {path} as a namespace: {e}")))
}

/// Render a definition as the file. A value is written as it stands, so one
/// that would need escaping is refused rather than written as a value that
/// reads back different from what was asked for.
fn render(definition: &Definition) -> Result<String> {
    plain_enough("owner", &definition.owner)?;
    plain_enough("created_at", &definition.created_at)?;
    Ok(format!(
        "owner = \"{}\"\nenabled = {}\ncreated_at = \"{}\"",
        definition.owner, definition.enabled, definition.created_at
    ))
}

fn plain_enough(field: &str, value: &str) -> Result<()> {
    let bad = value.contains('"')
        || value.contains('\\')
        || value.contains('\n')
        || value.contains('\r')
        || value.contains('\t');
    if bad {
        return Err(DuckdbError::Backend(format!(
            "the {field} {value:?} carries a quote, a backslash or a line break, and would not \
             read back from {NS_FILE} as what was written"
        )));
    }
    Ok(())
}

/// A namespace is one path segment.
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
/// not come from under `base` and the definition file, which is not a kind.
fn split_namespace_kind(base: &str, path: &str) -> Option<(String, String)> {
    let rest = path.strip_prefix(base)?.trim_start_matches('/');
    let mut parts = rest.split('/');
    let name = parts.next()?;
    let kind = parts.next()?;
    if name.is_empty() || kind.is_empty() || kind == NS_FILE {
        return None;
    }
    Some((name.to_string(), kind.to_string()))
}

/// Pull `<namespace>` out of the path of a definition file under `base`.
fn namespace_of(base: &str, path: &str) -> Option<String> {
    let rest = path.strip_prefix(base)?.trim_start_matches('/');
    let name = rest.split('/').next()?;
    if name.is_empty() {
        return None;
    }
    Some(name.to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn defined() -> Definition {
        Definition {
            owner: "google:104729".to_string(),
            enabled: true,
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

        // Creating writes the file that defines it.
        #[test]
        fn creating_makes_it_listable() {
            let (_dir, store) = park();
            store.create("playground", &defined()).expect("create");

            let found = store.list().expect("list");
            assert_eq!(found.len(), 1);
            assert_eq!(found[0].name, "playground");
            assert_eq!(found[0].definition, Some(defined()));
        }

        // The file is the namespace, so it is a file somebody can open and read.
        #[test]
        fn what_gets_written_is_the_file() {
            let (dir, store) = park();
            store.create("pond", &defined()).expect("create");

            let wrote = std::fs::read_to_string(dir.path().join("pond/ns.toml")).expect("read");
            assert_eq!(
                wrote,
                "owner = \"google:104729\"\nenabled = true\ncreated_at = \"2026-08-17T09:00:00Z\"\n"
            );
        }
    }

    mod spike {
        use super::*;

        // One owner appears on many namespaces.
        #[test]
        fn one_owner_holds_many() {
            let (_dir, store) = park();
            store.create("pond", &defined()).expect("create");
            store.create("playground", &defined()).expect("create");

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
            store.create("pond", &defined()).expect("create");

            assert!(store.create("pond", &defined()).is_err());
        }

        // A namespace is one path segment. A name that escapes it would put a
        // namespace somewhere the location does not reach.
        #[test]
        fn a_name_that_is_a_path_is_refused() {
            let (_dir, store) = park();
            assert!(store.create("pond/ducks", &defined()).is_err());
            assert!(store.create("..", &defined()).is_err());
            assert!(store.create("", &defined()).is_err());
        }

        // Written as it stands means a value carrying a quote would come back as
        // something else, so it does not get written at all.
        #[test]
        fn an_owner_that_would_not_read_back_is_refused() {
            let (_dir, store) = park();
            let sneaky = Definition {
                owner: "magpie\"\nenabled = false\nx = \"".to_string(),
                ..defined()
            };
            assert!(store.create("pond", &sneaky).is_err());
        }
    }

    mod jenny {
        use super::*;

        // Asking whether a namespace is defined is a lookup. Nothing there is an
        // answer, not a failure to get one.
        #[test]
        fn a_namespace_nobody_defined_answers_nothing() {
            let (_dir, store) = park();
            assert_eq!(store.definition("pond").expect("definition"), None);
        }

        // The namespaces that predate the file are still real, and hiding them
        // would make the list a record of this feature rather than of the disk.
        #[test]
        fn a_namespace_nobody_defined_still_lists() {
            let (dir, store) = park();
            let kind = dir.path().join("ducks/attestations");
            std::fs::create_dir_all(&kind).expect("mkdir");
            std::fs::write(kind.join("a.parquet"), b"x").expect("write");

            let found = store.list().expect("list");
            assert_eq!(found.len(), 1);
            assert_eq!(found[0].name, "ducks");
            assert_eq!(found[0].definition, None);
            assert_eq!(found[0].kinds, vec!["attestations"]);
        }

        // Reading a torn definition as nobody having defined it would let
        // create() take the name, and the attestations under it with it.
        #[test]
        fn an_unreadable_definition_does_not_free_the_name() {
            let (dir, store) = park();
            store.create("pond", &defined()).expect("create");
            let file = dir.path().join("pond/ns.toml");
            std::fs::write(&file, b"this is not toml").expect("corrupt");

            assert!(store.definition("pond").is_err());

            let thief = Definition {
                owner: "google:magpie".to_string(),
                ..defined()
            };
            assert!(store.create("pond", &thief).is_err());

            let left = std::fs::read_to_string(&file).expect("read");
            assert!(!left.contains("magpie"), "the file was overwritten: {left}");
        }

        // A file that does not say whether the namespace is enabled has not
        // defined it. Guessing enabled would put it into service.
        #[test]
        fn a_definition_that_says_nothing_about_enabled_is_not_one() {
            let (dir, store) = park();
            store.create("pond", &defined()).expect("create");
            std::fs::write(
                dir.path().join("pond/ns.toml"),
                b"owner = \"google:104729\"\ncreated_at = \"2026-08-17T09:00:00Z\"\n",
            )
            .expect("write");

            assert!(store.definition("pond").is_err());
        }

        // Disabled is a state the file carries, so it reads back as one.
        #[test]
        fn a_disabled_namespace_says_so() {
            let (_dir, store) = park();
            let off = Definition {
                enabled: false,
                ..defined()
            };
            store.create("pond", &off).expect("create");

            assert_eq!(store.definition("pond").expect("definition"), Some(off));
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

        // The file defines the namespace; it is not one of the things it holds.
        #[test]
        fn the_file_is_not_a_kind() {
            let (_dir, store) = park();
            store.create("pond", &defined()).expect("create");

            let found = store.list().expect("list");
            assert_eq!(found[0].kinds, Vec::<String>::new());
        }
    }
}
