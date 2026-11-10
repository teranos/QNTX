//! ats-dev: ATS on its own. The storage and query code the node runs
//! (`ats-sqlite`, `ats::ax`), without the node: no Go, no WASM, no auth, no
//! Tablet of Reach. It reads and writes attestations in one SQLite file.
//!
//! ```text
//! ats-dev [--db FILE] [--sync full|normal|off] [--key FILE] [--unsigned] put "ALICE is author of GITHUB by BOB" [--attr k=v]...
//! ats-dev [--db FILE] [--sync …] put -          # JSON attestations, one per line, on stdin
//! ats-dev [--db FILE] ax "ALICE is author of GITHUB since last month" [--limit N]
//! ats-dev [--db FILE] get ID
//! ats-dev [--db FILE] count
//! ats-dev [--db FILE] exists ID                 # prints true or false
//! ats-dev [--db FILE] ids
//! ats-dev [--db FILE] update '{"id":"AS-…","attributes":{…}}'   # the given fields replace the stored ones
//! ats-dev [--db FILE] delete ID
//! ats-dev [--db FILE] clear --yes
//! ats-dev [--db FILE] subjects | predicates | contexts | actors   # distinct values, one per line
//! ats-dev [--db FILE] stats
//! ats-dev [--db FILE] key                       # the signing key's did:key and file (minted if there is none)
//! ats-dev [--db FILE] verify [ID]               # valid / invalid / unsigned, one line each; exit 1 if any is invalid
//! ats-dev [--db FILE] alias add A B | rm A B | ls | resolve X
//! ```
//!
//! `put` signs with ats-dev's own ed25519 key (`--key`, else `$ATS_DEV_KEY`, else
//! `<db>.key`, minted on first use) over the bytes `ats/signing` signs in Go.
//! `--unsigned` writes without a signature. This is not the node's key.
//!
//! `--db` defaults to `$ATS_DEV_DB`, else `ats-dev.db`. Rows come out as JSON,
//! one attestation per line. A failure goes to stderr as a sacred-error `Error`
//! and exits 1.

mod sign;

use std::io::{BufRead, Write};
use std::time::{SystemTime, UNIX_EPOCH};

use ats::{ax, AliasStore, AttestationStore, QueryStore, TemporalConfig};
use ats::{Attestation, Parser};
use ats_sqlite::SqliteStore;
use sacred_error::{Context, Error, Severity};

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
    key: Option<String>,
    unsigned: bool,
    rest: Vec<String>,
}

fn opts(args: &[String]) -> Result<Opts, Box<Error>> {
    let mut db = std::env::var("ATS_DEV_DB").unwrap_or_else(|_| "ats-dev.db".into());
    let mut sync = None;
    let mut key = None;
    let mut unsigned = false;
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
            "--key" => key = Some(it.next().ok_or_else(|| fail("--key needs a file", "none given"))?.clone()),
            "--unsigned" => unsigned = true,
            _ => rest.push(a.clone()),
        }
    }
    Ok(Opts { db, sync, key, unsigned, rest })
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

/// ats-dev's signing key, loaded (or minted) the first time something needs a
/// signature made, never for reading, `--unsigned`, or a signature already given.
struct Keys {
    path: std::path::PathBuf,
    unsigned: bool,
    loaded: std::cell::OnceCell<sign::Signer>,
}

impl Keys {
    fn of(o: &Opts) -> Self {
        Keys { path: sign::key_path(o.key.as_deref(), &o.db), unsigned: o.unsigned, loaded: std::cell::OnceCell::new() }
    }

    fn signer(&self) -> Result<Option<&sign::Signer>, Box<Error>> {
        if self.unsigned {
            return Ok(None);
        }
        if let Some(s) = self.loaded.get() {
            return Ok(Some(s));
        }
        let s = sign::Signer::load_or_mint(&self.path)?;
        Ok(Some(self.loaded.get_or_init(|| s)))
    }
}

/// What is written: a signature already given is checked and kept, none given
/// is made with ats-dev's key.
fn seal(keys: &Keys, a: Attestation) -> Result<Attestation, Box<Error>> {
    if a.signature.is_none() && a.signer_did.is_some() {
        return Err(fail("a signer without a signature", format!("{}: name a signature too, or neither", a.id)));
    }
    if a.signature.is_some() {
        return match sign::verify(&a) {
            sign::Verdict::Invalid(why) => Err(fail("the signature given does not hold", format!("{}: {why}", a.id))),
            _ => Ok(a),
        };
    }
    match keys.signer()? {
        Some(sg) => sg.sign(a),
        None => Ok(a),
    }
}

fn run(args: &[String]) -> Result<(), Box<Error>> {
    let o = opts(args)?;
    let mut it = o.rest.iter();
    let cmd = it.next().ok_or_else(|| fail("no command", "key | verify | put | ax | get | count | exists | ids | update | delete | clear | subjects | predicates | contexts | actors | stats | alias"))?;
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
        "exists" => {
            let id = tail.first().ok_or_else(|| fail("exists needs an id", "ats-dev exists ID"))?;
            let yes = open(&o)?.exists(id).map_err(|e| fail("exists failed", e))?;
            println!("{yes}");
            Ok(())
        }
        "ids" => lines(open(&o)?.ids().map_err(|e| fail("ids failed", e))?),
        "subjects" => lines(open(&o)?.subjects().map_err(|e| fail("subjects failed", e))?),
        "predicates" => lines(open(&o)?.predicates().map_err(|e| fail("predicates failed", e))?),
        "contexts" => lines(open(&o)?.contexts().map_err(|e| fail("contexts failed", e))?),
        "actors" => lines(open(&o)?.actors().map_err(|e| fail("actors failed", e))?),
        "stats" => {
            let st = open(&o)?.stats().map_err(|e| fail("stats failed", e))?;
            println!(
                "{}",
                serde_json::json!({
                    "total_attestations": st.total_attestations,
                    "unique_subjects": st.unique_subjects,
                    "unique_predicates": st.unique_predicates,
                    "unique_contexts": st.unique_contexts,
                    "unique_actors": st.unique_actors,
                })
            );
            Ok(())
        }
        "delete" => {
            let id = tail.first().ok_or_else(|| fail("delete needs an id", "ats-dev delete ID"))?;
            if open(&o)?.delete(id).map_err(|e| fail("delete failed", e))? {
                Ok(())
            } else {
                Err(fail("no such attestation", id))
            }
        }
        "clear" => {
            if tail.first().map(|f| f.as_str()) != Some("--yes") {
                return Err(fail("clear removes every attestation", "say so: ats-dev clear --yes"));
            }
            open(&o)?.clear().map_err(|e| fail("clear failed", e))
        }
        "update" => update(&o, &tail),
        "key" => {
            let path = sign::key_path(o.key.as_deref(), &o.db);
            let sg = sign::Signer::load_or_mint(&path)?;
            println!("{}", serde_json::json!({ "did": sg.did, "file": path.display().to_string() }));
            Ok(())
        }
        "verify" => verify(&o, &tail),
        "alias" => alias(&o, &tail),
        other => Err(fail("unknown command", other)),
    }
}

fn lines(v: Vec<String>) -> Result<(), Box<Error>> {
    let mut out = std::io::stdout().lock();
    for x in v {
        writeln!(out, "{x}").map_err(|e| fail("could not write stdout", e))?;
    }
    Ok(())
}

/// The stored attestation with the given JSON fields laid over it. An update
/// that named only `attributes` leaves everything else as it was.
fn update(o: &Opts, tail: &[&String]) -> Result<(), Box<Error>> {
    let given = tail.first().ok_or_else(|| fail("update needs JSON", "ats-dev update '{\"id\":\"AS-…\",\"source\":\"x\"}'"))?;
    let patch: serde_json::Value = serde_json::from_str(given).map_err(|e| fail("not JSON", e))?;
    let patch = patch.as_object().ok_or_else(|| fail("not an object", given))?;
    let unknown: Vec<&str> = patch.keys().map(String::as_str).filter(|k| !FIELDS.contains(k)).collect();
    if !unknown.is_empty() {
        return Err(fail("not an attestation field", unknown.join(", ")));
    }
    // A signature is made, not edited: a patch that names one could swap the
    // signer of a row, or store one that does not hold.
    if let Some(k) = ["signature", "signer_did"].into_iter().find(|k| patch.contains_key(*k)) {
        return Err(fail("an update cannot name a signature", format!("{k}: ats-dev signs what it changes")));
    }
    let id = patch.get("id").and_then(|v| v.as_str()).ok_or_else(|| fail("update needs an \"id\"", given))?;
    let mut s = open(o)?;
    let stored = s.get(id).map_err(|e| fail("get failed", e))?.ok_or_else(|| fail("no such attestation", id))?;
    let mut v = serde_json::to_value(&stored).map_err(|e| fail("could not encode an attestation", e))?;
    if let Some(m) = v.as_object_mut() {
        for (k, x) in patch {
            m.insert(k.clone(), x.clone());
        }
    }
    let mut a: Attestation = serde_json::from_value(v).map_err(|e| fail("not an attestation", e))?;
    // A signature covers what was just changed. It stays only if it still
    // holds; else ats-dev re-signs what its own key signed, and refuses the rest.
    if stored.signature.is_some() {
        if !matches!(sign::verify(&a), sign::Verdict::Valid(_)) {
            let keys = Keys::of(o);
            let sg = keys.signer()?.ok_or_else(|| {
                fail("the update breaks the signature", format!("{id}: --unsigned cannot keep a signed attestation signed"))
            })?;
            if stored.signer_did.as_deref() != Some(sg.did.as_str()) {
                return Err(fail(
                    "the update breaks a signature ats-dev cannot make",
                    format!("{id} is signed by {}, ats-dev's key is {}", stored.signer_did.as_deref().unwrap_or("?"), sg.did),
                ));
            }
            // The actors are under the signature; a signed row with none is out of every actor's claims.
            if a.actors.is_empty() {
                return Err(fail("a signed attestation must name its actors", format!("{id}: the update would leave none")));
            }
            a.signature = None;
            a.signer_did = None;
            a = sg.sign(a)?;
        }
    } else {
        // An unsigned row is authored as put authors it: an update that empties
        // the actors would otherwise hide the row from every actor's claims.
        a = a.authored_by(ACTOR);
    }
    s.update(a).map_err(|e| fail("update failed", e))?;
    // What is on disk, not what was asked for: `created_at` is kept as it was.
    let kept = s.get(id).map_err(|e| fail("get failed", e))?.ok_or_else(|| fail("no such attestation", id))?;
    emit(&kept)
}

fn alias(o: &Opts, tail: &[&String]) -> Result<(), Box<Error>> {
    let usage = "ats-dev alias add A B | rm A B | ls | resolve X";
    let sub = tail.first().ok_or_else(|| fail("alias needs a subcommand", usage))?;
    let mut s = open(o)?;
    let pair = || -> Result<(&str, &str), Box<Error>> {
        match (tail.get(1), tail.get(2)) {
            (Some(a), Some(b)) => Ok((a.as_str(), b.as_str())),
            _ => Err(fail("needs two names", usage)),
        }
    };
    match sub.as_str() {
        "add" => {
            let (a, b) = pair()?;
            s.create_alias(a, b, ACTOR).map_err(|e| fail("could not add the alias", e))
        }
        "rm" => {
            let (a, b) = pair()?;
            s.remove_alias(a, b).map_err(|e| fail("could not remove the alias", e))
        }
        "ls" => {
            let all = s.all_aliases().map_err(|e| fail("could not list aliases", e))?;
            let mut keys: Vec<_> = all.keys().collect();
            keys.sort();
            let mut out = std::io::stdout().lock();
            for k in keys {
                let line = serde_json::json!({ "alias": k, "targets": all.get(k) });
                writeln!(out, "{line}").map_err(|e| fail("could not write stdout", e))?;
            }
            Ok(())
        }
        "resolve" => {
            let x = tail.get(1).ok_or_else(|| fail("resolve needs a name", usage))?;
            lines(s.resolve_alias(x).map_err(|e| fail("could not resolve", e))?)
        }
        other => Err(fail("unknown alias subcommand", other)),
    }
}

fn emit(a: &Attestation) -> Result<(), Box<Error>> {
    let line = serde_json::to_string(a).map_err(|e| fail("could not encode an attestation", e))?;
    writeln!(std::io::stdout(), "{line}").map_err(|e| fail("could not write stdout", e))
}

fn put(o: &Opts, tail: &[&String]) -> Result<(), Box<Error>> {
    let what = tail.first().ok_or_else(|| fail("put needs a sentence or -", "ats-dev put \"A is p of C by X\""))?;
    let mut s = open(o)?;
    let keys = Keys::of(o);
    if what.as_str() == "-" {
        return put_stdin(&mut s, &keys);
    }
    let parsed = Parser::parse(what).map_err(|e| fail("could not parse the sentence", e))?;
    if parsed.has_temporal() {
        return Err(fail(
            "a sentence cannot carry a time clause",
            "since/until/on/between/over name a range of time, not the moment an attestation was made; give \"timestamp\" (ms) on stdin (put -)",
        ));
    }
    let own = |w: &[&str]| w.iter().map(|x| x.to_string()).collect::<Vec<_>>();
    let a = Attestation {
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
    let mut a = a.authored_by(ACTOR);
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
    let a = seal(&keys, a)?;
    s.put(a.clone()).map_err(|e| fail("put failed", e))?;
    emit(&a)
}

/// Who authors what its caller named nobody for: the rule is ATS's
/// ([`Attestation::authored_by`]), the node's door applies it with the node's
/// actor, and ats-dev, which has no node, with its own.
const ACTOR: &str = "ats-dev";

/// The fields of an `Attestation`; a line naming any other is refused.
const FIELDS: [&str; 11] = [
    "id", "subjects", "predicates", "contexts", "actors", "timestamp", "source",
    "attributes", "created_at", "signature", "signer_did",
];

/// One JSON attestation per line. Missing id, timestamp, created_at, source,
/// predicates, contexts and actors are filled; everything given is kept.
fn complete(line: &str) -> Result<Attestation, Box<Error>> {
    let mut v: serde_json::Value = serde_json::from_str(line).map_err(|e| fail("not JSON", e))?;
    let obj = v.as_object_mut().ok_or_else(|| fail("not an object", line))?;
    let unknown: Vec<&str> = obj.keys().map(String::as_str).filter(|k| !FIELDS.contains(k)).collect();
    if !unknown.is_empty() {
        return Err(fail(
            "not an attestation field",
            format!("{} (arbitrary data goes in \"attributes\")", unknown.join(", ")),
        ));
    }
    let now = now_ms();
    if !obj.contains_key("id") { obj.insert("id".into(), new_id()?.into()); }
    if !obj.contains_key("timestamp") { obj.insert("timestamp".into(), now.into()); }
    if !obj.contains_key("created_at") { obj.insert("created_at".into(), now.into()); }
    if !obj.contains_key("source") { obj.insert("source".into(), "ats-dev".into()); }
    for k in ["predicates", "contexts"] {
        if !obj.contains_key(k) { obj.insert(k.into(), serde_json::json!(["_"])); }
    }
    if !obj.contains_key("actors") { obj.insert("actors".into(), serde_json::json!([])); }
    let a: Attestation = serde_json::from_value(v).map_err(|e| fail("not an attestation", e))?;
    // The actors are under the signature. Naming one here would store a row
    // whose signature no longer holds; the node's door refuses that too
    // ("the signer names them so"), so the signer must.
    if a.signature.is_some() && a.actors.is_empty() {
        return Err(fail(
            "a signed attestation must name its actors",
            format!("{}: the signature covers them, so ats-dev cannot author one in", a.id),
        ));
    }
    Ok(a.authored_by(ACTOR))
}

fn put_stdin(s: &mut SqliteStore, keys: &Keys) -> Result<(), Box<Error>> {
    let mut batch: Vec<String> = Vec::new();
    let mut total = 0usize;
    for (n, line) in std::io::stdin().lock().lines().enumerate() {
        let line = line.map_err(|e| fail("could not read stdin", e))?;
        if line.trim().is_empty() { continue; }
        let at = n + 1;
        let a = match complete(&line).and_then(|a| seal(keys, a)) {
            Ok(a) => a,
            Err(e) => return abandon(s, &batch, fail(&format!("line {at}: {}", e.title), e.why)),
        };
        if batch.is_empty() {
            s.connection().execute_batch("BEGIN").map_err(|e| fail("BEGIN failed", e))?;
        }
        let id = a.id.clone();
        match s.put(a) {
            Ok(()) => {}
            // put() runs a PASSIVE checkpoint after every 5000th put, which
            // SQLite refuses inside a transaction. The row is already inserted.
            Err(e) if e.to_string().contains("wal_checkpoint") => {}
            Err(e) => return abandon(s, &batch, fail(&format!("line {at}: put failed"), format!("{id}: {e}"))),
        }
        batch.push(id);
        total += 1;
        if batch.len() == BATCH {
            commit(s, &mut batch)?;
        }
    }
    commit(s, &mut batch)?;
    // wal_autocheckpoint is 0 in SqliteStore::open, so nothing else folds the WAL back.
    s.wal_checkpoint_truncate().map_err(|e| fail("checkpoint failed", e))?;
    eprintln!("ats-dev: {total} attestations written");
    Ok(())
}

/// COMMIT the open batch, then say its ids: an id on stdout is a row on disk.
fn commit(s: &SqliteStore, batch: &mut Vec<String>) -> Result<(), Box<Error>> {
    if batch.is_empty() { return Ok(()); }
    s.connection().execute_batch("COMMIT").map_err(|e| fail("COMMIT failed", e))?;
    for id in batch.drain(..) { emit_id(&id)?; }
    Ok(())
}

/// A line failed: the open batch is rolled back, none of its ids were said.
fn abandon(s: &SqliteStore, batch: &[String], why: Box<Error>) -> Result<(), Box<Error>> {
    if !batch.is_empty() {
        s.connection().execute_batch("ROLLBACK").map_err(|e| fail("ROLLBACK failed", e))?;
        eprintln!("ats-dev: rolled back {} attestations of the open batch", batch.len());
    }
    Err(why)
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
    let res = ax::execute_with_aliases(&s, &req.filter, &TemporalConfig::default(), now)
        .map_err(|e| fail("query failed", e))?;
    for a in &res.attestations {
        emit(a)?;
    }
    eprintln!("ats-dev: {} attestations", res.attestations.len());
    Ok(())
}

fn verify(o: &Opts, tail: &[&String]) -> Result<(), Box<Error>> {
    let s = open(o)?;
    let ids = match tail.first() {
        Some(id) => vec![id.to_string()],
        None => s.ids().map_err(|e| fail("ids failed", e))?,
    };
    let mut bad = 0usize;
    let mut out = std::io::stdout().lock();
    for id in ids {
        let a = s.get(&id).map_err(|e| fail("get failed", e))?.ok_or_else(|| fail("no such attestation", &id))?;
        let line = match sign::verify(&a) {
            sign::Verdict::Unsigned => serde_json::json!({ "id": id, "signature": "unsigned" }),
            sign::Verdict::Valid(did) => serde_json::json!({ "id": id, "signature": "valid", "signer": did }),
            sign::Verdict::Invalid(why) => {
                bad += 1;
                serde_json::json!({ "id": id, "signature": "invalid", "why": why })
            }
        };
        writeln!(out, "{line}").map_err(|e| fail("could not write stdout", e))?;
    }
    if bad > 0 {
        return Err(fail("signatures that do not hold", format!("{bad}")));
    }
    Ok(())
}
