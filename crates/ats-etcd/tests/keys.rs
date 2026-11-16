#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]
//! 「[Opus 5.5] Keys set whole under a prefix, against the etcd QNTX_ETCD_ENDPOINT
//! 「[Opus 5.5] names (scripts/with-etcd.sh). Unset fails rather than skips.」」

use std::collections::BTreeMap;
use std::sync::atomic::{AtomicUsize, Ordering};

use ats_etcd::{EtcdError, EtcdKeys};

fn endpoint() -> String {
    std::env::var("QNTX_ETCD_ENDPOINT")
        .expect("QNTX_ETCD_ENDPOINT names no etcd; run under scripts/with-etcd.sh")
}

/// 「[Opus 5.5] A prefix no other test touches.」
fn prefix() -> String {
    static N: AtomicUsize = AtomicUsize::new(0);
    format!(
        "/keys-test/{}_{}/",
        std::process::id(),
        N.fetch_add(1, Ordering::SeqCst)
    )
}

fn whole(entries: &[(&str, &str)]) -> BTreeMap<String, Vec<u8>> {
    entries
        .iter()
        .map(|(k, v)| (k.to_string(), v.as_bytes().to_vec()))
        .collect()
}

#[test]
fn what_is_set_is_what_the_prefix_holds() {
    let mut keys = EtcdKeys::open(&endpoint(), &prefix()).unwrap();
    keys.set(&whole(&[
        ("did:key:a", "{\"to\":\"A\"}"),
        ("did:key:b", "{\"to\":\"B\"}"),
    ]))
    .unwrap();
    assert_eq!(
        keys.held().unwrap(),
        whole(&[
            ("did:key:a", "{\"to\":\"A\"}"),
            ("did:key:b", "{\"to\":\"B\"}")
        ])
    );

    // 「[Opus 5.5] b is gone from the whole, so it is gone from etcd; a is changed.」
    keys.set(&whole(&[("did:key:a", "{\"to\":\"A2\"}")]))
        .unwrap();
    assert_eq!(
        keys.held().unwrap(),
        whole(&[("did:key:a", "{\"to\":\"A2\"}")])
    );

    keys.set(&BTreeMap::new()).unwrap();
    assert!(keys.held().unwrap().is_empty());
}

/// 「[Opus 5.5] A value that did not change is not put again: its revision stands,
/// 「[Opus 5.5] so a watcher is told nothing of it.」」
#[test]
fn what_did_not_change_is_not_put_again() {
    let at = prefix();
    let mut keys = EtcdKeys::open(&endpoint(), &at).unwrap();
    keys.set(&whole(&[("a", "1")])).unwrap();
    let first = keys.revision_of("a").unwrap();
    keys.set(&whole(&[("a", "1"), ("b", "2")])).unwrap();
    assert_eq!(keys.revision_of("a").unwrap(), first);
}

#[test]
fn prefixes_do_not_see_each_other() {
    let mut a = EtcdKeys::open(&endpoint(), &prefix()).unwrap();
    let b = EtcdKeys::open(&endpoint(), &prefix()).unwrap();
    a.set(&whole(&[("x", "1")])).unwrap();
    assert!(b.held().unwrap().is_empty());
}

#[test]
fn a_prefix_that_is_not_one_is_refused() {
    assert!(matches!(
        EtcdKeys::open(&endpoint(), "collected"),
        Err(EtcdError::BadPrefix { .. })
    ));
}
