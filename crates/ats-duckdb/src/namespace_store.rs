//! What a namespace is, and what a location holds (ADR-026). A namespace is
//! defined by its `ns.toml`, which root writes and no deployment manages.
//!
//! "WE SUPERSEDE. WE SAY, HERE IS ANOTHER PIECE OF DATA, THAT IS NOW MORE
//! LOAD-BEARING THAN BEFORE. AND THE DATA ACTUALLY NEVER LEAVES."
//!
//! Which is why a namespace goes out of service by its file saying so. Drained
//! and deleted are both states of the definition, and neither removes a byte
//! from under the prefix.

use serde::{Deserialize, Serialize};

use crate::error::{DuckdbError, Result};
use crate::is_remote;
use crate::namespace::{self, DEFAULT, SYSTEM};

/// What `ns.toml` says. The owner is an identity inside QNTX; the DID you show
/// to prove you reach that identity is outside QNTX and is not written here.
///
/// The optional fields are the states a namespace passes through on its way out
/// of service. A file carrying none of them is a namespace in service, which is
/// every file written before there were any.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Definition {
    pub owner: String,
    pub enabled: bool,
    pub created_at: String,
    /// The namespace this one was drained into. Every attestation it held was
    /// written into that one; what is under this prefix stays under it, and
    /// reads and writes are refused naming the target.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub drained_into: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub drained_at: Option<String>,
    /// How many attestations were carried across. Delete holds it against what
    /// the prefix still answers with, so empty is counted and never assumed.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub drained_count: Option<u64>,
    /// When it was deleted, and by whom. The file stays so the name cannot be
    /// taken again over the bytes the old one wrote.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub deleted_at: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub deleted_by: Option<String>,
}

/// The two namespaces a deployment always has (ADR-026). Neither was created,
/// so neither is drained and neither is deleted.
pub fn is_permanent(name: &str) -> bool {
    name == SYSTEM || name == DEFAULT
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
        let paths = crate::rows_fresh(
            &self.conn,
            &self.location,
            &sql,
            &format!("failed to glob namespaces at {base}"),
            |row| row.get::<_, String>(0),
        )?;

        let mut found: Vec<Namespace> = Vec::new();
        for path in paths {
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

    /// Every `ns.toml` under `base`, as it was found and what it holds.
    ///
    /// The pattern carries a wildcard, which is what makes this a listing. A
    /// path without one is fetched instead, and object storage answers a fetch
    /// of something absent with a 404 — which would put a namespace being free
    /// behind an error again.
    fn contents(&self, base: &str) -> Result<Vec<(String, String, String)>> {
        let pattern = format!("{base}/*/{NS_FILE}");
        let sql = format!("SELECT filename, content FROM read_text('{pattern}')");
        let read = crate::rows_fresh(
            &self.conn,
            &self.location,
            &sql,
            &format!("failed to read every {NS_FILE} under {base}"),
            |row| Ok((row.get::<_, String>(0)?, row.get::<_, String>(1)?)),
        )?;

        let mut found = Vec::new();
        for (path, content) in read {
            let Some(name) = namespace_of(base, &path) else {
                continue;
            };
            found.push((name, path, content));
        }
        Ok(found)
    }

    /// Every namespace that has an `ns.toml`, and what each one says.
    fn definitions(&self, base: &str) -> Result<Vec<(String, Definition)>> {
        self.contents(base)?
            .into_iter()
            .map(|(name, path, content)| Ok((name, parse(&path, &content)?)))
            .collect()
    }

    /// What `name`'s `ns.toml` says. `None` is the file not being there, which
    /// is a lookup answering, not a lookup failing.
    ///
    /// Only this namespace's file is parsed. A neighbour whose file is torn is
    /// that neighbour's problem, and must not be what refuses a new name.
    pub fn definition(&self, name: &str) -> Result<Option<Definition>> {
        let base = namespace::root(&self.location, "");
        let base = base.trim_end_matches('/');

        match self.contents(base)?.into_iter().find(|(n, _, _)| n == name) {
            Some((_, path, content)) => Ok(Some(parse(&path, &content)?)),
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

        self.write_definition(name, definition)
    }

    /// Supersede what `name`'s `ns.toml` says. The prefix, the objects and
    /// everything under them are untouched — a newer record says what the
    /// namespace is now, and that record is the one that counts.
    ///
    /// A name nobody defined is refused rather than defined by this: writing a
    /// definition is what creates a namespace, and superseding is not creating.
    ///
    /// `system` and `default` are refused. Neither was created, and a state
    /// saying one of them is out of service would take the node with it.
    pub fn amend(&self, name: &str, definition: &Definition) -> Result<()> {
        check_name(name)?;
        if is_permanent(name) {
            return Err(DuckdbError::Backend(format!(
                "the {name} namespace cannot be drained or deleted; it was never created"
            )));
        }
        if self.definition(name)?.is_none() {
            return Err(DuckdbError::Backend(format!(
                "namespace {name} has no {NS_FILE}, so there is no definition to supersede"
            )));
        }
        self.write_definition(name, definition)
    }

    /// Write the file that defines `name`, whatever it said before.
    fn write_definition(&self, name: &str, definition: &Definition) -> Result<()> {
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
///
/// The states a namespace has passed through are written only when it has
/// passed through them: a file saying `drained_into = ""` would say it was
/// drained into a namespace with no name.
fn render(definition: &Definition) -> Result<String> {
    plain_enough("owner", &definition.owner)?;
    plain_enough("created_at", &definition.created_at)?;
    let mut body = format!(
        "owner = \"{}\"\nenabled = {}\ncreated_at = \"{}\"",
        definition.owner, definition.enabled, definition.created_at
    );
    said(&mut body, "drained_into", definition.drained_into.as_deref())?;
    said(&mut body, "drained_at", definition.drained_at.as_deref())?;
    if let Some(count) = definition.drained_count {
        body.push_str(&format!("\ndrained_count = {count}"));
    }
    said(&mut body, "deleted_at", definition.deleted_at.as_deref())?;
    said(&mut body, "deleted_by", definition.deleted_by.as_deref())?;
    Ok(body)
}

/// Append one line the definition carries, or nothing when it does not.
fn said(body: &mut String, field: &str, value: Option<&str>) -> Result<()> {
    let Some(value) = value else { return Ok(()) };
    plain_enough(field, value)?;
    body.push_str(&format!("\n{field} = \"{value}\""));
    Ok(())
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
            drained_into: None,
            drained_at: None,
            drained_count: None,
            deleted_at: None,
            deleted_by: None,
        }
    }

    /// What the definition says once the namespace has been drained into
    /// `playground`: out of service, and the target named on the file.
    fn drained() -> Definition {
        Definition {
            enabled: false,
            drained_into: Some("playground".to_string()),
            drained_at: Some("2026-09-06T11:00:00Z".to_string()),
            drained_count: Some(3),
            ..defined()
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

        // Asking whether a name is free reads the files that are there, and one
        // of them being torn is that namespace's problem and not this name's.
        #[test]
        fn a_torn_neighbour_does_not_refuse_a_new_name() {
            let (dir, store) = park();
            store.create("pond", &defined()).expect("create");
            std::fs::write(dir.path().join("pond/ns.toml"), b"this is not toml").expect("corrupt");

            assert_eq!(store.definition("playground").expect("definition"), None);
            store.create("playground", &defined()).expect("create");
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

    // We drain before delete. Both are states of the file, and neither takes a
    // byte out from under the prefix.
    mod drained_before_deleted {
        use super::*;

        // A drain is a newer record saying where the namespace went. It reads
        // back as one, which is what makes reads and writes refusable by name.
        #[test]
        fn a_drained_namespace_says_where_it_went() {
            let (_dir, store) = park();
            store.create("pond", &defined()).expect("create");
            store.amend("pond", &drained()).expect("amend");

            assert_eq!(store.definition("pond").expect("definition"), Some(drained()));
        }

        // The file is the namespace, so a person can open it and read what
        // happened to it. The count is there because delete holds the prefix
        // against it rather than trusting that a drain emptied anything.
        #[test]
        fn what_a_drain_writes_is_the_file() {
            let (dir, store) = park();
            store.create("pond", &defined()).expect("create");
            store.amend("pond", &drained()).expect("amend");

            let wrote = std::fs::read_to_string(dir.path().join("pond/ns.toml")).expect("read");
            assert_eq!(
                wrote,
                "owner = \"google:104729\"\nenabled = false\n\
                 created_at = \"2026-08-17T09:00:00Z\"\n\
                 drained_into = \"playground\"\n\
                 drained_at = \"2026-09-06T11:00:00Z\"\n\
                 drained_count = 3\n"
            );
        }

        // Deleted is a state and not a removal. The file stays, and it says
        // when and by whom — a name whose bytes are still there is not free.
        #[test]
        fn a_deleted_namespace_keeps_the_file_that_says_so() {
            let (dir, store) = park();
            store.create("pond", &defined()).expect("create");
            let gone = Definition {
                deleted_at: Some("2026-09-06T11:05:00Z".to_string()),
                deleted_by: Some("https://mastodon.example/@tim".to_string()),
                ..drained()
            };
            store.amend("pond", &gone).expect("amend");

            assert!(dir.path().join("pond/ns.toml").exists(), "the file was removed");
            assert_eq!(store.definition("pond").expect("definition"), Some(gone));
        }

        // There is no undo for a home, so the name cannot be taken again over
        // the bytes the old one wrote.
        #[test]
        fn a_deleted_name_cannot_be_created_over() {
            let (_dir, store) = park();
            store.create("pond", &defined()).expect("create");
            let gone = Definition {
                deleted_at: Some("2026-09-06T11:05:00Z".to_string()),
                ..drained()
            };
            store.amend("pond", &gone).expect("amend");

            assert!(store.create("pond", &defined()).is_err());
        }

        // Superseding is not creating. A name nobody defined has no definition
        // to supersede, and writing one here would define it by the back door.
        #[test]
        fn a_namespace_nobody_defined_cannot_be_amended() {
            let (_dir, store) = park();
            assert!(store.amend("pond", &drained()).is_err());
        }

        // Neither was created, so neither may be drained or deleted (ADR-026).
        #[test]
        fn system_and_default_cannot_be_amended() {
            let (_dir, store) = park();
            assert!(store.amend(SYSTEM, &drained()).is_err());
            assert!(store.amend(DEFAULT, &drained()).is_err());
        }

        // A file written before there were any states is a namespace in
        // service, and reads back as one rather than failing to read.
        #[test]
        fn a_file_that_names_no_state_is_in_service() {
            let (dir, store) = park();
            store.create("pond", &defined()).expect("create");
            std::fs::write(
                dir.path().join("pond/ns.toml"),
                b"owner = \"google:104729\"\nenabled = true\ncreated_at = \"2026-08-17T09:00:00Z\"\n",
            )
            .expect("write");

            let found = store.definition("pond").expect("definition").expect("defined");
            assert_eq!(found.drained_into, None);
            assert_eq!(found.deleted_at, None);
        }

        // Written as it stands, the same as the owner: a target carrying a
        // quote would read back as a namespace nobody drained into.
        #[test]
        fn a_target_that_would_not_read_back_is_refused() {
            let (_dir, store) = park();
            store.create("pond", &defined()).expect("create");
            let sneaky = Definition {
                drained_into: Some("playground\"\ndeleted_at = \"now".to_string()),
                ..drained()
            };
            assert!(store.amend("pond", &sneaky).is_err());
        }
    }
}
