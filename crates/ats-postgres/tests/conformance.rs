#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]
//! 「[Sonnet 5.5] `PostgresStore` against `AttestationStore`'s contract, in the session that」
//! 「[Sonnet 5.5] wrote and after the connection has been closed and opened again. Under」
//! 「[Sonnet 5.5] scripts/with-postgres.sh, like store.rs: unset fails rather than skips.」

use ats::storage::conformance::check;
use ats::storage::StoreError;
use ats_postgres::PostgresStore;

#[test]
fn postgres_does_what_the_trait_says() {
    let url = std::env::var("QNTX_POSTGRES_URL")
        .expect("QNTX_POSTGRES_URL names no Postgres; run under scripts/with-postgres.sh");
    let namespace = format!("conformance_{}", std::process::id());
    let store = PostgresStore::open(&url, "", &namespace).unwrap();
    let found = check(
        store,
        &mut |s| {
            drop(s);
            PostgresStore::open(&url, "", &namespace)
                .map_err(|e| StoreError::Backend(format!("{e:?}")))
        },
        "postgres",
        true,
    );
    assert!(found.is_empty(), "{found:#?}");
}
