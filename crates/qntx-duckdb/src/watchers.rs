//! Watchers for the parquet backend: a cold declaration and a hot tally,
//! which the SQLite schema keeps in one row and this does not.

use std::collections::HashMap;

use serde::{Deserialize, Serialize};

use crate::error::{DuckdbError, Result};

/// A watcher as declared. Mirrors the cold half of `storage.Watcher`
/// (`ats/storage/watcher_store.go:41`); the counters are deliberately absent.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct WatcherRecord {
    pub id: String,
    pub name: String,
    pub action_type: String,
    pub action_data: String,
    pub ax_query: String,
    pub max_fires_per_second: i64,
    pub enabled: bool,
    pub created_at: i64,
    pub updated_at: i64,
}

/// One thing that happened to a watcher. `error` is None for a fire.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct FireEvent {
    pub watcher_id: String,
    pub at_ms: i64,
    pub error: Option<String>,
}

/// What the counters used to hold, derived rather than stored.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct Tally {
    pub fire_count: i64,
    pub last_fired_at: Option<i64>,
    pub error_count: i64,
    pub last_error: Option<String>,
}

/// Watchers held at a storage location.
pub struct WatcherStore {
    location: String,
    prefix: String,
    fires_prefix: String,
    conn: duckdb::Connection,
    by_id: HashMap<String, WatcherRecord>,
    tallies: HashMap<String, Tally>,
    pending: Vec<FireEvent>,
}

impl WatcherStore {
    pub fn open(_location: impl Into<String>) -> Result<Self> {
        unimplemented!("WatcherStore::open")
    }

    pub fn location(&self) -> &str {
        &self.location
    }

    /// Declare a watcher, replacing any under the same id. Writes through.
    pub fn put(&mut self, _record: WatcherRecord) -> Result<()> {
        unimplemented!("WatcherStore::put")
    }

    pub fn get(&self, _id: &str) -> Option<&WatcherRecord> {
        unimplemented!("WatcherStore::get")
    }

    /// Every declaration, ordered by creation so runs are comparable.
    pub fn list(&self) -> Vec<WatcherRecord> {
        unimplemented!("WatcherStore::list")
    }

    /// Withdraw a declaration. The fires it emitted stay.
    pub fn delete(&mut self, _id: &str) -> Result<bool> {
        unimplemented!("WatcherStore::delete")
    }

    /// Note a fire. Buffered, because a watcher's rate limit is per second and
    /// an object rewrite per fire is the cost this shape exists to refuse.
    pub fn record_fire(&mut self, _id: &str, _at_ms: i64) {
        unimplemented!("WatcherStore::record_fire")
    }

    pub fn record_error(&mut self, _id: &str, _at_ms: i64, _message: &str) {
        unimplemented!("WatcherStore::record_error")
    }

    /// Events waiting to be written.
    pub fn buffered(&self) -> usize {
        self.pending.len()
    }

    /// Write the buffered events as one file and clear the buffer.
    pub fn flush(&mut self) -> Result<()> {
        unimplemented!("WatcherStore::flush")
    }

    /// The counters, as the UI reads them.
    pub fn tally(&self, _id: &str) -> Tally {
        unimplemented!("WatcherStore::tally")
    }
}

/// The directory holding watcher declarations.
fn watcher_prefix(location: &str) -> String {
    let base = location.strip_prefix("file://").unwrap_or(location);
    format!("{}/watchers", base.trim_end_matches('/'))
}

/// The directory holding fire events. Not the attestation store: a fire that
/// went through `CreateAttestation` would reach `NotifyObservers`, and a
/// watcher with an empty filter matches everything (`engine_match.go:412`).
fn fires_prefix(location: &str) -> String {
    let base = location.strip_prefix("file://").unwrap_or(location);
    format!("{}/watcher_fires", base.trim_end_matches('/'))
}

#[cfg(test)]
mod tests {
    //! Personas: Tim (happy path), Spike (edge cases), Jenny (complex).

    use super::*;

    fn declaration(id: &str) -> WatcherRecord {
        WatcherRecord {
            id: id.to_string(),
            name: format!("watcher {id}"),
            action_type: "webhook".to_string(),
            action_data: "http://127.0.0.1:1/hook".to_string(),
            ax_query: "thing:happened".to_string(),
            max_fires_per_second: 8,
            enabled: true,
            created_at: 1_700_000_000_000,
            updated_at: 1_700_000_000_000,
        }
    }

    fn at(dir: &tempfile::TempDir) -> String {
        format!("file://{}", dir.path().display())
    }

    fn store(dir: &tempfile::TempDir) -> WatcherStore {
        WatcherStore::open(at(dir)).unwrap()
    }

    fn fire_files(dir: &tempfile::TempDir) -> usize {
        std::fs::read_dir(fires_prefix(&at(dir)))
            .map(|d| d.count())
            .unwrap_or(0)
    }

    mod tim {
        use super::*;

        #[test]
        fn declaration_survives_reopen() {
            let dir = tempfile::tempdir().unwrap();
            let mut s = store(&dir);
            s.put(declaration("w1")).unwrap();

            assert_eq!(store(&dir).get("w1"), Some(&declaration("w1")));
        }

        #[test]
        fn a_fire_is_counted() {
            let dir = tempfile::tempdir().unwrap();
            let mut s = store(&dir);
            s.put(declaration("w1")).unwrap();

            s.record_fire("w1", 1_700_000_001_000);

            let t = s.tally("w1");
            assert_eq!(t.fire_count, 1);
            assert_eq!(t.last_fired_at, Some(1_700_000_001_000));
        }

        // "a watcher is cold declaration plus hot tally is OK"
        #[test]
        fn the_tally_survives_reopen() {
            let dir = tempfile::tempdir().unwrap();
            let mut s = store(&dir);
            s.put(declaration("w1")).unwrap();
            s.record_fire("w1", 1_700_000_001_000);
            s.record_fire("w1", 1_700_000_002_000);
            s.flush().unwrap();

            let t = store(&dir).tally("w1");
            assert_eq!(t.fire_count, 2);
            assert_eq!(t.last_fired_at, Some(1_700_000_002_000));
        }

        #[test]
        fn an_error_is_counted_separately_from_a_fire() {
            let dir = tempfile::tempdir().unwrap();
            let mut s = store(&dir);
            s.put(declaration("w1")).unwrap();

            s.record_fire("w1", 1_700_000_001_000);
            s.record_error("w1", 1_700_000_002_000, "webhook returned 500");

            let t = s.tally("w1");
            assert_eq!(t.fire_count, 1);
            assert_eq!(t.error_count, 1);
            assert_eq!(t.last_error.as_deref(), Some("webhook returned 500"));
        }

        #[test]
        fn a_deleted_watcher_is_gone() {
            let dir = tempfile::tempdir().unwrap();
            let mut s = store(&dir);
            s.put(declaration("w1")).unwrap();

            assert!(s.delete("w1").unwrap());
            assert!(store(&dir).get("w1").is_none());
        }
    }

    mod spike {
        use super::*;

        // A rate limit is per second, so a fire is a per-second event. An
        // object rewrite each time is what this shape exists to refuse.
        #[test]
        fn recording_a_fire_touches_no_storage() {
            let dir = tempfile::tempdir().unwrap();
            let mut s = store(&dir);
            s.put(declaration("w1")).unwrap();
            let before = fire_files(&dir);

            for i in 0..8 {
                s.record_fire("w1", 1_700_000_000_000 + i);
            }

            assert_eq!(fire_files(&dir), before, "eight fires reached storage");
            assert_eq!(s.buffered(), 8);
        }

        // "tally private stream is OK"
        #[test]
        fn fires_land_under_their_own_prefix() {
            assert_eq!(
                fires_prefix("file:///var/lib/qntx/parquet"),
                "/var/lib/qntx/parquet/watcher_fires"
            );
            assert_eq!(
                watcher_prefix("s3://bucket/prefix/"),
                "s3://bucket/prefix/watchers"
            );
        }

        #[test]
        fn a_location_that_never_declared_a_watcher_is_empty() {
            let dir = tempfile::tempdir().unwrap();
            assert!(store(&dir).list().is_empty());
        }

        #[test]
        fn an_unknown_watcher_has_an_empty_tally() {
            let dir = tempfile::tempdir().unwrap();
            assert_eq!(store(&dir).tally("never-declared"), Tally::default());
        }

        #[test]
        fn deleting_an_unknown_watcher_reports_it() {
            let dir = tempfile::tempdir().unwrap();
            let mut s = store(&dir);
            assert!(!s.delete("nobody").unwrap());
        }

        /// A path is interpolated into SQL, so a quote would end the literal.
        #[test]
        fn quoted_location_is_refused() {
            match WatcherStore::open("file:///tmp/it's-here") {
                Err(DuckdbError::Backend(msg)) => assert!(msg.contains("quote")),
                Err(other) => panic!("expected a rejection, got {other:?}"),
                Ok(s) => panic!("opened a quoted location at {}", s.location()),
            }
        }

        #[test]
        fn flushing_an_empty_buffer_writes_nothing() {
            let dir = tempfile::tempdir().unwrap();
            let mut s = store(&dir);
            s.flush().unwrap();
            assert_eq!(fire_files(&dir), 0);
            assert_eq!(s.buffered(), 0);
        }
    }

    mod jenny {
        use super::*;

        // Eight a second for two minutes, which a busy watcher reaches.
        #[test]
        fn a_thousand_fires_become_one_file() {
            let dir = tempfile::tempdir().unwrap();
            let mut s = store(&dir);
            s.put(declaration("w1")).unwrap();

            for i in 0..1000 {
                s.record_fire("w1", 1_700_000_000_000 + i);
            }
            s.flush().unwrap();

            assert_eq!(fire_files(&dir), 1);
            assert_eq!(s.tally("w1").fire_count, 1000);
        }

        #[test]
        fn two_flushes_accumulate_rather_than_replace() {
            let dir = tempfile::tempdir().unwrap();
            let mut s = store(&dir);
            s.put(declaration("w1")).unwrap();

            s.record_fire("w1", 1_700_000_001_000);
            s.flush().unwrap();
            s.record_fire("w1", 1_700_000_002_000);
            s.flush().unwrap();

            assert_eq!(store(&dir).tally("w1").fire_count, 2);
        }

        #[test]
        fn one_watchers_fires_do_not_count_for_another() {
            let dir = tempfile::tempdir().unwrap();
            let mut s = store(&dir);
            s.put(declaration("w1")).unwrap();
            s.put(declaration("w2")).unwrap();

            s.record_fire("w1", 1_700_000_001_000);
            s.record_fire("w1", 1_700_000_002_000);
            s.record_fire("w2", 1_700_000_003_000);
            s.flush().unwrap();

            let reopened = store(&dir);
            assert_eq!(reopened.tally("w1").fire_count, 2);
            assert_eq!(reopened.tally("w2").fire_count, 1);
        }

        // A declaration is a thing you withdraw; a fire is a thing that
        // happened. Withdrawing the first does not unhappen the second.
        #[test]
        fn deleting_a_declaration_leaves_its_fires() {
            let dir = tempfile::tempdir().unwrap();
            let mut s = store(&dir);
            s.put(declaration("w1")).unwrap();
            s.record_fire("w1", 1_700_000_001_000);
            s.flush().unwrap();

            s.delete("w1").unwrap();

            let reopened = store(&dir);
            assert!(reopened.get("w1").is_none());
            assert_eq!(reopened.tally("w1").fire_count, 1);
        }

        // Changing what a watcher watches must not reset what it has done.
        #[test]
        fn redeclaring_keeps_the_tally() {
            let dir = tempfile::tempdir().unwrap();
            let mut s = store(&dir);
            s.put(declaration("w1")).unwrap();
            s.record_fire("w1", 1_700_000_001_000);
            s.flush().unwrap();

            let mut changed = declaration("w1");
            changed.ax_query = "other:thing".to_string();
            changed.updated_at = 1_700_000_005_000;
            s.put(changed).unwrap();

            assert_eq!(s.tally("w1").fire_count, 1);
        }
    }
}
