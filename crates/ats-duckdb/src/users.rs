//! User records for the parquet backend (ADR-031).

//! One object per User under `<location>/system/users/`, rewritten on change —
//! the small-config shape from ADR-024, the same one access tokens use.

//! The object is named by the User's own id. A User is reached by several
//! routes, so lookup scans for the one that matches.

use serde::{Deserialize, Deserializer, Serialize};

use crate::error::{DuckdbError, Name, Object, Refusal, Result};
use crate::objects::Objects;

/// Reads an explicit null as the default. Go marshals an empty slice as null,
/// and serde's own default covers an absent field only.
fn null_is_default<'de, D, T>(deserializer: D) -> std::result::Result<T, D::Error>
where
    D: Deserializer<'de>,
    T: Default + Deserialize<'de>,
{
    Ok(Option::deserialize(deserializer)?.unwrap_or_default())
}

/// One `did:key` a User holds. laye mints one per browser, an authenticator
/// derives one per device.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct KeyRecord {
    pub did: String,

    /// `BROWSER` or `DEVICE`.
    pub origin: String,
}

/// One provider account a User holds. Adding a provider adds a vocabulary
/// rather than a field.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct AccountRecord {
    pub provider: String,

    /// What the provider calls the account, and the string
    /// `auth.root_identities` is matched against.
    pub canonical_id: String,

    /// What the account calls itself. Display only, and it can change.
    #[serde(default)]
    pub handle: String,

    /// The signed binding that reached this account (ADR-031), as Go wrote it.
    /// Carried through unread: the node re-verifies it, this store only keeps
    /// it. Absent on a record written before bindings were kept.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub binding: Option<serde_json::Value>,
}

/// A User: a human being (ADR-031). Every field Go writes is named here, and a
/// field it writes that is not is refused rather than dropped.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct UserRecord {
    /// An ASUID under `US` (ADR-010).
    pub id: String,

    /// What this person calls themselves. Empty until they first arrive, and
    /// the one handle a User has that no provider issued.
    #[serde(default)]
    pub display_name: String,

    /// Any number of them, because neither one tells one User from another.
    #[serde(default, deserialize_with = "null_is_default")]
    pub email_addresses: Vec<String>,

    /// Any number of them, for the same reason.
    #[serde(default, deserialize_with = "null_is_default")]
    pub phone_numbers: Vec<String>,

    /// `ATTESTOR`, `SUPER` or `ROOT` (ADR-027).
    pub level: String,

    /// The User that created this one. Empty belongs to ROOT alone, created by
    /// proving a listed route before there is a User to name.
    #[serde(default)]
    pub created_by: String,

    /// The User that switched this one off, and empty is a User that is on
    /// (ADR-031).
    #[serde(default)]
    pub disabled_by: String,

    /// The door this User registered at (ADR-032), set for a public
    /// registration alone. The same account at two doors is two registrations,
    /// and this is the half of what identifies one that is not the account.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub namespace: String,

    /// The namespace this User is in, which the rectangle in the namespaces
    /// bar draws. It is the person's rather than the session's, so this is
    /// what makes a step survive the tab it was taken in. Empty is a User who
    /// has stepped nowhere, which is the default project.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub standing: String,

    #[serde(default, deserialize_with = "null_is_default")]
    pub keys: Vec<KeyRecord>,

    #[serde(default, deserialize_with = "null_is_default")]
    pub accounts: Vec<AccountRecord>,

    pub created_at: i64,
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
    objects: Objects,
}

impl UserStore {
    /// Open the store at `location`.
    pub fn open(location: impl Into<String>) -> Result<Self> {
        let location = location.into();
        if location.contains('\'') {
            return Err(DuckdbError::BadName {
                which: Name::Location,
                value: location,
                why: Refusal::CarriesAQuote,
            });
        }

        Ok(Self {
            prefix: users_prefix(&location),
            objects: Objects::open(&location)?,
            location,
        })
    }

    /// The location URL this store was opened with.
    pub fn location(&self) -> &str {
        &self.location
    }

    /// Every User. An empty prefix is an empty list; a prefix that cannot be
    /// read is an error. A node with no Users is a node nobody owns yet, and
    /// `claimed()` reads it that way, so which of the two it is has to be a
    /// fact the location answered and never an empty list standing in.
    pub fn all(&self) -> Result<Vec<UserRecord>> {
        let mut users = Vec::new();
        for path in self.objects.list(Object::Users, &self.prefix)? {
            if !path.ends_with(".json") {
                continue;
            }
            let Some(bytes) = self.objects.get(Object::User, &path)? else {
                continue;
            };
            let record: UserRecord =
                serde_json::from_slice(&bytes).map_err(|source| DuckdbError::NotJSON {
                    what: Object::User,
                    path: path.clone(),
                    source,
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
            return Err(DuckdbError::BadName {
                which: Name::UserID,
                value: record.id.clone(),
                why: Refusal::NotAnObjectName,
            });
        }

        let body = serde_json::to_vec(record).map_err(|source| DuckdbError::NotSerializable {
            what: Object::User,
            id: record.id.clone(),
            source,
        })?;
        let path = format!("{}/{}.json", self.prefix, record.id);
        self.objects.put(Object::User, &path, body)
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
            display_name: "Tim de Facile".to_string(),
            email_addresses: vec!["tim@example.com".to_string()],
            phone_numbers: vec!["+31612345678".to_string()],
            level: "ROOT".to_string(),
            created_by: String::new(),
            disabled_by: String::new(),
            namespace: String::new(),
            standing: String::new(),
            keys: vec![KeyRecord {
                did: route.to_string(),
                origin: "BROWSER".to_string(),
            }],
            accounts: Vec::new(),
            created_at: 1,
        }
    }

    /// What Go marshals for a User with no keys, accounts or addresses yet: a
    /// nil slice is null, not []. This is the write that failed in production.
    #[test]
    fn a_null_list_reads_as_an_empty_one() {
        let body = r#"{"id":"US-USER-ENMNSVLD","display_name":"","email_addresses":null,
            "phone_numbers":null,"level":"ROOT","created_by":"","keys":null,"accounts":null,"created_at":1}"#;

        let record: UserRecord = serde_json::from_str(body).expect("a null list is an empty list");
        assert!(record.email_addresses.is_empty());
        assert!(record.phone_numbers.is_empty());
        assert!(record.keys.is_empty());
        assert!(record.accounts.is_empty());
    }

    /// A field Go writes that this record does not name is dropped on the way
    /// through, and the write that dropped it answers as though it landed. The
    /// rectangle moved, the page reloaded, and the person was in default again.
    #[test]
    fn a_step_survives_the_write_that_records_it() {
        let body = r#"{"id":"US-TIM-1","display_name":"Tim de Facile","email_addresses":null,
            "phone_numbers":null,"level":"ROOT","created_by":"","disabled_by":"",
            "standing":"pond","keys":null,"accounts":null,"created_at":1}"#;

        let record: UserRecord = serde_json::from_str(body).expect("a User Go wrote");
        assert_eq!(record.standing, "pond");

        let dir = tempfile::tempdir().expect("tempdir");
        let location = format!("file://{}", dir.path().display());
        let store = UserStore::open(&location).expect("open");
        store.put(&record).expect("put");

        let held = UserStore::open(&location)
            .expect("reopen")
            .all()
            .expect("all");
        assert_eq!(held.len(), 1);
        assert_eq!(
            held[0].standing, "pond",
            "the step was written and read back as nowhere"
        );
    }

    /// A field this record does not name is refused rather than dropped. Go
    /// adding one and Rust not is a write that answers as though it landed, and
    /// the person finds out weeks later that nothing was kept.
    #[test]
    fn a_field_this_record_does_not_name_is_refused() {
        let body = r#"{"id":"US-TIM-1","display_name":"","email_addresses":null,
            "phone_numbers":null,"level":"ROOT","created_by":"","keys":null,"accounts":null,
            "created_at":1,"favourite_colour":"green"}"#;

        let refused = serde_json::from_str::<UserRecord>(body);
        let Err(why) = refused else {
            panic!("a field nobody named was read and thrown away");
        };
        assert!(
            why.to_string().contains("favourite_colour"),
            "the refusal does not name the field it refused: {why}"
        );
    }

    /// Stepping nowhere is the absence of the field rather than an empty one,
    /// which is what Go's omitempty writes and what every record already held.
    #[test]
    fn standing_nowhere_is_read_from_a_record_that_never_named_it() {
        let body = r#"{"id":"US-TIM-1","display_name":"","email_addresses":null,
            "phone_numbers":null,"level":"ROOT","created_by":"","keys":null,"accounts":null,"created_at":1}"#;

        let record: UserRecord =
            serde_json::from_str(body).expect("a User written before standing");
        assert_eq!(record.standing, "");

        let written = serde_json::to_string(&record).expect("serialize");
        assert!(
            !written.contains("standing"),
            "an empty standing was written out"
        );
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
            binding: None,
        });
        store.put(&u).expect("put");

        let found = store
            .by_route("https://mastodon.example/@tim")
            .expect("by_route");
        assert_eq!(found.map(|f| f.id), Some("US-TIM-2".to_string()));
    }

    /// The binding that reached an account is kept as Go wrote it, and read
    /// back the same, so the node can ask about its signer again (ADR-031).
    #[test]
    fn an_accounts_binding_survives_the_round_trip() {
        let dir = tempfile::tempdir().expect("tempdir");
        let store = UserStore::open(format!("file://{}", dir.path().display())).expect("open");

        let binding = serde_json::json!({
            "claim": {"peer_pubkey_hex": "aa", "provider": "mastodon",
                      "canonical_id": "https://mastodon.example/@tim", "handle": null, "issued_at": 1},
            "signature_hex": "bb", "signer_pubkey_hex": "cc"
        });
        let mut u = user("US-TIM-5", "did:key:zBrowser");
        u.accounts.push(AccountRecord {
            provider: "mastodon".to_string(),
            canonical_id: "https://mastodon.example/@tim".to_string(),
            handle: "@tim@mastodon.example".to_string(),
            binding: Some(binding.clone()),
        });
        store.put(&u).expect("put");

        let read = store.all().expect("all").remove(0);
        assert_eq!(read.accounts[0].binding, Some(binding));

        // A record from before bindings were kept reads as one with none.
        let body = r#"{"id":"US-OLD","level":"ROOT","keys":null,
            "accounts":[{"provider":"mastodon","canonical_id":"https://mastodon.example/@old","handle":""}],"created_at":1}"#;
        let old: UserRecord = serde_json::from_str(body).expect("an old record still reads");
        assert_eq!(old.accounts[0].binding, None);
    }

    /// A second write replaces the object.
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
