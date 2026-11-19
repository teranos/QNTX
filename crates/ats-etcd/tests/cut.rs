#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]
//! 「[Opus 5.5] A connection cut under the client, as a laptop's sleep cuts one: the
//! first put after it broke its pipe and was lost to the window (l, 2026-11-19
//! 08:03:02, a BrokenPipe transport error). The client reaches etcd through a
//! proxy here that cuts every connection it carries on cue.」

use std::collections::HashMap;
use std::io::copy;
use std::net::{Shutdown, TcpListener, TcpStream};
use std::sync::{Arc, Mutex};
use std::thread;

use ats::attestation::Attestation;
use ats::storage::AttestationStore;
use ats_etcd::EtcdStore;

fn endpoint() -> String {
    std::env::var("QNTX_ETCD_ENDPOINT")
        .expect("QNTX_ETCD_ENDPOINT names no etcd; run under scripts/with-etcd.sh")
}

/// 「[Opus 5.5] A proxy to etcd on a port of its own; cut shuts every connection it carries.」
struct Proxy {
    port: u16,
    live: Arc<Mutex<Vec<TcpStream>>>,
}

impl Proxy {
    fn to(target: &str) -> Proxy {
        let target = target.trim_start_matches("http://").to_string();
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let port = listener.local_addr().unwrap().port();
        let live: Arc<Mutex<Vec<TcpStream>>> = Arc::default();
        let held = live.clone();
        thread::spawn(move || {
            for client in listener.incoming() {
                let Ok(client) = client else { continue };
                let Ok(server) = TcpStream::connect(&target) else {
                    continue;
                };
                held.lock().unwrap().push(client.try_clone().unwrap());
                held.lock().unwrap().push(server.try_clone().unwrap());
                let (mut c1, mut s1) = (client.try_clone().unwrap(), server.try_clone().unwrap());
                let (mut c2, mut s2) = (client, server);
                thread::spawn(move || {
                    copy(&mut c1, &mut s1).ok();
                });
                thread::spawn(move || {
                    copy(&mut s2, &mut c2).ok();
                });
            }
        });
        Proxy { port, live }
    }

    fn endpoint(&self) -> String {
        format!("http://127.0.0.1:{}", self.port)
    }

    fn cut(&self) {
        for s in self.live.lock().unwrap().drain(..) {
            s.shutdown(Shutdown::Both).ok();
        }
    }
}

fn attestation(id: &str) -> Attestation {
    Attestation {
        id: id.to_string(),
        subjects: vec!["l".to_string()],
        predicates: vec!["slept".to_string()],
        contexts: vec!["etcd".to_string()],
        actors: vec!["human:bob".to_string()],
        timestamp: 1000,
        source: "test".to_string(),
        attributes: HashMap::new(),
        created_at: 1000,
        signature: None,
        signer_did: None,
    }
}

#[test]
fn a_put_after_the_connection_was_cut_lands() {
    let proxy = Proxy::to(&endpoint());
    let ns = format!("cut{}", std::process::id());
    let mut store = EtcdStore::open(&proxy.endpoint(), &ns).unwrap();
    store.put(attestation("AS-before")).unwrap();
    proxy.cut();
    store.put(attestation("AS-after")).unwrap();
    assert!(store.get("AS-after").unwrap().is_some());
}
