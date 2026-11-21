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

/// 「[Opus 5.5] What a prefix holds is read a key at a time: six values of 900 KB are
/// 「[Opus 5.5] past the 4 MiB one gRPC answer may carry, and set and held still work,」
/// 「[Opus 5.5] the delete among them (CodeRabbit on #1768).」」
#[test]
fn what_is_held_is_read_past_one_answers_limit() {
    let mut keys = EtcdKeys::open(&endpoint(), &prefix()).unwrap();
    let big = "x".repeat(900_000);
    let six: Vec<(String, String)> = (0..6)
        .map(|i| (format!("did:key:{i}"), format!("{i}{big}")))
        .collect();
    let whole: BTreeMap<String, Vec<u8>> = six
        .iter()
        .map(|(k, v)| (k.clone(), v.as_bytes().to_vec()))
        .collect();
    keys.set(&whole).unwrap();
    assert_eq!(keys.held().unwrap().len(), 6);
    keys.set(&BTreeMap::new()).unwrap();
    assert!(keys.held().unwrap().is_empty());
}

/// 「[Opus 5.5] Names are read a page at a time, and every page is read.」
#[test]
fn every_page_of_names_is_read() {
    let mut keys = EtcdKeys::open(&endpoint(), &prefix()).unwrap();
    let many: BTreeMap<String, Vec<u8>> = (0..2500)
        .map(|i| (format!("did:key:{i:05}"), b"{}".to_vec()))
        .collect();
    keys.set(&many).unwrap();
    assert_eq!(keys.names().unwrap().len(), 2500);
    keys.set(&BTreeMap::new()).unwrap();
    assert!(keys.names().unwrap().is_empty());
}

/// 「[Opus 5.5] A page is a count of names, and names can be long: 1,100 names of
/// 「[Opus 5.5] 4,200 bytes pass 4 MiB in one page, and are still read, and deleted」
/// 「[Opus 5.5] (CodeRabbit on #1768).」」
#[test]
fn long_names_are_read_past_one_answers_limit() {
    let mut keys = EtcdKeys::open(&endpoint(), &prefix()).unwrap();
    let long = "n".repeat(4200);
    let many: BTreeMap<String, Vec<u8>> = (0..1100)
        .map(|i| (format!("{i:05}{long}"), b"{}".to_vec()))
        .collect();
    keys.set(&many).unwrap();
    assert_eq!(keys.names().unwrap().len(), 1100);
    keys.set(&BTreeMap::new()).unwrap();
    assert!(keys.names().unwrap().is_empty());
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

// "LAYE OWNS IDENTITY"

/// 「[Opus 5.5] A swap writes only while what it compared is as it was read; one that
/// 「[Opus 5.5] another wrote between is not taken, and writes nothing.」」
#[test]
fn a_swap_is_taken_only_while_nothing_moved() {
    let base = prefix();
    let keys = EtcdKeys::open(&endpoint(), &base).unwrap();
    let name = format!("{base}names/alder");
    let named = format!("{base}named/did:key:a");

    // 「[Opus 5.5] Neither is held: each is compared at 0.」
    let free: BTreeMap<String, i64> = [(name.clone(), 0), (named.clone(), 0)].into();
    let put = whole(&[
        (name.as_str(), "{\"did\":\"did:key:a\"}"),
        (named.as_str(), "alder"),
    ]);
    assert!(keys.swap(&free, &put, &[]).unwrap());

    let read = keys
        .read(&[name.clone(), named.clone(), format!("{base}names/none")])
        .unwrap();
    assert_eq!(read.len(), 2, "a key etcd does not hold is left out");
    let (value, revision) = read.get(&name).unwrap();
    assert_eq!(value, b"{\"did\":\"did:key:a\"}");
    assert!(*revision > 0);

    // 「[Opus 5.5] Compared as free again, after it was taken: not taken, nothing written.」
    let other = whole(&[(name.as_str(), "{\"did\":\"did:key:b\"}")]);
    assert!(!keys.swap(&free, &other, &[]).unwrap());
    assert_eq!(
        keys.read(std::slice::from_ref(&name))
            .unwrap()
            .get(&name)
            .unwrap()
            .0,
        b"{\"did\":\"did:key:a\"}"
    );

    // 「[Opus 5.5] At the revision it was read at, the swap deletes as it puts.」
    let at: BTreeMap<String, i64> = [(name.clone(), *revision)].into();
    assert!(keys
        .swap(&at, &BTreeMap::new(), &[name.clone(), named.clone()])
        .unwrap());
    assert!(keys.read(&[name, named]).unwrap().is_empty());
}

#[test]
fn a_key_not_whole_is_refused() {
    let keys = EtcdKeys::open(&endpoint(), &prefix()).unwrap();
    assert!(matches!(
        keys.read(&["names/alder".to_string()]),
        Err(EtcdError::BadPrefix { .. })
    ));
}

/// 「[Opus 5.5] A node given its user before etcd authenticates anyone asks as nobody.」
#[test]
fn a_user_asks_an_etcd_that_authenticates_nobody_yet() {
    let keys = EtcdKeys::open_as(&endpoint(), &prefix(), Some(("qntx", "qntx-word"))).unwrap();
    assert!(keys.names().unwrap().is_empty());
}
