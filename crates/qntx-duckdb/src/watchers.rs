//! Watchers for the parquet backend: a cold declaration and a hot tally,
//! which the SQLite schema keeps in one row and this does not.

use std::collections::HashMap;

use serde::{Deserialize, Serialize};

use crate::error::{DuckdbError, Result};
use crate::{is_remote, remote_setup_sql};

/// A watcher as declared. Mirrors the cold half of `storage.Watcher`
/// (`ats/storage/watcher_store.go:41`); the counters are deliberately absent.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
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
    /// The AX filter and the attribute filters, as the JSON Go already speaks.
    /// Nested shapes that Rust has no reason to know the inside of.
    pub filter_json: String,
    pub attribute_filters_json: String,
    pub semantic_query: String,
    pub semantic_threshold: f64,
    pub semantic_cluster_id: Option<i64>,
    pub upstream_semantic_query: String,
    pub upstream_semantic_threshold: f64,
}

/// One thing that happened to a watcher. `error` is None for a fire.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct FireEvent {
    pub watcher_id: String,
    pub at_ms: i64,
    pub error: Option<String>,
}

/// What the counters used to hold, derived rather than stored.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
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
    seq: u64,
}

impl WatcherStore {
    /// Open at `location`, loading the declarations there and folding the fire
    /// stream into the tallies it describes.
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
            prefix: watcher_prefix(&location),
            fires_prefix: fires_prefix(&location),
            location,
            conn,
            by_id: HashMap::new(),
            tallies: HashMap::new(),
            pending: Vec::new(),
            seq: 0,
        };
        store.load_declarations()?;
        store.load_tallies()?;
        Ok(store)
    }

    pub fn location(&self) -> &str {
        &self.location
    }

    /// Declare a watcher, replacing any under the same id. Writes through.
    pub fn put(&mut self, record: WatcherRecord) -> Result<()> {
        self.write_object(&record, false)?;
        self.by_id.insert(record.id.clone(), record);
        Ok(())
    }

    pub fn get(&self, id: &str) -> Option<&WatcherRecord> {
        self.by_id.get(id)
    }

    /// Every declaration, ordered by creation so runs are comparable.
    pub fn list(&self) -> Vec<WatcherRecord> {
        let mut all: Vec<WatcherRecord> = self.by_id.values().cloned().collect();
        all.sort_by(|a, b| a.created_at.cmp(&b.created_at).then(a.id.cmp(&b.id)));
        all
    }

    /// Withdraw a declaration; the fires it emitted stay. A tombstone, because
    /// DuckDB writes objects and does not remove them — deleting the file
    /// would work locally and leave the watcher standing on `s3://`.
    pub fn delete(&mut self, id: &str) -> Result<bool> {
        let record = match self.by_id.remove(id) {
            Some(record) => record,
            None => return Ok(false),
        };
        self.write_object(&record, true)?;
        Ok(true)
    }

    /// Note a fire. Buffered, because a watcher's rate limit is per second and
    /// an object rewrite per fire is the cost this shape exists to refuse.
    pub fn record_fire(&mut self, id: &str, at_ms: i64) {
        self.note(FireEvent {
            watcher_id: id.to_string(),
            at_ms,
            error: None,
        });
    }

    pub fn record_error(&mut self, id: &str, at_ms: i64, message: &str) {
        self.note(FireEvent {
            watcher_id: id.to_string(),
            at_ms,
            error: Some(message.to_string()),
        });
    }

    /// Buffer an event and move its tally, so a reader sees the fire before
    /// the flush that makes it durable.
    fn note(&mut self, event: FireEvent) {
        let tally = self.tallies.entry(event.watcher_id.clone()).or_default();
        match &event.error {
            None => {
                tally.fire_count += 1;
                if tally.last_fired_at.is_none_or(|prior| event.at_ms >= prior) {
                    tally.last_fired_at = Some(event.at_ms);
                }
            }
            Some(message) => {
                tally.error_count += 1;
                tally.last_error = Some(message.clone());
            }
        }
        self.pending.push(event);
    }

    /// Events waiting to be written.
    pub fn buffered(&self) -> usize {
        self.pending.len()
    }

    /// Write the buffered events as one file and clear the buffer.
    pub fn flush(&mut self) -> Result<()> {
        if self.pending.is_empty() {
            return Ok(());
        }
        if !is_remote(&self.location) {
            let _ = std::fs::create_dir_all(&self.fires_prefix);
        }

        self.conn.execute_batch(
            "CREATE OR REPLACE TEMP TABLE fire_batch \
             (watcher_id VARCHAR, at_ms BIGINT, error VARCHAR)",
        )?;
        {
            let mut stmt = self
                .conn
                .prepare("INSERT INTO fire_batch VALUES (?, ?, ?)")?;
            for event in &self.pending {
                stmt.execute(duckdb::params![event.watcher_id, event.at_ms, event.error])
                    .map_err(|e| {
                        DuckdbError::Backend(format!("failed to buffer a fire event: {e}"))
                    })?;
            }
        }

        self.seq += 1;
        let path = format!("{}/{}.parquet", self.fires_prefix, self.batch_name());
        self.conn
            .execute_batch(&format!(
                "COPY fire_batch TO '{path}' (FORMAT PARQUET); DROP TABLE fire_batch"
            ))
            .map_err(|e| {
                DuckdbError::Backend(format!("failed to write fire events to {path}: {e}"))
            })?;

        self.pending.clear();
        Ok(())
    }

    /// A name no earlier flush can hold. The counter separates two flushes
    /// inside one millisecond.
    fn batch_name(&self) -> String {
        let millis = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map(|d| d.as_millis())
            .unwrap_or(0);
        format!("{millis}-{}", self.seq)
    }

    /// The counters, as the UI reads them.
    pub fn tally(&self, id: &str) -> Tally {
        self.tallies.get(id).cloned().unwrap_or_default()
    }

    /// Read every declaration object, skipping the withdrawn. `read_json`
    /// errors on a glob that matches nothing, which is a location that has
    /// never declared a watcher — empty, not broken.
    fn load_declarations(&mut self) -> Result<()> {
        let sql = format!(
            "SELECT id, name, action_type, action_data, ax_query, \
                    max_fires_per_second, enabled, created_at, updated_at, \
                    filter_json, attribute_filters_json, semantic_query, \
                    semantic_threshold, semantic_cluster_id, \
                    upstream_semantic_query, upstream_semantic_threshold, deleted \
             FROM read_json('{}/*.json', columns = {{ \
                 id: 'VARCHAR', name: 'VARCHAR', action_type: 'VARCHAR', \
                 action_data: 'VARCHAR', ax_query: 'VARCHAR', \
                 max_fires_per_second: 'BIGINT', enabled: 'BOOLEAN', \
                 created_at: 'BIGINT', updated_at: 'BIGINT', \
                 filter_json: 'VARCHAR', attribute_filters_json: 'VARCHAR', \
                 semantic_query: 'VARCHAR', semantic_threshold: 'DOUBLE', \
                 semantic_cluster_id: 'BIGINT', \
                 upstream_semantic_query: 'VARCHAR', \
                 upstream_semantic_threshold: 'DOUBLE', deleted: 'BOOLEAN' }})",
            self.prefix
        );

        let mut stmt = match self.conn.prepare(&sql) {
            Ok(stmt) => stmt,
            Err(_) => return Ok(()),
        };
        let rows = match stmt.query_map([], |row| {
            let withdrawn: bool = row.get(16)?;
            Ok((
                withdrawn,
                WatcherRecord {
                    id: row.get(0)?,
                    name: row.get(1)?,
                    action_type: row.get(2)?,
                    action_data: row.get(3)?,
                    ax_query: row.get(4)?,
                    max_fires_per_second: row.get(5)?,
                    enabled: row.get(6)?,
                    created_at: row.get(7)?,
                    updated_at: row.get(8)?,
                    filter_json: row.get(9)?,
                    attribute_filters_json: row.get(10)?,
                    semantic_query: row.get(11)?,
                    semantic_threshold: row.get(12)?,
                    semantic_cluster_id: row.get(13)?,
                    upstream_semantic_query: row.get(14)?,
                    upstream_semantic_threshold: row.get(15)?,
                },
            ))
        }) {
            Ok(rows) => rows,
            Err(_) => return Ok(()),
        };

        for row in rows {
            let (withdrawn, record) = row.map_err(|e| {
                DuckdbError::Backend(format!(
                    "failed to read a watcher object under {}: {e}",
                    self.prefix
                ))
            })?;
            if !withdrawn {
                self.by_id.insert(record.id.clone(), record);
            }
        }
        Ok(())
    }

    /// Fold the fire stream into one row per watcher. This query is why the
    /// tally is not a stored column.
    fn load_tallies(&mut self) -> Result<()> {
        let sql = format!(
            "SELECT watcher_id, \
                    count(*) FILTER (WHERE error IS NULL), \
                    max(at_ms) FILTER (WHERE error IS NULL), \
                    count(*) FILTER (WHERE error IS NOT NULL), \
                    arg_max(error, at_ms) FILTER (WHERE error IS NOT NULL) \
             FROM read_parquet('{}/*.parquet') GROUP BY watcher_id",
            self.fires_prefix
        );

        let mut stmt = match self.conn.prepare(&sql) {
            Ok(stmt) => stmt,
            Err(_) => return Ok(()),
        };
        let rows = match stmt.query_map([], |row| {
            Ok((
                row.get::<_, String>(0)?,
                Tally {
                    fire_count: row.get(1)?,
                    last_fired_at: row.get(2)?,
                    error_count: row.get(3)?,
                    last_error: row.get(4)?,
                },
            ))
        }) {
            Ok(rows) => rows,
            Err(_) => return Ok(()),
        };

        for row in rows {
            let (id, tally) = row.map_err(|e| {
                DuckdbError::Backend(format!(
                    "failed to read fire events under {}: {e}",
                    self.fires_prefix
                ))
            })?;
            self.tallies.insert(id, tally);
        }
        Ok(())
    }

    /// Write one declaration to its own object, replacing what was there.
    /// `withdrawn` writes the tombstone `delete` relies on.
    fn write_object(&self, record: &WatcherRecord, withdrawn: bool) -> Result<()> {
        if !is_remote(&self.location) {
            let _ = std::fs::create_dir_all(&self.prefix);
        }

        let path = format!("{}/{}.json", self.prefix, record.id);
        let sql = format!(
            "COPY (SELECT ? AS id, ? AS name, ? AS action_type, ? AS action_data, \
                          ? AS ax_query, ?::BIGINT AS max_fires_per_second, \
                          ?::BOOLEAN AS enabled, ?::BIGINT AS created_at, \
                          ?::BIGINT AS updated_at, ? AS filter_json, \
                          ? AS attribute_filters_json, ? AS semantic_query, \
                          ?::DOUBLE AS semantic_threshold, \
                          ?::BIGINT AS semantic_cluster_id, \
                          ? AS upstream_semantic_query, \
                          ?::DOUBLE AS upstream_semantic_threshold, \
                          ?::BOOLEAN AS deleted) \
             TO '{path}' (FORMAT JSON)"
        );

        self.conn
            .execute(
                &sql,
                duckdb::params![
                    record.id,
                    record.name,
                    record.action_type,
                    record.action_data,
                    record.ax_query,
                    record.max_fires_per_second,
                    record.enabled,
                    record.created_at,
                    record.updated_at,
                    record.filter_json,
                    record.attribute_filters_json,
                    record.semantic_query,
                    record.semantic_threshold,
                    record.semantic_cluster_id,
                    record.upstream_semantic_query,
                    record.upstream_semantic_threshold,
                    withdrawn,
                ],
            )
            .map_err(|e| {
                DuckdbError::Backend(format!("failed to write watcher object {path}: {e}"))
            })?;
        Ok(())
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
            filter_json: r#"{"predicates":["thing:happened"]}"#.to_string(),
            attribute_filters_json: "[]".to_string(),
            semantic_query: String::new(),
            semantic_threshold: 0.0,
            semantic_cluster_id: None,
            upstream_semantic_query: String::new(),
            upstream_semantic_threshold: 0.0,
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
