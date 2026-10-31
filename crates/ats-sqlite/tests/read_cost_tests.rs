#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]
//! What a read costs is the rows it answers with, not the rows the file holds.

// "I WANT TO BE ABLE TO STORE A FUCKTON OF DATA"

use ats::{storage::AttestationStore, AttestationBuilder, AxFilter};
use ats_sqlite::store::build_query_sql;
use ats_sqlite::SqliteStore;
use rusqlite::StatementStatus;

const LIMIT: usize = 30;
const NEW: i64 = 1_000_000;

/// Rows `from..to`, each a second after the last; every name repeats.
fn fill(store: &mut SqliteStore, from: i64, to: i64) {
    for i in from..to {
        let mut a = AttestationBuilder::new()
            .id(format!("AS-{}", i))
            .subject(format!("s{}", i % 7))
            .predicate(format!("p{}", i % 5))
            .context(format!("c{}", i % 3))
            .actor(format!("a{}", i % 4))
            .timestamp(i * 1000)
            .source("test")
            .build();
        a.created_at = i * 1000;
        store.put(a).unwrap();
    }
}

/// The sqlite VM steps the read takes, and the ids it answers with.
fn read(store: &SqliteStore, filter: &AxFilter) -> (i32, Vec<String>) {
    let (sql, params) = build_query_sql(filter);
    let mut stmt = store.connection().prepare(&sql).unwrap();
    let ids = stmt
        .query_map(rusqlite::params_from_iter(params.iter()), |row| {
            row.get::<_, String>(0)
        })
        .unwrap()
        .map(|id| id.unwrap())
        .collect();
    (stmt.get_status(StatementStatus::VmStep), ids)
}

fn shapes() -> Vec<(&'static str, AxFilter)> {
    let one = |f: &dyn Fn(&mut AxFilter)| {
        let mut x = AxFilter {
            limit: LIMIT,
            ..AxFilter::default()
        };
        f(&mut x);
        x
    };
    let since = (NEW + 1_000) * 1000;
    vec![
        ("newest", one(&|_| {})),
        ("a subject", one(&|f| f.subjects = vec!["s1".into()])),
        ("a predicate", one(&|f| f.predicates = vec!["p1".into()])),
        ("a context", one(&|f| f.contexts = vec!["c1".into()])),
        ("an actor", one(&|f| f.actors = vec!["a1".into()])),
        ("two predicates", one(&|f| f.predicates = vec!["p1".into(), "p2".into()])),
        ("a subject and a predicate", one(&|f| {
            f.subjects = vec!["s1".into()];
            f.predicates = vec!["p1".into()];
        })),
        ("since a time", one(&|f| f.time_start = Some(since))),
        ("a predicate since a time", one(&|f| {
            f.predicates = vec!["p1".into()];
            f.time_start = Some(since);
        })),
    ]
}

// On k the ground namespace held 533,847 rows, and a newest-first read
// walked them all and sorted them before its LIMIT.
#[test]
fn older_rows_cost_a_read_nothing() {
    let mut small = SqliteStore::in_memory().unwrap();
    fill(&mut small, NEW, NEW + 2_000);
    let mut big = SqliteStore::in_memory().unwrap();
    fill(&mut big, 0, 20_000);
    fill(&mut big, NEW, NEW + 2_000);

    for (name, f) in shapes() {
        let (few, answer) = read(&small, &f);
        let (many, same) = read(&big, &f);
        assert_eq!(answer.len(), LIMIT, "{}", name);
        assert_eq!(answer, same, "{}: ten times the older rows changed the answer", name);
        assert!(
            many <= few + few / 10,
            "{}: {} steps with 2,000 rows, {} with 22,000 — the read pays for rows it does not answer with",
            name,
            few,
            many
        );
    }
}

// On k ramen asked the ground namespace for what came since a time nothing
// had come since, every few seconds, and each read walked all 543,573 rows.
#[test]
fn a_window_with_less_than_a_page_costs_only_its_rows() {
    let mut small = SqliteStore::in_memory().unwrap();
    fill(&mut small, NEW, NEW + 2_000);
    let mut big = SqliteStore::in_memory().unwrap();
    fill(&mut big, 0, 20_000);
    fill(&mut big, NEW, NEW + 2_000);

    let window = |from: i64, f: &dyn Fn(&mut AxFilter)| {
        let mut x = AxFilter {
            limit: LIMIT,
            time_start: Some(from * 1000),
            ..AxFilter::default()
        };
        f(&mut x);
        x
    };
    let empty = NEW + 2_000;
    let short = NEW + 2_000 - 10;
    let shapes: Vec<(&str, AxFilter, usize)> = vec![
        ("since nothing came", window(empty, &|_| {}), 0),
        ("since ten came", window(short, &|_| {}), 10),
        ("a predicate since nothing came", window(empty, &|f| f.predicates = vec!["p1".into()]), 0),
        ("a predicate since ten came", window(short, &|f| f.predicates = vec!["p1".into()]), 2),
    ];
    for (name, f, rows) in shapes {
        let (few, answer) = read(&small, &f);
        let (many, same) = read(&big, &f);
        assert_eq!(answer.len(), rows, "{}", name);
        assert_eq!(answer, same, "{}: ten times the older rows changed the answer", name);
        assert!(
            many <= few + few / 10,
            "{}: {} steps with 2,000 rows, {} with 22,000 — the read pays for rows outside its window",
            name,
            few,
            many
        );
    }
}

// On k's disk every row read for its timestamp was a seek: a window of 3,889
// rows took 27s to answer 301 of them.
#[test]
fn a_window_reads_only_the_rows_it_answers_with() {
    let mut store = SqliteStore::in_memory().unwrap();
    fill(&mut store, NEW, NEW + 2_000);
    for (name, f) in shapes() {
        if f.time_start.is_none() {
            continue;
        }
        let (sql, params) = build_query_sql(&f);
        let mut stmt = store
            .connection()
            .prepare(&format!("EXPLAIN QUERY PLAN {}", sql))
            .unwrap();
        let plan: Vec<String> = stmt
            .query_map(rusqlite::params_from_iter(params.iter()), |row| {
                row.get::<_, String>(3)
            })
            .unwrap()
            .map(|step| step.unwrap())
            .collect();
        assert!(
            plan.iter()
                .any(|step| step.contains("COVERING INDEX idx_attestations_window")),
            "{}: the window is not chosen inside its index: {:?}",
            name,
            plan
        );
    }
}

#[test]
fn a_row_named_twice_is_answered_once() {
    let mut store = SqliteStore::in_memory().unwrap();
    let mut a = AttestationBuilder::new()
        .id("AS-both")
        .subject("s")
        .predicate("p1")
        .predicate("p2")
        .source("test")
        .build();
    a.created_at = 5_000;
    store.put(a).unwrap();
    let f = AxFilter {
        predicates: vec!["p1".into(), "p2".into()],
        limit: LIMIT,
        ..AxFilter::default()
    };
    assert_eq!(read(&store, &f).1, vec!["AS-both".to_string()]);
}

// internal/testing/database.go writes junction rows without created_at.
#[test]
fn a_junction_row_written_without_its_age_is_given_it() {
    let mut store = SqliteStore::in_memory().unwrap();
    fill(&mut store, 7, 8);
    let conn = store.connection();
    conn.execute(
        "INSERT INTO attestation_predicates (attestation_id, predicate) VALUES ('AS-7', 'late')",
        [],
    )
    .unwrap();
    let age: Option<String> = conn
        .query_row(
            "SELECT created_at FROM attestation_predicates WHERE predicate = 'late'",
            [],
            |row| row.get(0),
        )
        .unwrap();
    let own: String = conn
        .query_row("SELECT created_at FROM attestations WHERE id = 'AS-7'", [], |row| row.get(0))
        .unwrap();
    assert_eq!(age, Some(own));
}

#[test]
fn the_count_is_kept_not_counted() {
    let count = |store: &SqliteStore| {
        let mut stmt = store.connection().prepare(ats_sqlite::store::COUNT_SQL).unwrap();
        let n: i64 = stmt.query_row([], |row| row.get(0)).unwrap();
        (stmt.get_status(StatementStatus::VmStep), n)
    };
    let mut small = SqliteStore::in_memory().unwrap();
    fill(&mut small, NEW, NEW + 2_000);
    let mut big = SqliteStore::in_memory().unwrap();
    fill(&mut big, 0, 20_000);
    let (few, _) = count(&small);
    let (many, n) = count(&big);
    assert_eq!(n, 20_000);
    assert_eq!(few, many, "counting 20,000 rows took more steps than counting 2,000");

    big.delete("AS-3").unwrap();
    assert_eq!(big.count().unwrap(), 19_999);
    big.clear().unwrap();
    assert_eq!(big.count().unwrap(), 0);
}
