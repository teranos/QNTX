//! `ats-dev bench`: what the store takes, and where it falls, as numbers.
//!
//! ```text
//! ats-dev bench load N [--step S] [--batch K] [--replace]   fresh db, N synthetic puts, a line per S puts
//! ats-dev bench probe N                                     N per-put writes onto the existing db, then the queries
//! ats-dev bench query                                       the queries, on the existing db
//! ```
//!
//! `load` without `--batch` is the library's own path: every put commits on its
//! own. `--batch K` puts K per transaction. The workload is synthetic, unsigned
//! and fixed (the same attestations on every run), so two runs, two disks or two
//! versions of the store compare. `load` replaces the database file, so it
//! refuses one that exists unless `--replace` says so.
//!
//! The columns are raw. `crates/ats-dev/bench/README.md` says what they are and
//! how to run this against a slower disk than the one you have.

use std::time::Instant;

use ats::{Attestation, AttestationStore, AxFilter};
use ats_sqlite::store::build_query_sql;
use ats_sqlite::SqliteStore;
use sacred_error::Error;

use crate::{fail, open, Opts};

/// xorshift64: fixed seed, no dependency, the same stream on every machine.
struct Rng(u64);

impl Rng {
    fn next(&mut self) -> u64 {
        let mut x = self.0;
        x ^= x << 13;
        x ^= x >> 7;
        x ^= x << 17;
        self.0 = x;
        x
    }
}

const SEED: u64 = 0x9E37_79B9_7F4A_7C15;
/// `timestamp` of attestation 0, and a second apart after it.
const EPOCH_MS: i64 = 1_700_000_000_000;

fn make(i: u64, rng: &mut Rng, subjects: u64) -> Attestation {
    let ts = EPOCH_MS + i64::try_from(i).unwrap_or(0) * 1000;
    let mut a = Attestation {
        id: format!("AS-{:016x}-{:016x}", i, rng.next()),
        subjects: vec![format!("subject-{}", rng.next() % subjects)],
        predicates: vec![format!("pred-{}", rng.next() % 50)],
        contexts: vec![format!("ctx-{}", rng.next() % 20)],
        actors: vec![format!("actor-{}", rng.next() % 5)],
        timestamp: ts,
        created_at: ts,
        source: "bench".into(),
        ..Attestation::default()
    };
    a.attributes.insert("n".into(), serde_json::json!(i));
    a.attributes.insert("note".into(), serde_json::json!("x".repeat(120)));
    a
}

/// The `p` quantile (0..1) of `v`, sorted in place.
fn quantile(v: &mut [f64], p: f64) -> f64 {
    v.sort_by(|a, b| a.partial_cmp(b).unwrap_or(std::cmp::Ordering::Equal));
    let at = ((v.len().saturating_sub(1)) as f64 * p) as usize;
    v.get(at).copied().unwrap_or(0.0)
}

fn ms(t: Instant) -> f64 {
    t.elapsed().as_secs_f64() * 1e3
}

/// What this process has asked of the kernel's block layer (`/proc/self/io`);
/// `None` where there is no /proc.
#[derive(Clone, Copy, Default)]
struct Io {
    read_syscalls: u64,
    write_syscalls: u64,
    read_bytes: u64,
    write_bytes: u64,
}

fn io() -> Option<Io> {
    let text = std::fs::read_to_string("/proc/self/io").ok()?;
    let mut o = Io::default();
    for line in text.lines() {
        let (k, v) = line.split_once(": ")?;
        let v: u64 = v.trim().parse().ok()?;
        match k {
            "syscr" => o.read_syscalls = v,
            "syscw" => o.write_syscalls = v,
            "read_bytes" => o.read_bytes = v,
            "write_bytes" => o.write_bytes = v,
            _ => {}
        }
    }
    Some(o)
}

fn since(a: Option<Io>, b: Option<Io>) -> Option<Io> {
    let (a, b) = (a?, b?);
    Some(Io {
        read_syscalls: b.read_syscalls.saturating_sub(a.read_syscalls),
        write_syscalls: b.write_syscalls.saturating_sub(a.write_syscalls),
        read_bytes: b.read_bytes.saturating_sub(a.read_bytes),
        write_bytes: b.write_bytes.saturating_sub(a.write_bytes),
    })
}

/// A column that needs /proc, or `-` without it.
fn col(v: Option<f64>, places: usize) -> String {
    v.map_or_else(|| "-".into(), |x| format!("{x:.places$}"))
}

/// Peak resident set, MB (`VmHWM`).
fn peak_rss_mb() -> Option<f64> {
    let text = std::fs::read_to_string("/proc/self/status").ok()?;
    let kb: f64 = text.lines().find(|l| l.starts_with("VmHWM"))?.split_whitespace().nth(1)?.parse().ok()?;
    Some(kb / 1024.0)
}

fn file_mb(path: &str) -> f64 {
    std::fs::metadata(path).map(|m| m.len()).unwrap_or(0) as f64 / 1e6
}

/// `put()` runs a PASSIVE checkpoint after every 5000th put, which SQLite
/// refuses inside a transaction. The row is already inserted.
fn put_in_txn(s: &mut SqliteStore, a: Attestation) -> Result<(), Box<Error>> {
    match s.put(a) {
        Ok(()) => Ok(()),
        Err(e) if e.to_string().contains("wal_checkpoint") => Ok(()),
        Err(e) => Err(fail("put failed", e)),
    }
}

fn number(flag: &str, v: Option<&&String>) -> Result<u64, Box<Error>> {
    let v = v.ok_or_else(|| fail(&format!("{flag} needs a number"), "none given"))?;
    v.parse().map_err(|e| fail(&format!("{flag} is not a number"), format!("{v}: {e}")))
}

pub fn run(o: &Opts, tail: &[&String]) -> Result<(), Box<Error>> {
    let usage = "ats-dev bench load N [--step S] [--batch K] [--replace] | probe N | query";
    let mut it = tail.iter();
    let sub = it.next().ok_or_else(|| fail("bench needs load, probe or query", usage))?;
    let rest: Vec<&&String> = it.collect();
    match sub.as_str() {
        "load" => load(o, &rest),
        "probe" => probe(o, rest.first().copied().copied()),
        "query" => {
            let s = open(o)?;
            let rows = s.count().map_err(|e| fail("count failed", e))?;
            queries(&s, rows as i64)
        }
        other => Err(fail("unknown bench subcommand", format!("{other}: {usage}"))),
    }
}

fn load(o: &Opts, rest: &[&&String]) -> Result<(), Box<Error>> {
    let total = number("N", rest.first().copied())?;
    if total == 0 {
        return Err(fail("N must be at least 1", "0 given"));
    }
    let (mut step, mut batch, mut replace) = (None, None, false);
    let mut flags = rest.iter().skip(1);
    while let Some(f) = flags.next() {
        match f.as_str() {
            "--step" => step = Some(number("--step", flags.next().copied())?.max(1)),
            "--batch" => batch = Some(number("--batch", flags.next().copied())?.max(1)),
            "--replace" => replace = true,
            other => return Err(fail("unknown bench load flag", other)),
        }
    }
    let step = step.unwrap_or((total / 10).max(1));
    for suffix in ["", "-wal", "-shm"] {
        let f = format!("{}{suffix}", o.db);
        if std::path::Path::new(&f).exists() {
            if !replace {
                return Err(fail(
                    "bench load replaces the database",
                    format!("{f} exists; --replace to delete it and the -wal/-shm beside it"),
                ));
            }
            std::fs::remove_file(&f).map_err(|e| fail("could not remove the old database", format!("{f}: {e}")))?;
        }
    }

    let subjects = (total / 10).max(100);
    let mut s = open(o)?;
    let mut rng = Rng(SEED);
    let mode = batch.map_or_else(|| "put".to_string(), |k| format!("batch:{k}"));
    println!("# mode={mode} total={total} step={step} subjects={subjects} sync={}", o.sync.as_deref().unwrap_or("db default"));
    println!("# latency columns are per put{}", if batch.is_some() { " batch (one transaction)" } else { "" });
    println!("n_done,put_per_s,p50_ms,p99_ms,max_ms,db_MB,wal_MB,write_MB_total,write_syscalls_total");
    let io0 = io();
    let mut i = 0u64;
    while i < total {
        let step_end = (i + step).min(total);
        let done_before = i;
        let mut lat: Vec<f64> = Vec::new();
        let t0 = Instant::now();
        match batch {
            None => {
                while i < step_end {
                    let a = make(i, &mut rng, subjects);
                    let t = Instant::now();
                    s.put(a).map_err(|e| fail("put failed", e))?;
                    lat.push(ms(t));
                    i += 1;
                }
            }
            Some(k) => {
                while i < step_end {
                    let t = Instant::now();
                    s.connection().execute_batch("BEGIN").map_err(|e| fail("BEGIN failed", e))?;
                    for _ in 0..k.min(step_end - i) {
                        put_in_txn(&mut s, make(i, &mut rng, subjects))?;
                        i += 1;
                    }
                    s.connection().execute_batch("COMMIT").map_err(|e| fail("COMMIT failed", e))?;
                    lat.push(ms(t));
                }
                // wal_autocheckpoint is 0 in SqliteStore::open: nothing else folds the WAL back.
                s.wal_checkpoint_truncate().map_err(|e| fail("checkpoint failed", e))?;
            }
        }
        let elapsed = t0.elapsed().as_secs_f64();
        let max = lat.iter().copied().fold(0.0, f64::max);
        let used = since(io0, io());
        println!(
            "{i},{:.0},{:.3},{:.3},{max:.1},{:.1},{:.1},{},{}",
            (i - done_before) as f64 / elapsed,
            quantile(&mut lat, 0.5),
            quantile(&mut lat, 0.99),
            file_mb(&o.db),
            file_mb(&format!("{}-wal", o.db)),
            col(used.map(|u| u.write_bytes as f64 / 1e6), 0),
            used.map_or_else(|| "-".into(), |u| u.write_syscalls.to_string()),
        );
    }
    s.wal_checkpoint_truncate().map_err(|e| fail("checkpoint failed", e))?;
    let bytes = std::fs::metadata(&o.db).map(|m| m.len()).unwrap_or(0);
    println!("# final db_bytes={bytes} bytes_per_attestation={} peak_rss_MB={}", bytes / total, col(peak_rss_mb(), 0));
    queries(&s, total as i64)
}

fn probe(o: &Opts, n: Option<&String>) -> Result<(), Box<Error>> {
    let n = number("N", n.as_ref())?;
    if n == 0 {
        return Err(fail("N must be at least 1", "0 given"));
    }
    let mut s = open(o)?;
    let rows = s.count().map_err(|e| fail("count failed", e))?;
    let subjects = (rows as u64 / 10).max(100);
    let mut rng = Rng(SEED);
    // ids that cannot meet the load's
    let first = rows as u64 + 10_000_000;
    let (io0, t0) = (io(), Instant::now());
    let mut lat = Vec::new();
    for i in (first..).take(usize::try_from(n).unwrap_or(usize::MAX)) {
        let a = make(i, &mut rng, subjects);
        let t = Instant::now();
        s.put(a).map_err(|e| fail("put failed", e))?;
        lat.push(ms(t));
    }
    let el = t0.elapsed().as_secs_f64();
    let used = since(io0, io());
    println!(
        "# probe {n} per-put writes onto an existing db (rows={rows}): {:.1}/s p50={:.2}ms p99={:.2}ms | per put: phys_read_KB={} read_syscalls={} write_KB={}",
        n as f64 / el,
        quantile(&mut lat, 0.5),
        quantile(&mut lat, 0.99),
        col(used.map(|u| u.read_bytes as f64 / 1e3 / n as f64), 1),
        col(used.map(|u| u.read_syscalls as f64 / n as f64), 1),
        col(used.map(|u| u.write_bytes as f64 / 1e3 / n as f64), 1),
    );
    queries(&s, (rows as u64 + n) as i64)
}

/// The query set the workload is built for: subjects, predicates, contexts and
/// actors it drew from, times it spaced a second apart.
fn queries(s: &SqliteStore, rows: i64) -> Result<(), Box<Error>> {
    let f = |subjects: &[&str], predicates: &[&str], contexts: &[&str], actors: &[&str], from: Option<i64>, to: Option<i64>| AxFilter {
        subjects: subjects.iter().map(|x| x.to_string()).collect(),
        predicates: predicates.iter().map(|x| x.to_string()).collect(),
        contexts: contexts.iter().map(|x| x.to_string()).collect(),
        actors: actors.iter().map(|x| x.to_string()).collect(),
        time_start: from,
        time_end: to,
        limit: 50,
        ..AxFilter::default()
    };
    let at = |n: i64| EPOCH_MS + n * 1000;
    let set: Vec<(&str, AxFilter)> = vec![
        ("by_subject(rare)", f(&["subject-7"], &[], &[], &[], None, None)),
        ("by_predicate(common)", f(&[], &["pred-3"], &[], &[], None, None)),
        ("by_actor(common)", f(&[], &[], &[], &["actor-1"], None, None)),
        ("subject+predicate", f(&["subject-7"], &["pred-3"], &[], &[], None, None)),
        ("window(newest 1%)", f(&[], &[], &[], &[], Some(at(rows * 99 / 100)), None)),
        ("window(oldest 1%)", f(&[], &[], &[], &[], None, Some(at(rows / 100)))),
        (
            "pred+ctx+actor mid-window",
            f(&[], &["pred-3"], &["ctx-2"], &["actor-1"], Some(at(rows / 2)), Some(at(rows / 2 + rows / 100))),
        ),
    ];
    println!("# queries (rows={rows}), limit 50: label,rows,first_ms,second_ms,phys_read_KB_first,read_syscalls_first");
    for (name, filter) in &set {
        let (sql, params) = build_query_sql(filter);
        let params = serde_json::to_string(&params).map_err(|e| fail("could not encode the query parameters", e))?;
        let (mut times, mut got, mut used) = ([0f64; 2], 0usize, None);
        for (k, slot) in times.iter_mut().enumerate() {
            let io0 = io();
            let t = Instant::now();
            let v = s.query_attestations_raw(&sql, &params).map_err(|e| fail("query failed", e))?;
            *slot = ms(t);
            got = v.len();
            if k == 0 {
                used = since(io0, io());
            }
        }
        println!(
            "{name},{got},{:.2},{:.2},{},{}",
            times[0],
            times[1],
            col(used.map(|u| u.read_bytes as f64 / 1e3), 0),
            used.map_or_else(|| "-".into(), |u| u.read_syscalls.to_string()),
        );
    }
    Ok(())
}
