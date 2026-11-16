//! 「[Opus 5.5] Keys under one prefix in etcd, set whole: what the node holds as a
//! 「[Opus 5.5] map is kept there key by key, so a reader watches the prefix and has it」
//! 「[Opus 5.5] as the node does. No lease: a key stands until the node says it is gone.」」

// "「[Opus 5.5] our gap is kv」"

use std::collections::BTreeMap;

use etcd_client::{Client, ConnectOptions, GetOptions};
use tokio::runtime::Runtime;

use crate::error::{EtcdError, Result};

/// 「[Opus 5.5] How long open waits for etcd to answer, as the window does.」
const OPEN_TIMEOUT: std::time::Duration = std::time::Duration::from_secs(5);

/// 「[Opus 5.5] A prefix starts and ends at `/`, so no prefix is the start of another
/// 「[Opus 5.5] key's name that is not under it.」」
fn fits(prefix: &str) -> bool {
    prefix.len() > 2 && prefix.starts_with('/') && prefix.ends_with('/')
}

pub struct EtcdKeys {
    runtime: Runtime,
    client: Client,
    prefix: String,
}

impl EtcdKeys {
    /// 「[Opus 5.5] Connect to the etcd at `endpoint`, for the keys under `prefix`.」
    pub fn open(endpoint: &str, prefix: &str) -> Result<Self> {
        if !fits(prefix) {
            return Err(EtcdError::BadPrefix {
                value: prefix.to_string(),
            });
        }
        let runtime = tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()
            .map_err(EtcdError::Runtime)?;
        let options = ConnectOptions::new()
            .with_connect_timeout(OPEN_TIMEOUT)
            .with_timeout(OPEN_TIMEOUT);
        let client = runtime
            .block_on(async {
                let mut client = Client::connect([endpoint], Some(options)).await?;
                client.status().await?;
                Ok::<Client, etcd_client::Error>(client)
            })
            .map_err(|source| EtcdError::Connect {
                endpoint: endpoint.to_string(),
                source,
            })?;
        Ok(Self {
            runtime,
            client,
            prefix: prefix.to_string(),
        })
    }

    /// 「[Opus 5.5] What the prefix holds, each key by its name under the prefix.」
    pub fn held(&self) -> Result<BTreeMap<String, Vec<u8>>> {
        let mut client = self.client.clone();
        let prefix = self.prefix.clone();
        self.runtime.block_on(async {
            let got = client
                .get(prefix.clone(), Some(GetOptions::new().with_prefix()))
                .await?;
            let mut held = BTreeMap::new();
            for kv in got.kvs() {
                let Some(name) = kv.key().strip_prefix(prefix.as_bytes()) else {
                    continue;
                };
                held.insert(
                    String::from_utf8_lossy(name).into_owned(),
                    kv.value().to_vec(),
                );
            }
            Ok(held)
        })
    }

    /// 「[Opus 5.5] The revision a key was last put at; None when the prefix does not hold it.」
    pub fn revision_of(&self, name: &str) -> Result<Option<i64>> {
        let mut client = self.client.clone();
        let key = format!("{}{name}", self.prefix);
        self.runtime.block_on(async {
            let got = client.get(key, None).await?;
            Ok(got.kvs().first().map(|kv| kv.mod_revision()))
        })
    }

    /// 「[Opus 5.5] The prefix holds `whole` from here on: a key whose value changed or
    /// 「[Opus 5.5] that is new is put, one that is not in it is deleted, and one that holds」
    /// 「[Opus 5.5] the same is left, so a watcher is told only what changed. Each is its own」
    /// 「[Opus 5.5] request: etcd refuses a request over its --max-request-bytes, and a key's」
    /// 「[Opus 5.5] value alone is held under that by its writer.」」
    pub fn set(&mut self, whole: &BTreeMap<String, Vec<u8>>) -> Result<()> {
        let held = self.held()?;
        let mut client = self.client.clone();
        let prefix = self.prefix.clone();
        self.runtime.block_on(async {
            for (name, value) in whole {
                if held.get(name) == Some(value) {
                    continue;
                }
                client
                    .put(format!("{prefix}{name}"), value.clone(), None)
                    .await?;
            }
            for name in held.keys() {
                if !whole.contains_key(name) {
                    client.delete(format!("{prefix}{name}"), None).await?;
                }
            }
            Ok(())
        })
    }
}
