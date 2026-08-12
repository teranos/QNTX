//! Schedules for the parquet backend, in the shape ADR-028 gives them.

use std::collections::HashMap;

use qntx_proto::{ScheduleDeclaration, ScheduleProgress, ScheduleTick};

use crate::error::{DuckdbError, Result};
use crate::{is_remote, remote_setup_sql};

/// What the ticks derive. Zero means never, which epoch milliseconds can
/// afford to say because no real tick lands on 1970.
pub type Progress = ScheduleProgress;

/// Schedules held at a storage location.
pub struct ScheduleStore {
    location: String,
    prefix: String,
    ticks_prefix: String,
    conn: duckdb::Connection,
    by_id: HashMap<String, ScheduleDeclaration>,
    progress: HashMap<String, Progress>,
    pending: Vec<ScheduleTick>,
    seq: u64,
}

impl ScheduleStore {
    /// Open at `location`, loading the declarations there and folding the tick
    /// stream into the progress it describes.
    pub fn open(location: impl Into<String>, namespace: impl AsRef<str>) -> Result<Self> {
        let location = location.into();
        let namespace = namespace.as_ref();
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
            prefix: schedule_prefix(&location, namespace),
            ticks_prefix: ticks_prefix(&location, namespace),
            location,
            conn,
            by_id: HashMap::new(),
            progress: HashMap::new(),
            pending: Vec::new(),
            seq: 0,
        };
        store.load_declarations()?;
        store.load_progress()?;
        Ok(store)
    }

    pub fn location(&self) -> &str {
        &self.location
    }

    /// Declare a schedule, replacing any under the same id. Writes through.
    pub fn put(&mut self, declaration: ScheduleDeclaration) -> Result<()> {
        self.write_object(&declaration, false)?;
        self.by_id.insert(declaration.id.clone(), declaration);
        Ok(())
    }

    pub fn get(&self, id: &str) -> Option<&ScheduleDeclaration> {
        self.by_id.get(id)
    }

    /// The live declaration for a handler. Two on one handler is a declaration
    /// bug, so the earliest wins and stays stable across runs.
    pub fn active_by_handler(&self, handler_name: &str) -> Option<ScheduleDeclaration> {
        self.list()
            .into_iter()
            .find(|d| d.handler_name == handler_name && d.state != "deleted")
    }

    /// Every declaration, ordered by creation so runs are comparable.
    pub fn list(&self) -> Vec<ScheduleDeclaration> {
        let mut all: Vec<ScheduleDeclaration> = self.by_id.values().cloned().collect();
        all.sort_by(|a, b| a.created_at_ms.cmp(&b.created_at_ms).then(a.id.cmp(&b.id)));
        all
    }

    /// Withdraw a declaration; the ticks it emitted stay. A tombstone, because
    /// DuckDB writes objects and does not remove them — deleting the file
    /// would work locally and leave the schedule standing on `s3://`.
    pub fn delete(&mut self, id: &str) -> Result<bool> {
        let declaration = match self.by_id.remove(id) {
            Some(declaration) => declaration,
            None => return Ok(false),
        };
        self.write_object(&declaration, true)?;
        Ok(true)
    }

    /// What is owed at `now_ms`. A paused schedule keeps its next run, so
    /// resuming does not lose where it had got to.
    pub fn due(&self, now_ms: i64) -> Vec<ScheduleDeclaration> {
        self.list()
            .into_iter()
            .filter(|d| d.state == "active")
            .filter(|d| self.next_run_at(&d.id).is_some_and(|next| next <= now_ms))
            .collect()
    }

    /// The soonest run owed by any active schedule.
    pub fn next_scheduled(&self) -> Option<ScheduleDeclaration> {
        self.list()
            .into_iter()
            .filter(|d| d.state == "active")
            .filter_map(|d| self.next_run_at(&d.id).map(|next| (next, d)))
            .min_by_key(|(next, d)| (*next, d.id.clone()))
            .map(|(_, d)| d)
    }

    /// Note a run. Buffered, because a tick per schedule per interval is a
    /// stream, and an object rewrite per tick is what this shape refuses.
    pub fn record_run(&mut self, id: &str, at_ms: i64, execution_id: &str, next_run_at_ms: i64) {
        self.note(ScheduleTick {
            schedule_id: id.to_string(),
            at_ms,
            execution_id: execution_id.to_string(),
            next_run_at_ms,
        });
    }

    /// Move the next run without a run having happened — a force trigger.
    pub fn reschedule(&mut self, id: &str, at_ms: i64, next_run_at_ms: i64) {
        self.note(ScheduleTick {
            schedule_id: id.to_string(),
            at_ms,
            execution_id: String::new(),
            next_run_at_ms,
        });
    }

    /// Buffer a tick and move its progress, so a reader sees the run before
    /// the flush that makes it durable.
    fn note(&mut self, tick: ScheduleTick) {
        let progress = self.progress.entry(tick.schedule_id.clone()).or_default();
        let later = tick.at_ms >= progress.last_run_at_ms;
        if !tick.execution_id.is_empty() {
            progress.run_count += 1;
            if later {
                progress.last_run_at_ms = tick.at_ms;
                progress.last_execution_id = tick.execution_id.clone();
            }
        }
        if later {
            progress.next_run_at_ms = tick.next_run_at_ms;
        }
        self.pending.push(tick);
    }

    /// Ticks waiting to be written.
    pub fn buffered(&self) -> usize {
        self.pending.len()
    }

    /// Write the buffered ticks as one file and clear the buffer.
    pub fn flush(&mut self) -> Result<()> {
        if self.pending.is_empty() {
            return Ok(());
        }
        if !is_remote(&self.location) {
            let _ = std::fs::create_dir_all(&self.ticks_prefix);
        }

        self.conn.execute_batch(
            "CREATE OR REPLACE TEMP TABLE tick_batch \
             (schedule_id VARCHAR, at_ms BIGINT, execution_id VARCHAR, next_run_at_ms BIGINT)",
        )?;
        {
            let mut stmt = self
                .conn
                .prepare("INSERT INTO tick_batch VALUES (?, ?, ?, ?)")?;
            for tick in &self.pending {
                let execution_id = if tick.execution_id.is_empty() {
                    None
                } else {
                    Some(tick.execution_id.as_str())
                };
                stmt.execute(duckdb::params![
                    tick.schedule_id,
                    tick.at_ms,
                    execution_id,
                    tick.next_run_at_ms
                ])
                .map_err(|e| DuckdbError::Backend(format!("failed to buffer a tick: {e}")))?;
            }
        }

        self.seq += 1;
        let path = format!("{}/{}.parquet", self.ticks_prefix, self.batch_name());
        self.conn
            .execute_batch(&format!(
                "COPY tick_batch TO '{path}' (FORMAT PARQUET); DROP TABLE tick_batch"
            ))
            .map_err(|e| DuckdbError::Backend(format!("failed to write ticks to {path}: {e}")))?;

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

    /// What the ticks derive, as the UI reads it.
    pub fn progress(&self, id: &str) -> Progress {
        self.progress.get(id).cloned().unwrap_or_default()
    }

    /// When this schedule next runs. The ticks say, until there are none and
    /// the declaration's first run is the answer.
    pub fn next_run_at(&self, id: &str) -> Option<i64> {
        match self.progress.get(id).map(|p| p.next_run_at_ms) {
            Some(next) if next != 0 => Some(next),
            _ => self.by_id.get(id).map(|d| d.first_run_at_ms),
        }
    }

    /// Read every declaration object, skipping the withdrawn. `read_json`
    /// errors on a glob matching nothing, which is a location that has never
    /// declared a schedule — empty, not broken.
    fn load_declarations(&mut self) -> Result<()> {
        let sql = format!(
            "SELECT id, ats_code, handler_name, payload, source_url, \
                    interval_seconds, state, created_from_doc, metadata, \
                    created_at_ms, first_run_at_ms, deleted \
             FROM read_json('{}/*.json', columns = {{ \
                 id: 'VARCHAR', ats_code: 'VARCHAR', handler_name: 'VARCHAR', \
                 payload: 'VARCHAR', source_url: 'VARCHAR', \
                 interval_seconds: 'INTEGER', state: 'VARCHAR', \
                 created_from_doc: 'VARCHAR', metadata: 'VARCHAR', \
                 created_at_ms: 'BIGINT', first_run_at_ms: 'BIGINT', \
                 deleted: 'BOOLEAN' }})",
            self.prefix
        );

        let mut stmt = match self.conn.prepare(&sql) {
            Ok(stmt) => stmt,
            Err(_) => return Ok(()),
        };
        let rows = match stmt.query_map([], |row| {
            let withdrawn: bool = row.get(11)?;
            let payload: String = row.get(3)?;
            Ok((
                withdrawn,
                ScheduleDeclaration {
                    id: row.get(0)?,
                    ats_code: row.get(1)?,
                    handler_name: row.get(2)?,
                    payload: payload.into_bytes(),
                    source_url: row.get(4)?,
                    interval_seconds: row.get(5)?,
                    state: row.get(6)?,
                    created_from_doc: row.get(7)?,
                    metadata: row.get(8)?,
                    created_at_ms: row.get(9)?,
                    first_run_at_ms: row.get(10)?,
                },
            ))
        }) {
            Ok(rows) => rows,
            Err(_) => return Ok(()),
        };

        for row in rows {
            let (withdrawn, declaration) = row.map_err(|e| {
                DuckdbError::Backend(format!(
                    "failed to read a schedule object under {}: {e}",
                    self.prefix
                ))
            })?;
            if !withdrawn {
                self.by_id.insert(declaration.id.clone(), declaration);
            }
        }
        Ok(())
    }

    /// Fold the tick stream into one row per schedule. This query is why next
    /// run and last run are not stored columns.
    fn load_progress(&mut self) -> Result<()> {
        let sql = format!(
            "SELECT schedule_id, \
                    count(*) FILTER (WHERE execution_id IS NOT NULL), \
                    max(at_ms) FILTER (WHERE execution_id IS NOT NULL), \
                    arg_max(execution_id, at_ms) FILTER (WHERE execution_id IS NOT NULL), \
                    arg_max(next_run_at_ms, at_ms) \
             FROM read_parquet('{}/*.parquet') GROUP BY schedule_id",
            self.ticks_prefix
        );

        let mut stmt = match self.conn.prepare(&sql) {
            Ok(stmt) => stmt,
            Err(_) => return Ok(()),
        };
        let rows = match stmt.query_map([], |row| {
            Ok((
                row.get::<_, String>(0)?,
                Progress {
                    run_count: row.get(1)?,
                    last_run_at_ms: row.get::<_, Option<i64>>(2)?.unwrap_or(0),
                    last_execution_id: row.get::<_, Option<String>>(3)?.unwrap_or_default(),
                    next_run_at_ms: row.get::<_, Option<i64>>(4)?.unwrap_or(0),
                },
            ))
        }) {
            Ok(rows) => rows,
            Err(_) => return Ok(()),
        };

        for row in rows {
            let (id, progress) = row.map_err(|e| {
                DuckdbError::Backend(format!(
                    "failed to read ticks under {}: {e}",
                    self.ticks_prefix
                ))
            })?;
            self.progress.insert(id, progress);
        }
        Ok(())
    }

    /// Write one declaration to its own object, replacing what was there.
    /// `withdrawn` writes the tombstone `delete` relies on.
    fn write_object(&self, declaration: &ScheduleDeclaration, withdrawn: bool) -> Result<()> {
        if !is_remote(&self.location) {
            let _ = std::fs::create_dir_all(&self.prefix);
        }

        let path = format!("{}/{}.json", self.prefix, declaration.id);
        let sql = format!(
            "COPY (SELECT ? AS id, ? AS ats_code, ? AS handler_name, \
                          ? AS payload, ? AS source_url, \
                          ?::INTEGER AS interval_seconds, ? AS state, \
                          ? AS created_from_doc, ? AS metadata, \
                          ?::BIGINT AS created_at_ms, ?::BIGINT AS first_run_at_ms, \
                          ?::BOOLEAN AS deleted) \
             TO '{path}' (FORMAT JSON)"
        );

        let payload = String::from_utf8_lossy(&declaration.payload).to_string();
        self.conn
            .execute(
                &sql,
                duckdb::params![
                    declaration.id,
                    declaration.ats_code,
                    declaration.handler_name,
                    payload,
                    declaration.source_url,
                    declaration.interval_seconds,
                    declaration.state,
                    declaration.created_from_doc,
                    declaration.metadata,
                    declaration.created_at_ms,
                    declaration.first_run_at_ms,
                    withdrawn,
                ],
            )
            .map_err(|e| {
                DuckdbError::Backend(format!("failed to write schedule object {path}: {e}"))
            })?;
        Ok(())
    }
}

/// The directory holding schedule declarations. Named for the table it stands
/// in for, because `make parity` pairs a backend's prefix with a SQLite table
/// by name and a second name would read as a second thing.
/// A schedule created in a namespace stays there, so its prefix is that
/// namespace and never another.
fn schedule_prefix(location: &str, namespace: &str) -> String {
    crate::namespace::prefix(location, namespace, "scheduled_pulse_jobs")
}

/// The directory holding ticks. Not the attestation store, for the same reason
/// watcher fires are not: a tick through `CreateAttestation` would reach
/// `NotifyObservers` and wake every empty-filter watcher.
fn ticks_prefix(location: &str, namespace: &str) -> String {
    crate::namespace::prefix(location, namespace, "schedule_ticks")
}

#[cfg(test)]
mod tests {
    use super::*;

    const NS: &str = "did:key:ztestnamespace";

    fn location(name: &str) -> String {
        let dir = std::env::temp_dir().join(format!(
            "qntx-schedules-{name}-{}-{:?}",
            std::process::id(),
            std::thread::current().id()
        ));
        std::fs::remove_dir_all(&dir).ok();
        std::fs::create_dir_all(&dir).unwrap();
        format!("file://{}", dir.display())
    }

    fn declaration(id: &str, handler: &str, interval: i32) -> ScheduleDeclaration {
        ScheduleDeclaration {
            id: id.to_string(),
            ats_code: String::new(),
            handler_name: handler.to_string(),
            payload: Vec::new(),
            source_url: String::new(),
            interval_seconds: interval,
            state: "active".to_string(),
            created_from_doc: String::new(),
            metadata: String::new(),
            created_at_ms: 1_000,
            first_run_at_ms: 2_000,
        }
    }

    #[test]
    fn a_declared_schedule_is_read_back() {
        let mut store = ScheduleStore::open(location("declared"), NS).unwrap();
        store.put(declaration("s1", "capy.renew", 864_000)).unwrap();

        let read = store.get("s1").expect("the declaration is there");
        assert_eq!(read.handler_name, "capy.renew");
        assert_eq!(read.interval_seconds, 864_000);
    }

    #[test]
    fn a_schedule_that_never_ran_is_owed_its_first_run() {
        let mut store = ScheduleStore::open(location("first"), NS).unwrap();
        store.put(declaration("s1", "capy.renew", 600)).unwrap();

        assert_eq!(store.next_run_at("s1"), Some(2_000));
        assert!(store.due(1_999).is_empty(), "not owed before its first run");
        assert_eq!(store.due(2_000).len(), 1, "owed at its first run");
    }

    #[test]
    fn a_run_moves_the_next_one_without_touching_the_declaration() {
        let mut store = ScheduleStore::open(location("run"), NS).unwrap();
        store.put(declaration("s1", "capy.renew", 600)).unwrap();
        store.record_run("s1", 5_000, "JB-1", 605_000);

        let progress = store.progress("s1");
        assert_eq!(progress.run_count, 1);
        assert_eq!(progress.last_run_at_ms, 5_000);
        assert_eq!(progress.last_execution_id, "JB-1");
        assert_eq!(store.next_run_at("s1"), Some(605_000));
        assert_eq!(
            store.get("s1").unwrap().first_run_at_ms,
            2_000,
            "the declaration is untouched by a tick"
        );
    }

    #[test]
    fn a_force_trigger_moves_the_next_run_without_counting_as_a_run() {
        let mut store = ScheduleStore::open(location("force"), NS).unwrap();
        store.put(declaration("s1", "capy.renew", 600)).unwrap();
        store.reschedule("s1", 5_000, 5_000);

        assert_eq!(store.progress("s1").run_count, 0);
        assert_eq!(store.next_run_at("s1"), Some(5_000));
    }

    #[test]
    fn a_paused_schedule_keeps_its_place() {
        let mut store = ScheduleStore::open(location("paused"), NS).unwrap();
        let mut paused = declaration("s1", "capy.renew", 600);
        paused.state = "paused".to_string();
        store.put(paused).unwrap();

        assert!(store.due(10_000).is_empty(), "paused is never owed");
        assert_eq!(store.next_run_at("s1"), Some(2_000), "and does not forget");
    }

    #[test]
    fn a_withdrawn_declaration_stops_being_owed() {
        let mut store = ScheduleStore::open(location("withdrawn"), NS).unwrap();
        store
            .put(declaration("s1", "capy.duplicate", 604_800))
            .unwrap();
        assert_eq!(store.due(10_000).len(), 1);

        assert!(store.delete("s1").unwrap());
        assert!(store.get("s1").is_none());
        assert!(store.due(10_000).is_empty(), "nothing runs it now");
    }

    #[test]
    fn ticks_survive_a_reopen_and_the_progress_is_folded_from_them() {
        let loc = location("reopen");
        {
            let mut store = ScheduleStore::open(loc.clone(), NS).unwrap();
            store.put(declaration("s1", "capy.renew", 600)).unwrap();
            store.record_run("s1", 5_000, "JB-1", 605_000);
            store.record_run("s1", 605_000, "JB-2", 1_205_000);
            store.flush().unwrap();
        }

        let store = ScheduleStore::open(loc, NS).unwrap();
        let progress = store.progress("s1");
        assert_eq!(progress.run_count, 2);
        assert_eq!(progress.last_execution_id, "JB-2");
        assert_eq!(store.next_run_at("s1"), Some(1_205_000));
    }

    #[test]
    fn a_withdrawn_schedule_keeps_the_ticks_it_emitted() {
        let loc = location("tombstone");
        {
            let mut store = ScheduleStore::open(loc.clone(), NS).unwrap();
            store.put(declaration("s1", "capy.duplicate", 600)).unwrap();
            store.record_run("s1", 5_000, "JB-1", 605_000);
            store.flush().unwrap();
            store.delete("s1").unwrap();
        }

        let store = ScheduleStore::open(loc, NS).unwrap();
        assert!(store.get("s1").is_none(), "the declaration is withdrawn");
        assert_eq!(store.progress("s1").run_count, 1, "what ran still ran");
    }

    #[test]
    fn a_location_nothing_has_declared_is_empty_not_broken() {
        let store = ScheduleStore::open(location("empty"), NS).unwrap();
        assert!(store.list().is_empty());
        assert!(store.due(i64::MAX).is_empty());
    }
}
