#![allow(clippy::unwrap_used, clippy::expect_used)]
//! `SqliteStore` against `AttestationStore`'s contract, in the session that
//! wrote and after the file has been closed and opened again.

use ats::storage::conformance::check;
use ats_sqlite::SqliteStore;

#[test]
fn sqlite_does_what_the_trait_says() {
    let dir = tempfile::tempdir().unwrap();
    let path = dir.path().join("t.db");
    let store = SqliteStore::open(&path).unwrap();
    let found = check(
        store,
        &mut |s| {
            drop(s);
            SqliteStore::open(&path).map_err(|e| ats::storage::StoreError::Backend(e.to_string()))
        },
        "sqlite",
        true,
    );
    assert!(found.is_empty(), "{found:#?}");
}
