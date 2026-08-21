//! User records for the parquet backend (ADR-031).

//! One object per User under `<location>/system/users/`, rewritten on change —
//! the small-config shape from ADR-024, the same one access tokens use.

//! The system namespace, because a User lives in namespaces plural and cannot
//! be kept inside one of them. Resolving who someone is happens above them,
//! the same reason a bearer is resolved there (ADR-027).

//! The object is named by the User's own id rather than by what it is looked
//! up with: a User is reached by several routes, and naming it after one would
//! make the others a second answer. Lookup scans, so no index can drift.

use serde::{Deserialize, Serialize};

use crate::error::{DuckdbError, Result};
use crate::{is_remote, remote_setup_sql};

/// One `did:key` a User holds. laye mints one per browser, an authenticator
/// derives one per device, and none of them is the User.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct KeyRecord {
    pub did: String,

    /// `BROWSER` or `DEVICE`. An unknown origin is not a third kind, it is a
    /// record that should not have been written.
    pub origin: String,
}

/// One provider account a User holds. Adding a provider adds a vocabulary
/// rather than a field.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct AccountRecord {
    pub provider: String,

    /// What the provider calls the account, and the string
    /// `auth.root_identities` is matched against.
    pub canonical_id: String,

    /// What the account calls itself. Display only, and it can change.
    #[serde(default)]
    pub handle: String,
}

/// A User: a human being. Mirrors `protocol.User` for the fields this pass
/// writes — names, addresses and namespaces are ADR-031 and not here.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct UserRecord {
    /// An ASUID under `US` (ADR-010).
    pub id: String,

    /// What this person calls themselves. Empty until they first arrive, and
    /// the one handle a User has that no provider issued.
    #[serde(default)]
    pub username: String,

    /// Any number of them, because neither one tells one User from another.
    #[serde(default)]
    pub email_addresses: Vec<String>,

    /// `ATTESTOR`, `SUPER` or `ROOT` (ADR-027).
    pub level: String,

    /// The User that created this one. Empty belongs to ROOT alone, created by
    /// proving a listed route before there is a User to name.
    #[serde(default)]
    pub created_by: String,

    #[serde(default)]
    pub keys: Vec<KeyRecord>,

    #[serde(default)]
    pub accounts: Vec<AccountRecord>,

    pub created_at: i64,

    /// A disabled User still exists, so this is when rather than whether.
    #[serde(default)]
    pub disabled_at: Option<i64>,

    #[serde(default)]
    pub disabled_by: Option<String>,
}

impl UserRecord {
    /// Whether an `auth.root_identities` entry reaches this User. A route is a
    /// did:key or an account's canonical_id, so both are asked.
    pub fn reached_by(&self, route: &str) -> bool {
        self.keys.iter().any(|k| k.did == route)
            || self.accounts.iter().any(|a| a.canonical_id == route)
    }
}

/// The Users at a storage location.
pub struct UserStore {
    location: String,
    prefix: String,
    conn: duckdb::Connection,
}

impl UserStore {
    /// Open the store at `location`.
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

        Ok(Self {
            prefix: users_prefix(&location),
            location,
            conn,
        })
    }

    /// The location URL this store was opened with.
    pub fn location(&self) -> &str {
        &self.location
    }

    /// Every User. An unresolvable prefix is no Users rather than a failure, so
    /// a deployment that never minted one can tell that from a broken path.
    pub fn all(&self) -> Result<Vec<UserRecord>> {
        let sql = format!("SELECT content FROM read_text('{}/*.json')", self.prefix);

        let mut stmt = match self.conn.prepare(&sql) {
            Ok(stmt) => stmt,
            Err(_) => return Ok(Vec::new()),
        };
        let rows = match stmt.query_map([], |row| row.get::<_, String>(0)) {
            Ok(rows) => rows,
            Err(_) => return Ok(Vec::new()),
        };

        let mut users = Vec::new();
        for row in rows {
            let body = row.map_err(|e| {
                DuckdbError::Backend(format!("failed to read a User under {}: {e}", self.prefix))
            })?;
            let record: UserRecord = serde_json::from_str(&body).map_err(|e| {
                DuckdbError::Backend(format!(
                    "a User object under {} is not readable JSON: {e}",
                    self.prefix
                ))
            })?;
            users.push(record);
        }
        Ok(users)
    }

    /// The User an `auth.root_identities` entry reaches, if one was minted.
    pub fn by_route(&self, route: &str) -> Result<Option<UserRecord>> {
        Ok(self.all()?.into_iter().find(|u| u.reached_by(route)))
    }

    /// Write a User, replacing what was there. Rewriting the one object that
    /// holds it leaves no earlier version to be read back.
    pub fn put(&self, record: &UserRecord) -> Result<()> {
        if record.id.contains('/') || record.id.contains('\'') {
            return Err(DuckdbError::Backend(format!(
                "User id {} cannot be used as an object name",
                record.id
            )));
        }
        if !is_remote(&self.location) {
            let _ = std::fs::create_dir_all(&self.prefix);
        }

        let body = serde_json::to_string(record).map_err(|e| {
            DuckdbError::Backend(format!("failed to serialize User {}: {e}", record.id))
        })?;

        let path = format!("{}/{}.json", self.prefix, record.id);
        let sql = format!(
            "COPY (SELECT ? AS body) TO '{path}' \
             (FORMAT CSV, HEADER false, QUOTE '', DELIMITER '')"
        );

        self.conn
            .execute(&sql, duckdb::params![body])
            .map_err(|e| DuckdbError::Backend(format!("failed to write User {path}: {e}")))?;
        Ok(())
    }
}

/// A User lives in the system namespace, above the namespaces they live in.
fn users_prefix(location: &str) -> String {
    crate::namespace::prefix(location, crate::namespace::SYSTEM, "users")
}

#[cfg(test)]
mod tests {
    use super::*;

    fn user(id: &str, route: &str) -> UserRecord {
        UserRecord {
            id: id.to_string(),
            username: "tim".to_string(),
            email_addresses: vec!["tim@example.com".to_string()],
            level: "ROOT".to_string(),
            created_by: String::new(),
            keys: vec![KeyRecord {
                did: route.to_string(),
                origin: "BROWSER".to_string(),
            }],
            accounts: Vec::new(),
            created_at: 1,
            disabled_at: None,
            disabled_by: None,
        }
    }

    #[test]
    fn a_fresh_location_holds_no_users() {
        let dir = tempfile::tempdir().expect("tempdir");
        let store = UserStore::open(format!("file://{}", dir.path().display())).expect("open");
        assert!(store.all().expect("all").is_empty());
    }

    #[test]
    fn a_user_survives_reopening() {
        let dir = tempfile::tempdir().expect("tempdir");
        let location = format!("file://{}", dir.path().display());

        let store = UserStore::open(&location).expect("open");
        store.put(&user("US-TIM-1", "did:key:zOne")).expect("put");

        let reopened = UserStore::open(&location).expect("reopen");
        assert_eq!(reopened.all().expect("all").len(), 1);
    }

    /// A User is reached by several routes, and an account is one of them.
    #[test]
    fn an_account_reaches_the_user_that_holds_it() {
        let dir = tempfile::tempdir().expect("tempdir");
        let store = UserStore::open(format!("file://{}", dir.path().display())).expect("open");

        let mut u = user("US-TIM-2", "did:key:zBrowser");
        u.accounts.push(AccountRecord {
            provider: "mastodon".to_string(),
            canonical_id: "https://mastodon.example/@tim".to_string(),
            handle: "@tim@mastodon.example".to_string(),
        });
        store.put(&u).expect("put");

        let found = store
            .by_route("https://mastodon.example/@tim")
            .expect("by_route");
        assert_eq!(found.map(|f| f.id), Some("US-TIM-2".to_string()));
    }

    /// Rewriting replaces, so no earlier version can be read back.
    #[test]
    fn writing_a_user_again_replaces_it() {
        let dir = tempfile::tempdir().expect("tempdir");
        let store = UserStore::open(format!("file://{}", dir.path().display())).expect("open");

        let mut u = user("US-TIM-3", "did:key:zFirst");
        store.put(&u).expect("put");
        u.keys.push(KeyRecord {
            did: "did:key:zSecond".to_string(),
            origin: "BROWSER".to_string(),
        });
        store.put(&u).expect("put again");

        let all = store.all().expect("all");
        assert_eq!(all.len(), 1);
        assert_eq!(all[0].keys.len(), 2);
    }

    #[test]
    fn the_objects_land_under_the_system_namespace() {
        let dir = tempfile::tempdir().expect("tempdir");
        let store = UserStore::open(format!("file://{}", dir.path().display())).expect("open");
        store.put(&user("US-TIM-4", "did:key:zOne")).expect("put");

        assert!(dir
            .path()
            .join("system")
            .join("users")
            .join("US-TIM-4.json")
            .exists());
    }
}
