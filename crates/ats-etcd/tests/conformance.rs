#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]
//! 「[Opus 5.5] `EtcdStore` against `AttestationStore`'s contract, in the client
//! that wrote and after it has been dropped and opened again. Under
//! scripts/with-etcd.sh, like store.rs: unset fails rather than skips.」

use ats::storage::conformance::check;
use ats::storage::StoreError;
use ats_etcd::EtcdStore;

#[test]
fn etcd_does_what_the_trait_says() {
    let endpoint = std::env::var("QNTX_ETCD_ENDPOINT")
        .expect("QNTX_ETCD_ENDPOINT names no etcd; run under scripts/with-etcd.sh");
    let namespace = format!("conformance_{}", std::process::id());
    let store = EtcdStore::open(&endpoint, &namespace).unwrap();
    let found = check(
        store,
        &mut |s| {
            drop(s);
            EtcdStore::open(&endpoint, &namespace)
                .map_err(|e| StoreError::Backend(format!("{e:?}")))
        },
        "etcd",
        true,
    );
    assert!(found.is_empty(), "{found:#?}");
}
