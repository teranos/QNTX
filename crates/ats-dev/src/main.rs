//! ats-dev: ATS on its own. The storage and query code the node runs
//! (`ats-sqlite`, `ats::ax`), without the node: no Go, no WASM, no auth, no
//! Tablet of Reach. It reads and writes attestations in one SQLite file.
//!
//! ```text
//! ats-dev [--backend sqlite|duckdb|postgres] [--db FILE|LOCATION|URL] [--namespace NS] [--ca FILE]   # before any command; see below
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
//! ats-dev [--db FILE] key                       # the signing key's did:key and file
//! ats-dev [--db FILE] push NODE [ID...]         # send rows to a node, the request signed by the key; one JSON line per row
//! ats-dev [--db FILE] bench load N [--step S] [--batch K] [--replace] | probe N | query   # see bench/README.md
//! ats-dev [--db FILE] verify [ID]               # valid / invalid / unsigned, one line each; exit 1 if any is invalid
//! ats-dev [--db FILE] alias add A B | rm A B | ls | resolve X
//! ```
//!
//! `put` signs with `--key FILE`, else `$ATS_DEV_KEY`, else the key kept in
//! `~/.qntx/key`: the one `qntx` signs a request to a node with, a 32-byte
//! ed25519 seed in hex that `ground code login` keeps. ats-dev never mints one.
//! The bytes are the ones the node verifies (`ats-sign`), and a row signed here
//! is the node's to store as it was signed: its actors lead with the key's
//! did:key and its timestamp is a whole second, as the node writes them under
//! a signature. `--unsigned` writes without a signature, authored by `ats-dev`.
//! A key file that group or other can reach (mode other than 0600) is refused.
//!
//! "A signature proves the key, and nothing reusable leaves the node."
//! `push NODE` posts rows to `NODE/api/attestations`, each request carrying a
//! JWT the same key signs for that node, spent once there; the node stores a
//! row signed by that key as signed, and signs an unsigned one as itself.
//!
//! `--db` defaults to `$ATS_DEV_DB`, else `ats-dev.db`. Rows come out as JSON,
//! one attestation per line. A failure goes to stderr as a sacred-error `Error`
//! and exits 1.
//!
//! `--backend` (or `$ATS_DEV_BACKEND`) picks the store. `sqlite` is the default.
//! `duckdb` and `postgres` are built with `--features duckdb` / `--features
//! postgres` (duckdb links libduckdb dynamically, as the node does) and run
//! what `AttestationStore` has: put, get, exists, count, ids, update, delete,
//! clear, verify. `ax`, the listings, `stats` and `alias` need `QueryStore` /
//! `AliasStore`, which only `SqliteStore` implements, so they refuse on the
//! others. `--db` is DuckDB's location (a path, or an `s3://` / `file://` url)
//! and Postgres's connection url (`$QNTX_POSTGRES_URL` if not given); `--ca`
//! is Postgres's CA file when the url is TLS; `--namespace` (default
//! `ats-dev`) is the namespace both open.
//!
//! `duckdb` is one writer per location (ADR-024: "One node writes a namespace
//! prefix"). `ats-dev` does not coordinate two processes writing the same one,
//! and two that put the same id at once can both succeed.

mod backend;
mod bench;
mod sign;

use std::io::{BufRead, Write};
use std::time::{SystemTime, UNIX_EPOCH};

use ats::{ax, AliasStore, AttestationStore, QueryStore, TemporalConfig};
use ats::{Attestation, Parser};
use ats_sqlite::SqliteStore;
use backend::{Kind, Store};
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
    backend: Kind,
    db: String,
    #[cfg_attr(not(any(feature = "duckdb", feature = "postgres")), allow(dead_code))]
    namespace: String,
    ca: Option<String>,
    sync: Option<String>,
    key: Option<String>,
    unsigned: bool,
    rest: Vec<String>,
}

fn opts(args: &[String]) -> Result<Opts, Box<Error>> {
    let mut backend = std::env::var("ATS_DEV_BACKEND").ok();
    let mut db = std::env::var("ATS_DEV_DB").ok();
    let mut namespace = "ats-dev".to_string();
    let mut ca = None;
    let mut sync = None;
    let mut key = None;
    let mut unsigned = false;
    let mut rest = Vec::new();
    let mut it = args.iter();
    while let Some(a) = it.next() {
        match a.as_str() {
            "--db" => db = Some(it.next().ok_or_else(|| fail("--db needs a file", "none given"))?.clone()),
            "--backend" => backend = Some(it.next().ok_or_else(|| fail("--backend needs a name", "sqlite|duckdb|postgres"))?.clone()),
            "--namespace" => namespace = it.next().ok_or_else(|| fail("--namespace needs a name", "none given"))?.clone(),
            "--ca" => ca = Some(it.next().ok_or_else(|| fail("--ca needs a file", "none given"))?.clone()),
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
    let backend = Kind::parse(backend.as_deref().unwrap_or("sqlite"))?;
    let db = match db {
        Some(d) => d,
        None => backend.default_db()?,
    };
    Ok(Opts { backend, db, namespace, ca, sync, key, unsigned, rest })
}

impl Opts {
}

fn open_sqlite(o: &Opts) -> Result<SqliteStore, Box<Error>> {
    if o.backend != Kind::Sqlite {
        return Err(fail("this command is SQLite's", format!("--backend {} given", o.backend.name())));
    }
    let s = SqliteStore::open(&o.db).map_err(|e| fail("could not open the database", e))?;
    if let Some(m) = &o.sync {
        s.connection()
            .pragma_update(None, "synchronous", m.as_str())
            .map_err(|e| fail("could not set synchronous", e))?;
    }
    Ok(s)
}

fn open(o: &Opts) -> Result<Store, Box<Error>> {
    if o.backend != Kind::Sqlite && o.sync.is_some() {
        return Err(fail("--sync is SQLite's", format!("--backend {} given", o.backend.name())));
    }
    if o.backend != Kind::Postgres && o.ca.is_some() {
        return Err(fail("--ca is Postgres's", format!("--backend {} given", o.backend.name())));
    }
    if o.backend == Kind::Duckdb {
        duckdb_location(o)?;
    }
    match o.backend {
        Kind::Sqlite => Ok(Store::Sqlite(open_sqlite(o)?)),
        #[cfg(feature = "duckdb")]
        Kind::Duckdb => {
            let location = duckdb_location(o)?;
            let s = ats_duckdb::DuckdbStore::open(location, &o.namespace)
                .map_err(|e| Box::new(e.sacred("")))?;
            Ok(Store::Duckdb(s))
        }
        #[cfg(feature = "postgres")]
        Kind::Postgres => {
            let s = ats_postgres::PostgresStore::open(&o.db, o.ca.as_deref().unwrap_or(""), &o.namespace)
                .map_err(|e| Box::new(e.sacred(None)))?;
            Ok(Store::Postgres(s))
        }
        #[cfg(not(feature = "duckdb"))]
        Kind::Duckdb => Err(unbuilt("duckdb")),
        #[cfg(not(feature = "postgres"))]
        Kind::Postgres => Err(unbuilt("postgres")),
    }
}

/// The location DuckDB is opened at: a plain path is a directory on this machine,
/// made absolute, a url is taken as given. ats-duckdb writes the location, the
/// namespace and the file name into a COPY statement inside quotes and does not
/// escape them, so an apostrophe in any of what it will write fails at the first
/// flush, after the directories are made. Refused here, on the resolved value:
/// a relative `--db` under a working directory with an apostrophe has one too.
fn duckdb_location(o: &Opts) -> Result<String, Box<Error>> {
    let location = if o.db.contains("://") {
        o.db.clone()
    } else {
        let abs = std::path::absolute(&o.db).map_err(|e| fail("could not make the path absolute", e))?;
        format!("file://{}", abs.display())
    };
    for (what, value) in [("location", &location), ("namespace", &o.namespace)] {
        if value.contains('\'') {
            return Err(fail(
                &format!("a DuckDB {what} cannot contain an apostrophe"),
                format!("{value}: ats-duckdb puts it inside a quoted SQL string, unescaped"),
            ));
        }
    }
    Ok(location)
}

#[cfg(not(all(feature = "duckdb", feature = "postgres")))]
fn unbuilt(feature: &str) -> Box<Error> {
    fail(
        &format!("this ats-dev was built without {feature}"),
        format!("cargo build -p ats-dev --features {feature}"),
    )
}

/// The signing key, loaded the first time something needs a signature made,
/// never for reading, `--unsigned`, or a signature already given.
struct Keys {
    path: Result<std::path::PathBuf, Box<Error>>,
    unsigned: bool,
    loaded: std::cell::OnceCell<sign::Signer>,
}

impl Keys {
    fn of(o: &Opts) -> Self {
        Keys { path: sign::key_path(o.key.as_deref()), unsigned: o.unsigned, loaded: std::cell::OnceCell::new() }
    }

    fn path(&self) -> Result<&std::path::Path, Box<Error>> {
        match &self.path {
            Ok(p) => Ok(p),
            Err(e) => Err(e.clone()),
        }
    }

    /// The key if one is kept; none kept is not an error here.
    fn existing(&self) -> Result<Option<&sign::Signer>, Box<Error>> {
        if self.unsigned {
            return Ok(None);
        }
        if let Some(s) = self.loaded.get() {
            return Ok(Some(s));
        }
        Ok(sign::Signer::load_existing(self.path()?)?.map(|s| self.loaded.get_or_init(|| s)))
    }

    fn signer(&self) -> Result<Option<&sign::Signer>, Box<Error>> {
        if self.unsigned {
            return Ok(None);
        }
        if let Some(s) = self.loaded.get() {
            return Ok(Some(s));
        }
        let s = sign::Signer::load(self.path()?)?;
        Ok(Some(self.loaded.get_or_init(|| s)))
    }
}

/// What is written: a signature already given is checked and kept, none given
/// is made with the key, and an unsigned row is authored by ats-dev.
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
        Some(sg) => sg.sign(ready_to_sign(sg, a)?),
        None => Ok(a.authored_by(ACTOR)),
    }
}

/// The attestation as the node writes it under this key's signature, so that
/// what is signed here is what the node verifies there. The node writes a
/// request admitted as a key with that key's did:key leading the actors
/// (`actorsFor`, QNTX `server/attestation_handlers.go`), and a signature's
/// actors must be those; it takes a timestamp in seconds (`time.Unix(s, 0)`),
/// so a signature over a sub-second moment would not verify there.
fn ready_to_sign(sg: &sign::Signer, mut a: Attestation) -> Result<Attestation, Box<Error>> {
    if a.timestamp.rem_euclid(1000) != 0 {
        return Err(fail(
            "a signed attestation's timestamp is a whole second",
            format!("{}: timestamp {} ms; the node carries it in seconds, so a signature over milliseconds would not verify there", a.id, a.timestamp),
        ));
    }
    if a.actors.first() != Some(&sg.did) {
        a.actors.insert(0, sg.did.clone());
    }
    Ok(a)
}

/// Now, to the whole second: a timestamp the node carries as it was signed.
fn now_whole_ms() -> i64 {
    now_ms().div_euclid(1000) * 1000
}

fn run(args: &[String]) -> Result<(), Box<Error>> {
    let o = opts(args)?;
    let mut it = o.rest.iter();
    let cmd = it.next().ok_or_else(|| fail("no command", "key | verify | bench | put | ax | get | count | exists | ids | update | delete | clear | subjects | predicates | contexts | actors | stats | alias"))?;
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
        "ids" => lines(listed(&open(&o)?)?),
        "subjects" => lines(open(&o)?.sqlite("QueryStore")?.subjects().map_err(|e| fail("subjects failed", e))?),
        "predicates" => lines(open(&o)?.sqlite("QueryStore")?.predicates().map_err(|e| fail("predicates failed", e))?),
        "contexts" => lines(open(&o)?.sqlite("QueryStore")?.contexts().map_err(|e| fail("contexts failed", e))?),
        "actors" => lines(open(&o)?.sqlite("QueryStore")?.actors().map_err(|e| fail("actors failed", e))?),
        "stats" => {
            let st = open(&o)?.sqlite("QueryStore")?.stats().map_err(|e| fail("stats failed", e))?;
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
            let mut s = open(&o)?;
            if s.delete(id).map_err(|e| fail("delete failed", e))? {
                return s.persist();
            }
            // false says nothing was removed. If the store still has it, the
            // delete did not reach the row, and "no such attestation" would be untrue.
            if s.exists(id).map_err(|e| fail("exists failed", e))? {
                return Err(fail(
                    "delete removed nothing, and the attestation is still there",
                    format!("{id}: delete returned false, exists returns true ({})", s.kind().name()),
                ));
            }
            Err(fail("no such attestation", id))
        }
        "clear" => {
            if tail.first().map(|f| f.as_str()) != Some("--yes") {
                return Err(fail("clear removes every attestation", "say so: ats-dev clear --yes"));
            }
            let mut s = open(&o)?;
            s.clear().map_err(|e| fail("clear failed", e))?;
            s.persist()?;
            // Cleared, or said so: Ok with rows left is a clear that did not reach them.
            let left = s.count().map_err(|e| fail("count failed", e))?;
            if left != 0 {
                return Err(fail(
                    "clear returned Ok, and the store still holds attestations",
                    format!("{left} left ({})", s.kind().name()),
                ));
            }
            Ok(())
        }
        "update" => update(&o, &tail),
        "key" => {
            let path = sign::key_path(o.key.as_deref())?;
            let sg = sign::Signer::load(&path)?;
            println!("{}", serde_json::json!({ "did": sg.did, "file": path.display().to_string() }));
            Ok(())
        }
        "verify" => verify(&o, &tail),
        "push" => push(&o, &tail),
        "bench" => bench::run(&o, &tail),
        "alias" => alias(&o, &tail),
        other => Err(fail("unknown command", other)),
    }
}

/// Every id the store holds, or a refusal. `ids` and `count` are two questions
/// to a store, and one that answers fewer ids than it counts (DuckDB's `ids` reads
/// its buffer, `count` the Parquet files too) would make `verify` check nothing
/// and say so with an exit 0.
///
/// They are two reads, so a writer committing between them makes them differ
/// for a moment on a store that is complete. A mismatch that is still there on
/// the third reading is the store's.
fn listed(s: &Store) -> Result<Vec<String>, Box<Error>> {
    let mut last = (0, 0);
    for _ in 0..3 {
        let ids = s.ids().map_err(|e| fail("ids failed", e))?;
        let count = s.count().map_err(|e| fail("count failed", e))?;
        if ids.len() == count {
            return Ok(ids);
        }
        last = (ids.len(), count);
    }
    Err(fail(
        "ids does not list every stored attestation",
        format!("ids lists {}, count says {} ({}), on three readings", last.0, last.1, s.kind().name()),
    ))
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
    // When it was first stored is not an update's to change (`AttestationStore::update`),
    // so a patch naming `created_at` is ignored and the row must come back with the stored one.
    a.created_at = stored.created_at;
    // A signature covers what was just changed. It stays only if it still
    // holds; else ats-dev re-signs what its own key signed, and refuses the rest.
    if stored.signature.is_some() {
        if !matches!(sign::verify(&a), sign::Verdict::Valid(_)) {
            // Whose key it must be is known from the stored row; no key is minted to find out.
            let keys = Keys::of(o);
            let sg = match keys.existing()? {
                Some(sg) if stored.signer_did.as_deref() == Some(sg.did.as_str()) => sg,
                _ if o.unsigned => {
                    return Err(fail(
                        "the update breaks the signature",
                        format!("{id}: --unsigned cannot keep a signed attestation signed"),
                    ))
                }
                other => {
                    return Err(fail(
                        "the update breaks a signature ats-dev cannot make",
                        format!(
                            "{id} is signed by {}, the key kept is {}",
                            stored.signer_did.as_deref().unwrap_or("?"),
                            other.map(|k| k.did.as_str()).unwrap_or("none"),
                        ),
                    ))
                }
            };
            a.signature = None;
            a.signer_did = None;
            a = sg.sign(ready_to_sign(sg, a)?)?;
        }
    } else {
        // An unsigned row is authored as put authors it: an update that empties
        // the actors would otherwise hide the row from every actor's claims.
        a = a.authored_by(ACTOR);
    }
    s.update(a.clone()).map_err(|e| fail("update failed", e))?;
    s.persist()?;
    // What is on disk, not what was asked for: `created_at` is kept as it was.
    let kept = s.get(id).map_err(|e| fail("get failed", e))?.ok_or_else(|| fail("no such attestation", id))?;
    // An update that said Ok but left something else stored is a store that
    // did not do what its trait says; say so, with both rows.
    if kept != a {
        let held = serde_json::to_string(&kept).map_err(|e| fail("could not encode an attestation", e))?;
        let asked = serde_json::to_string(&a).map_err(|e| fail("could not encode an attestation", e))?;
        return Err(fail(
            "update returned Ok, and the store holds something else",
            format!("{} said Ok; asked: {asked}; stored: {held}", s.kind().name()),
        ));
    }
    emit(&kept)
}

fn alias(o: &Opts, tail: &[&String]) -> Result<(), Box<Error>> {
    let usage = "ats-dev alias add A B | rm A B | ls | resolve X";
    let sub = tail.first().ok_or_else(|| fail("alias needs a subcommand", usage))?;
    let mut store = open(o)?;
    let s = store.sqlite_mut("AliasStore")?;
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
        timestamp: now_whole_ms(),
        created_at: now_ms(),
        source: "ats-dev".into(),
        ..Attestation::default()
    };
    let mut a = a;
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
    s.persist()?;
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
    if !obj.contains_key("timestamp") { obj.insert("timestamp".into(), now_whole_ms().into()); }
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
    Ok(a)
}

fn put_stdin(s: &mut Store, keys: &Keys) -> Result<(), Box<Error>> {
    let mut batch: Vec<String> = Vec::new();
    let mut total = 0usize;
    for (n, line) in std::io::stdin().lock().lines().enumerate() {
        let at = n + 1;
        let line = match line {
            Ok(l) => l,
            // Rows before this one are already stored on a backend with no batch to undo.
            Err(e) => return abandon(s, &batch, fail(&format!("line {at}: could not read stdin"), e)),
        };
        if line.trim().is_empty() { continue; }
        let a = match complete(&line).and_then(|a| seal(keys, a)) {
            Ok(a) => a,
            Err(e) => return abandon(s, &batch, fail(&format!("line {at}: {}", e.title), e.why)),
        };
        if batch.is_empty() {
            if let Some(q) = s.as_sqlite() {
                q.connection().execute_batch("BEGIN").map_err(|e| fail("BEGIN failed", e))?;
            }
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
    if let Some(q) = s.as_sqlite() {
        q.wal_checkpoint_truncate().map_err(|e| fail("checkpoint failed", e))?;
    }
    eprintln!("ats-dev: {total} attestations written");
    Ok(())
}

/// Make the open batch durable, then say its ids: an id on stdout is a row
/// that is stored. SQLite COMMITs the transaction the batch ran in; the others
/// have no transaction across puts, so it is the backend's own `persist`.
fn commit(s: &Store, batch: &mut Vec<String>) -> Result<(), Box<Error>> {
    if batch.is_empty() { return Ok(()); }
    match s.as_sqlite() {
        Some(q) => q.connection().execute_batch("COMMIT").map_err(|e| fail("COMMIT failed", e))?,
        None => s.persist()?,
    }
    for id in batch.drain(..) { emit_id(&id)?; }
    Ok(())
}

/// A line failed. SQLite rolls the open batch back and none of its ids are
/// said. Another backend has already stored each put, so those rows stay: they
/// are made durable and their ids said, and the failure follows.
fn abandon(s: &Store, batch: &[String], why: Box<Error>) -> Result<(), Box<Error>> {
    if batch.is_empty() { return Err(why); }
    match s.as_sqlite() {
        Some(q) => {
            q.connection().execute_batch("ROLLBACK").map_err(|e| fail("ROLLBACK failed", e))?;
            eprintln!("ats-dev: rolled back {} attestations of the open batch", batch.len());
        }
        None => {
            s.persist()?;
            for id in batch { emit_id(id)?; }
            eprintln!("ats-dev: {} has no batch to roll back; {} attestations before the failure stay written", s.kind().name(), batch.len());
        }
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
    let store = open(o)?;
    let s = store.sqlite("QueryStore and AliasStore")?;
    let res = ax::execute_with_aliases(s, &req.filter, &TemporalConfig::default(), now)
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
        None => listed(&s)?,
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

/// Rows per request: one signature is spent per request, and the node answers
/// a list row by row, in the order sent (`handleCreateAttestation`).
const PUSH_BATCH: usize = 100;

/// Bytes a request's body may reach: the node reads at most 10 MB of one
/// (`maxAttestationBody`) and refuses the whole request past it, so a batch is
/// sent before a row would take it over this, and a row larger on its own is
/// not sent at all.
const PUSH_BODY_BUDGET: usize = 8 * 1024 * 1024;

/// Seconds a request's signature lasts: long enough to reach the node, well
/// inside the five minutes the node allows one (what `qntx sign` gives).
const PUSH_SIGNED_FOR: i64 = 60;

/// Send rows to a node: `POST NODE/api/attestations`, each request signed by
/// the key for that node. The node answers every row; each answer is a line on
/// stdout, as the node gave it. A row signed by the key is stored as signed,
/// an unsigned one is signed by the node, one signed by another key is refused
/// there unless the node's grant holds that key.
///
/// Rows are read and sent a batch at a time, so a large store is never held
/// whole. Every id asked for gets a line, whatever happens to the network: a
/// request the node did not answer reports its rows, and the rows after it are
/// reported as not sent.
fn push(o: &Opts, tail: &[&String]) -> Result<(), Box<Error>> {
    let usage = "ats-dev push https://node.example [ID...]";
    let node = tail.first().ok_or_else(|| fail("push needs a node", usage))?;
    let origin = origin_of(node)?;
    let url = format!("{}/api/attestations", node.trim_end_matches('/'));
    let sg = sign::Signer::load(&sign::key_path(o.key.as_deref())?)?;
    let s = open(o)?;
    let ids: Vec<String> = match tail.get(1..) {
        Some(given) if !given.is_empty() => given.iter().map(|x| x.to_string()).collect(),
        _ => listed(&s)?,
    };
    // A node that accepts the connection and never answers would hold push
    // forever; past the signature's life the node would refuse it anyway.
    let agent: ureq::Agent = ureq::Agent::config_builder()
        .http_status_as_error(false)
        .timeout_global(Some(std::time::Duration::from_secs(PUSH_SIGNED_FOR.unsigned_abs())))
        .build()
        .into();
    let mut p = Pushing { out: std::io::stdout().lock(), tally: Tally::default() };
    let mut batch: Vec<(String, serde_json::Value)> = Vec::with_capacity(PUSH_BATCH);
    // The body is `[` rows joined by `,` `]`.
    let mut bytes = 2usize;
    let mut ids = ids.into_iter();
    let mut broken: Option<String> = None;
    for id in ids.by_ref() {
        let row = match s.get(&id).map_err(|e| fail("get failed", e))? {
            None => {
                p.refused(&id, "no such attestation")?;
                continue;
            }
            Some(a) => match wire(&a) {
                Ok(v) => v,
                Err(why) => {
                    p.refused(&id, &why)?;
                    continue;
                }
            },
        };
        let size = row.to_string().len();
        if size + 2 > PUSH_BODY_BUDGET {
            p.refused(&id, &format!("the row is {size} bytes; a request to the node carries at most {PUSH_BODY_BUDGET}"))?;
            continue;
        }
        if !batch.is_empty() && bytes + size + 1 > PUSH_BODY_BUDGET {
            match p.send(&agent, &url, &origin, &sg, &mut batch) {
                Ok(()) => bytes = 2,
                Err(Halt::Unanswered(why)) => {
                    broken = Some(why);
                    p.refused(&id, &format!("not sent: {}", broken.as_deref().unwrap_or("")))?;
                    break;
                }
                Err(Halt::Unsaid(e)) => return Err(e),
            }
        }
        bytes += size + 1;
        batch.push((id, row));
        if batch.len() == PUSH_BATCH {
            bytes = 2;
            match p.send(&agent, &url, &origin, &sg, &mut batch) {
                Ok(()) => {}
                Err(Halt::Unanswered(why)) => {
                    broken = Some(why);
                    break;
                }
                Err(Halt::Unsaid(e)) => return Err(e),
            }
        }
    }
    if broken.is_none() && !batch.is_empty() {
        match p.send(&agent, &url, &origin, &sg, &mut batch) {
            Ok(()) => {}
            Err(Halt::Unanswered(why)) => broken = Some(why),
            Err(Halt::Unsaid(e)) => return Err(e),
        }
    }
    if let Some(why) = &broken {
        for id in ids {
            p.refused(&id, &format!("not sent: {why}"))?;
        }
    }
    let t = &p.tally;
    eprintln!(
        "ats-dev: {} sent to {url} as {}: {} created, {} the node already had under that id (what it holds was not compared), {} not taken",
        t.sent, sg.did, t.created, t.existed, t.not_taken
    );
    if t.not_taken > 0 {
        return Err(fail("rows the node did not take", t.not_taken));
    }
    Ok(())
}

#[derive(Default)]
struct Tally {
    sent: usize,
    created: usize,
    existed: usize,
    not_taken: usize,
}

/// Why a batch stopped push: the node did not answer it, and the rows after it
/// go unsent; or a row's line could not be written, and push ends there, whatever
/// the node took, since a row whose answer was not said is not one to call done.
enum Halt {
    Unanswered(String),
    Unsaid(Box<Error>),
}

/// Where push says what became of each row.
struct Pushing<W: Write> {
    out: W,
    tally: Tally,
}

impl<W: Write> Pushing<W> {
    fn say(&mut self, line: serde_json::Value) -> Result<(), Box<Error>> {
        writeln!(self.out, "{line}").map_err(|e| fail("could not write stdout", e))
    }

    fn refused(&mut self, id: &str, why: &str) -> Result<(), Box<Error>> {
        self.tally.not_taken += 1;
        self.say(serde_json::json!({ "id": id, "refused": why }))
    }

    /// One request for the batch, and a line for each of its rows; the batch is
    /// emptied either way.
    fn send(
        &mut self,
        agent: &ureq::Agent,
        url: &str,
        origin: &str,
        sg: &sign::Signer,
        batch: &mut Vec<(String, serde_json::Value)>,
    ) -> Result<(), Halt> {
        let rows: Vec<(String, serde_json::Value)> = std::mem::take(batch);
        let unanswered = |p: &mut Self, why: String| -> Result<(), Halt> {
            for (id, _) in &rows {
                p.refused(id, &why).map_err(Halt::Unsaid)?;
            }
            Err(Halt::Unanswered(why))
        };
        let auth = match sg.authorization(origin, PUSH_SIGNED_FOR) {
            Ok(a) => a,
            Err(e) => return unanswered(self, format!("the request was not signed: {}", e.why)),
        };
        let body = serde_json::Value::Array(rows.iter().map(|(_, v)| v.clone()).collect()).to_string();
        self.tally.sent += rows.len();
        let mut resp = match agent.post(url).header("Authorization", &auth).header("Content-Type", "application/json").send(body) {
            Ok(r) => r,
            Err(e) => return unanswered(self, format!("{url} did not answer: {e}")),
        };
        let status = resp.status().as_u16();
        let text = match resp.body_mut().read_to_string() {
            Ok(t) => t,
            Err(e) => return unanswered(self, format!("{url} answered {status} and the body did not read: {e}")),
        };
        let answer: serde_json::Value = serde_json::from_str(&text).unwrap_or(serde_json::Value::String(text.clone()));
        let results = answer.get("results").and_then(|r| r.as_array()).filter(|r| status == 200 && r.len() == rows.len());
        match results {
            Some(results) => {
                for ((id, _), r) in rows.iter().zip(results) {
                    let st = r.get("status").and_then(|x| x.as_u64()).unwrap_or(0);
                    let existed = st == 200 && r.get("answer").and_then(|a| a.get("status")).and_then(|x| x.as_str()) == Some("exists");
                    match st {
                        201 => self.tally.created += 1,
                        200 if existed => self.tally.existed += 1,
                        _ => self.tally.not_taken += 1,
                    }
                    self.say(serde_json::json!({ "id": id, "status": st, "answer": r.get("answer") })).map_err(Halt::Unsaid)?;
                }
            }
            // The request itself was not taken (the gate, a body the node did
            // not read): every row of it is answered with what the node said.
            None => {
                for (id, _) in &rows {
                    self.tally.not_taken += 1;
                    self.say(serde_json::json!({ "id": id, "status": status, "answer": answer })).map_err(Halt::Unsaid)?;
                }
            }
        }
        Ok(())
    }
}

/// The origin a request's signature is made out to: the scheme and the
/// authority as the node url names them, as `qntx sign` makes it
/// (`cmd/qntx/commands/element.go`, `signedFor`) and ground's signer does. The
/// node compares it with its configured origin exactly, so the url is named as
/// the node names itself, an explicit port included. A query or a fragment is
/// refused: `/api/attestations` is appended to the url, and would land in it.
fn origin_of(node: &str) -> Result<String, Box<Error>> {
    if node.contains(['?', '#']) {
        return Err(fail("a node url names no query or fragment", node));
    }
    let (scheme, rest) = node.split_once("://").ok_or_else(|| fail("a node is a url", node))?;
    if scheme != "http" && scheme != "https" {
        return Err(fail("a node is reached over http or https", node));
    }
    let authority = rest.split('/').next().unwrap_or("");
    if authority.is_empty() {
        return Err(fail("a node url names a host", node));
    }
    Ok(format!("{scheme}://{authority}"))
}

/// One row as `POST /api/attestations` takes it: the timestamp in seconds, the
/// signature as Go reads a `[]byte`, `created_at` left to the node. A row whose
/// timestamp is not a whole second is not sent: the node would store another
/// moment, and a signature over this one would not verify there.
fn wire(a: &Attestation) -> Result<serde_json::Value, String> {
    use base64::Engine;
    if a.timestamp.rem_euclid(1000) != 0 {
        return Err(format!("timestamp {} ms is not a whole second; the node takes seconds", a.timestamp));
    }
    let mut v = serde_json::json!({
        "id": a.id, "subjects": a.subjects, "predicates": a.predicates, "contexts": a.contexts,
        "actors": a.actors, "timestamp": a.timestamp.div_euclid(1000), "source": a.source,
        "attributes": a.attributes,
    });
    match (&a.signature, &a.signer_did, v.as_object_mut()) {
        (Some(sig), Some(did), Some(m)) => {
            m.insert("signature".into(), base64::engine::general_purpose::STANDARD.encode(sig).into());
            m.insert("signer_did".into(), did.clone().into());
        }
        (None, None, _) => {}
        // Sent without the half it has, the node would store it unsigned and
        // sign it as itself: a row that claimed a signer would lose the claim.
        _ => return Err("a signature and a signer are given together or not at all; this row has one".into()),
    }
    Ok(v)
}

#[cfg(test)]
#[allow(clippy::unwrap_used)]
mod tests {
    use super::origin_of;

    /// The node compares the audience with its configured origin exactly, so
    /// it is the url's own, as `qntx sign` makes it.
    #[test]
    fn a_node_url_is_made_out_to_its_origin_as_qntx_sign_makes_it() {
        for (url, origin) in [
            ("https://node.example", "https://node.example"),
            ("https://node.example/", "https://node.example"),
            ("https://node.example/api/", "https://node.example"),
            ("https://node.example:443", "https://node.example:443"),
            ("http://127.0.0.1:8820/x", "http://127.0.0.1:8820"),
        ] {
            assert_eq!(origin_of(url).unwrap(), origin, "{url}");
        }
        for bad in ["node.example", "ftp://node.example", "HTTPS://node.example", "https://", "https:///x", "https://node.example?x=y", "https://node.example/#a"] {
            assert!(origin_of(bad).is_err(), "{bad}");
        }
    }
}
