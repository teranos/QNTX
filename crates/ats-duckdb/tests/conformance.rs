#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]
//! `DuckdbStore` against `AttestationStore`'s contract. `reopen` drops the
//! store, which flushes the buffer to a Parquet file, and opens the location
//! again: a new process's view of what the first one left.
//!
//! DuckDB does not meet the contract after a flush, and this test says exactly
//! where. `ids`, `delete`, `update` and `clear` run against the in-memory buffer
//! table; `get`, `exists` and `count` also read the Parquet files. ADR-024 says
//! "Files are immutable once written" and defines no delete or update for them,
//! so a row in a file cannot be changed through the trait: `update` returns
//! `Ok(())` and `delete` returns `false` with the row still there.
//!
//! `KNOWN` is that gap, by (case, step). The test fails on a violation that is
//! not in it, and on an entry that no longer happens: whoever makes DuckDB do
//! what the trait says, or makes the trait say what DuckDB can, removes the
//! entry here. What a delete or an update means over immutable files is
//! ADR-024's to decide; this list is not that decision.

use std::collections::BTreeSet;

use ats::storage::conformance::check;
use ats::storage::StoreError;
use ats_duckdb::DuckdbStore;

const KNOWN: [(&str, &str); 9] = [
    ("clear/after_reopen", "count after clear"),
    ("clear/after_reopen", "count after clear and reopen"),
    ("delete/after_reopen", "delete of a stored id"),
    ("delete/after_reopen", "exists after delete"),
    ("delete/after_reopen", "exists after delete and reopen"),
    ("delete/after_reopen", "get after delete"),
    ("ids_and_count/after_reopen", "ids lists every stored id"),
    ("update/after_reopen", "after update"),
    ("update/after_reopen", "after update and reopen"),
];

#[test]
fn duckdb_does_what_the_trait_says_except_where_adr_024_leaves_it_open() {
    let dir = tempfile::tempdir().unwrap();
    let location = format!("file://{}", dir.path().display());
    let store = DuckdbStore::open(location.clone(), "default").unwrap();
    let found = check(
        store,
        &mut |s| {
            drop(s);
            DuckdbStore::open(location.clone(), "default")
                .map_err(|e| StoreError::Backend(format!("{e:?}")))
        },
        "duckdb",
        true,
    );
    let seen: BTreeSet<(&str, &str)> = found.iter().map(|v| (v.case.as_str(), v.step)).collect();
    let known: BTreeSet<(&str, &str)> = KNOWN.into_iter().collect();
    let new: Vec<_> = found.iter().filter(|v| !known.contains(&(v.case.as_str(), v.step))).collect();
    let fixed: Vec<_> = known.difference(&seen).collect();
    assert!(new.is_empty(), "violations not in KNOWN: {new:#?}");
    assert!(fixed.is_empty(), "no longer happen, remove from KNOWN: {fixed:#?}");
}
