#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]
//! A node signed in to etcd as its own user keeps reading after etcd forgets
//! its token. On q, after the token went unused past etcd's --auth-token-ttl,
//! every read of /names/ was refused for an invalid token, and who holds a name
//! could not be answered (A2A ListTasks for Rowan, 2026-11-22).
//!
//! This test runs an etcd of its own, the one scripts/with-etcd.sh names in
//! QNTX_ETCD_BIN, else $ETCD, else `etcd` on PATH, with authentication on and
//! tokens forgotten after one second.

use std::net::TcpListener;
use std::process::{Child, Command, Stdio};
use std::time::{Duration, Instant};

use ats_etcd::EtcdKeys;
use etcd_client::{Client, ConnectOptions, Permission, PermissionType};

struct Etcd {
    child: Child,
    data: std::path::PathBuf,
    at: String,
}

impl Drop for Etcd {
    fn drop(&mut self) {
        if let Err(e) = self.child.kill() {
            eprintln!("the etcd of this test was not stopped: {e}");
        }
        if let Err(e) = self.child.wait() {
            eprintln!("the etcd of this test did not end: {e}");
        }
        if let Err(e) = std::fs::remove_dir_all(&self.data) {
            eprintln!("{} was not removed: {e}", self.data.display());
        }
    }
}

fn free_port() -> u16 {
    TcpListener::bind("127.0.0.1:0")
        .unwrap()
        .local_addr()
        .unwrap()
        .port()
}

fn etcd_with_short_tokens() -> Etcd {
    let binary = std::env::var("QNTX_ETCD_BIN")
        .map(|bin| format!("{bin}/etcd"))
        .or_else(|_| std::env::var("ETCD"))
        .unwrap_or_else(|_| "etcd".into());
    let (client, peer) = (free_port(), free_port());
    let data = std::env::temp_dir().join(format!("ats-etcd-stale-token-{}", std::process::id()));
    let at = format!("http://127.0.0.1:{client}");
    let child = Command::new(&binary)
        .arg("--data-dir")
        .arg(&data)
        .arg("--listen-client-urls")
        .arg(&at)
        .arg("--advertise-client-urls")
        .arg(&at)
        .arg("--listen-peer-urls")
        .arg(format!("http://127.0.0.1:{peer}"))
        .arg("--initial-advertise-peer-urls")
        .arg(format!("http://127.0.0.1:{peer}"))
        .arg("--initial-cluster")
        .arg(format!("default=http://127.0.0.1:{peer}"))
        .arg("--auth-token-ttl")
        .arg("1")
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .unwrap_or_else(|e| panic!("{binary} did not start; this test runs a real etcd: {e}"));
    Etcd { child, data, at }
}

/// Users root and qntx, qntx reaching /names/, and authentication on.
fn authenticate(at: &str) {
    let runtime = tokio::runtime::Builder::new_current_thread()
        .enable_all()
        .build()
        .unwrap();
    runtime.block_on(async {
        let deadline = Instant::now() + Duration::from_secs(10);
        let client = loop {
            let options = ConnectOptions::new().with_connect_timeout(Duration::from_secs(1));
            if let Ok(mut c) = Client::connect([at], Some(options)).await {
                if c.status().await.is_ok() {
                    break c;
                }
            }
            assert!(Instant::now() < deadline, "etcd at {at} never answered");
            tokio::time::sleep(Duration::from_millis(100)).await;
        };
        let mut auth = client.auth_client();
        auth.user_add("root", "root-word", None).await.unwrap();
        auth.user_grant_role("root", "root").await.unwrap();
        auth.user_add("qntx", "qntx-word", None).await.unwrap();
        auth.role_add("kept").await.unwrap();
        auth.role_grant_permission(
            "kept",
            Permission::new(PermissionType::Readwrite, "/names/").with_prefix(),
        )
        .await
        .unwrap();
        auth.user_grant_role("qntx", "kept").await.unwrap();
        auth.auth_enable().await.unwrap();
    });
}

#[test]
fn a_node_keeps_reading_after_etcd_forgets_its_token() {
    let etcd = etcd_with_short_tokens();
    authenticate(&etcd.at);

    let keys = EtcdKeys::open_as(&etcd.at, "/names/", Some(("qntx", "qntx-word"))).unwrap();
    assert!(keys.names().unwrap().is_empty());

    // Unused past the one second etcd keeps a token for.
    std::thread::sleep(Duration::from_secs(3));

    assert!(
        keys.names().is_ok(),
        "after etcd forgot the token, the names were not read"
    );
    assert_eq!(keys.value("alder").unwrap(), None);
}
