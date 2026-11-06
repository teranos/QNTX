//! ats-dev: ATS on its own. The storage and query code the node runs
//! (`ats-sqlite`, `ats::ax`), without the node: no Go, no WASM, no auth, no
//! Tablet of Reach. It reads and writes attestations in one SQLite file.
//!
//! ```text
//! ats-dev [--db FILE] [--sync full|normal|off] put "ALICE is author of GITHUB by BOB" [--attr k=v]...
//! ats-dev [--db FILE] [--sync …] put -          # JSON attestations, one per line, on stdin
//! ats-dev [--db FILE] ax "ALICE is author of GITHUB since last month" [--limit N]
//! ats-dev [--db FILE] get ID
//! ats-dev [--db FILE] count
//! ```
//!
//! `--db` defaults to `$ATS_DEV_DB`, else `ats-dev.db`. Rows come out as JSON,
//! one attestation per line. A failure goes to stderr as a laye-error `Error`
//! and exits 1.

use std::io::{BufRead, Write};
use std::time::{SystemTime, UNIX_EPOCH};

use ats::{ax, AttestationStore, TemporalConfig};
use ats::{Attestation, Parser};
use ats_sqlite::SqliteStore;
use laye_error::{Context, Error, Severity};

/// Puts per transaction on `put -`. Unbatched, each put is five commits
/// (`SqliteStore::put` commits the row and each junction loop on its own).
const BATCH: usize = 1000;
const DEFAULT_LIMIT: usize = 100;

fn main() {
    let args: Vec<String> = std::env::args().skip(1).collect();
    if let Err(e) = run(&args) {
        let out = serde_json::to_string(&e).unwrap_or_else(|_| format!("{e:?}"));
        eprintln!("{out}");
        std::process::exit(1);
    }
}

fn fail(title: &str, why: impl std::fmt::Display) -> Box<Error> {
    Box::new(Error {
        id: format!("ats-dev-{}", now_ms()),
        severity: Severity::Error,
        context: Context { surface: "ats-dev".into(), region: None, anchor: None },
        title: title.into(),
        why: why.to_string(),
        trace: Vec::new(),
        raw: None,
        at: now_ms().to_string(),
        source: Some("ats-dev".into()),
        ffi_call: None,
        location: None,
        js_stack: None,
        raw_stderr: None,
        requires_reload: false,
    })
}

fn now_ms() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_millis() as i64)
        .unwrap_or(0)
}

/// `AS-` and a random v4 UUID, from the OS.
fn new_id() -> Result<String, Box<Error>> {
    let mut b = [0u8; 16];
    std::fs::File::open("/dev/urandom")
        .and_then(|mut f| std::io::Read::read_exact(&mut f, &mut b))
        .map_err(|e| fail("could not read /dev/urandom", e))?;
    b[6] = (b[6] & 0x0f) | 0x40;
    b[8] = (b[8] & 0x3f) | 0x80;
    let h: String = b.iter().map(|x| format!("{x:02x}")).collect();
    Ok(format!(
        "AS-{}-{}-{}-{}-{}",
        h.get(0..8).unwrap_or(""),
        h.get(8..12).unwrap_or(""),
        h.get(12..16).unwrap_or(""),
        h.get(16..20).unwrap_or(""),
        h.get(20..32).unwrap_or("")
    ))
}

struct Opts {
    db: String,
    sync: Option<String>,
    rest: Vec<String>,
}

fn opts(args: &[String]) -> Result<Opts, Box<Error>> {
    let mut db = std::env::var("ATS_DEV_DB").unwrap_or_else(|_| "ats-dev.db".into());
    let mut sync = None;
    let mut rest = Vec::new();
    let mut it = args.iter();
    while let Some(a) = it.next() {
        match a.as_str() {
            "--db" => db = it.next().ok_or_else(|| fail("--db needs a file", "none given"))?.clone(),
            "--sync" => {
                let v = it.next().ok_or_else(|| fail("--sync needs a value", "full|normal|off"))?;
                if !matches!(v.as_str(), "full" | "normal" | "off") {
                    return Err(fail("--sync takes full, normal or off", v));
                }
                sync = Some(v.clone());
            }
            _ => rest.push(a.clone()),
        }
    }
    Ok(Opts { db, sync, rest })
}

fn open(o: &Opts) -> Result<SqliteStore, Box<Error>> {
    let s = SqliteStore::open(&o.db).map_err(|e| fail("could not open the database", e))?;
    if let Some(m) = &o.sync {
        s.connection()
            .pragma_update(None, "synchronous", m.as_str())
            .map_err(|e| fail("could not set synchronous", e))?;
    }
    Ok(s)
}

fn run(args: &[String]) -> Result<(), Box<Error>> {
    let o = opts(args)?;
    let mut it = o.rest.iter();
    let cmd = it.next().ok_or_else(|| fail("no command", "put | ax | get | count"))?;
    let tail: Vec<&String> = it.collect();
    match cmd.as_str() {
        "put" => put(&o, &tail),
        "ax" => query(&o, &tail),
        "get" => {
            let id = tail.first().ok_or_else(|| fail("get needs an id", "ats-dev get ID"))?;
            let s = open(&o)?;
            match s.get(id).map_err(|e| fail("get failed", e))? {
                Some(a) => emit(&a),
                None => Err(fail("no such attestation", id)),
            }
        }
        "count" => {
            let n = open(&o)?.count().map_err(|e| fail("count failed", e))?;
            println!("{n}");
            Ok(())
        }
        other => Err(fail("unknown command", other)),
    }
}

fn emit(a: &Attestation) -> Result<(), Box<Error>> {
    let line = serde_json::to_string(a).map_err(|e| fail("could not encode an attestation", e))?;
    writeln!(std::io::stdout(), "{line}").map_err(|e| fail("could not write stdout", e))
}

fn put(o: &Opts, tail: &[&String]) -> Result<(), Box<Error>> {
    let what = tail.first().ok_or_else(|| fail("put needs a sentence or -", "ats-dev put \"A is p of C by X\""))?;
    let mut s = open(o)?;
    if what.as_str() == "-" {
        return put_stdin(&mut s);
    }
    let parsed = Parser::parse(what).map_err(|e| fail("could not parse the sentence", e))?;
    let own = |w: &[&str]| w.iter().map(|x| x.to_string()).collect::<Vec<_>>();
    let mut a = Attestation {
        id: new_id()?,
        subjects: own(&parsed.subjects),
        contexts: if parsed.contexts.is_empty() { vec!["_".into()] } else { own(&parsed.contexts) },
        predicates: if parsed.predicates.is_empty() { vec!["_".into()] } else { own(&parsed.predicates) },
        actors: own(&parsed.actors),
        timestamp: now_ms(),
        created_at: now_ms(),
        source: "ats-dev".into(),
        ..Attestation::default()
    };
    let mut flags = tail.iter().skip(1);
    while let Some(f) = flags.next() {
        match f.as_str() {
            "--attr" => {
                let kv = flags.next().ok_or_else(|| fail("--attr needs k=v", "none given"))?;
                let (k, v) = kv.split_once('=').ok_or_else(|| fail("--attr takes k=v", kv))?;
                // JSON when it is JSON (3, true, {"a":1}), else the text.
                let val = serde_json::from_str(v).unwrap_or_else(|_| serde_json::Value::String(v.into()));
                a.attributes.insert(k.into(), val);
            }
            "--source" => a.source = flags.next().ok_or_else(|| fail("--source needs a value", "none given"))?.to_string(),
            other => return Err(fail("unknown put flag", other)),
        }
    }
    s.put(a.clone()).map_err(|e| fail("put failed", e))?;
    emit(&a)
}

/// One JSON attestation per line. Missing id, timestamp, created_at, source,
/// predicates, contexts and actors are filled; everything given is kept.
fn complete(line: &str) -> Result<Attestation, Box<Error>> {
    let mut v: serde_json::Value = serde_json::from_str(line).map_err(|e| fail("not JSON", e))?;
    let obj = v.as_object_mut().ok_or_else(|| fail("not an object", line))?;
    let now = now_ms();
    if !obj.contains_key("id") { obj.insert("id".into(), new_id()?.into()); }
    if !obj.contains_key("timestamp") { obj.insert("timestamp".into(), now.into()); }
    if !obj.contains_key("created_at") { obj.insert("created_at".into(), now.into()); }
    if !obj.contains_key("source") { obj.insert("source".into(), "ats-dev".into()); }
    for k in ["predicates", "contexts"] {
        if !obj.contains_key(k) { obj.insert(k.into(), serde_json::json!(["_"])); }
    }
    if !obj.contains_key("actors") { obj.insert("actors".into(), serde_json::json!([])); }
    serde_json::from_value(v).map_err(|e| fail("not an attestation", e))
}

fn put_stdin(s: &mut SqliteStore) -> Result<(), Box<Error>> {
    let mut in_txn = 0usize;
    let mut total = 0usize;
    for line in std::io::stdin().lock().lines() {
        let line = line.map_err(|e| fail("could not read stdin", e))?;
        if line.trim().is_empty() { continue; }
        let a = complete(&line)?;
        if in_txn == 0 {
            s.connection().execute_batch("BEGIN").map_err(|e| fail("BEGIN failed", e))?;
        }
        let id = a.id.clone();
        match s.put(a) {
            Ok(()) => {}
            // put() runs a PASSIVE checkpoint after every 5000th put, which
            // SQLite refuses inside a transaction. The row is already inserted.
            Err(e) if e.to_string().contains("wal_checkpoint") => {}
            Err(e) => {
                s.connection().execute_batch("ROLLBACK").map_err(|e| fail("ROLLBACK failed", e))?;
                return Err(fail("put failed", format!("{id}: {e}")));
            }
        }
        in_txn += 1;
        total += 1;
        if in_txn == BATCH {
            s.connection().execute_batch("COMMIT").map_err(|e| fail("COMMIT failed", e))?;
            in_txn = 0;
        }
        emit_id(&id)?;
    }
    if in_txn > 0 {
        s.connection().execute_batch("COMMIT").map_err(|e| fail("COMMIT failed", e))?;
    }
    // wal_autocheckpoint is 0 in SqliteStore::open, so nothing else folds the WAL back.
    s.wal_checkpoint_truncate().map_err(|e| fail("checkpoint failed", e))?;
    eprintln!("ats-dev: {total} attestations written");
    Ok(())
}

fn emit_id(id: &str) -> Result<(), Box<Error>> {
    writeln!(std::io::stdout(), "{id}").map_err(|e| fail("could not write stdout", e))
}

fn query(o: &Opts, tail: &[&String]) -> Result<(), Box<Error>> {
    let q = tail.first().ok_or_else(|| fail("ax needs a query", "ats-dev ax \"ALICE is author of GITHUB\""))?;
    let mut limit = DEFAULT_LIMIT;
    let mut flags = tail.iter().skip(1);
    while let Some(f) = flags.next() {
        match f.as_str() {
            "--limit" => {
                let n = flags.next().ok_or_else(|| fail("--limit needs a number", "none given"))?;
                limit = n.parse().map_err(|e| fail("--limit is not a number", e))?;
            }
            other => return Err(fail("unknown ax flag", other)),
        }
    }
    let now = now_ms();
    let req = ax::ax_filter(q, now, limit).map_err(|e| fail("could not read the query", e))?;
    let s = open(o)?;
    let res = ax::execute(&s, &req.filter, &TemporalConfig::default(), now)
        .map_err(|e| fail("query failed", e))?;
    for a in &res.attestations {
        emit(a)?;
    }
    eprintln!("ats-dev: {} attestations", res.attestations.len());
    Ok(())
}
