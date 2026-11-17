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

/// 「[Opus 5.5] How many names one page of names holds: names alone, far under 4 MiB.」
const NAMES_PAGE: i64 = 1000;

/// 「[Opus 5.5] gRPC's OUT_OF_RANGE, what an answer past the decoding limit is said as.
/// etcd-client's tonic is not the workspace's, so the code is named by its number.」
const GRPC_OUT_OF_RANGE: i32 = 11;

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

    /// 「[Opus 5.5] The names under the prefix, read without their values and a page at
    /// 「[Opus 5.5] a time: one answer holding every value passed gRPC's 4 MiB, and then」
    /// 「[Opus 5.5] nothing under the prefix could be set or deleted (CodeRabbit on #1768).」」
    pub fn names(&self) -> Result<Vec<String>> {
        let mut client = self.client.clone();
        let prefix = self.prefix.clone();
        let mut end = prefix.clone().into_bytes();
        if let Some(last) = end.last_mut() {
            *last += 1;
        }
        self.runtime.block_on(async {
            let mut names = Vec::new();
            let mut from = prefix.clone().into_bytes();
            // 「[Opus 5.5] A page is a count, and names can be long: an answer past what
            // gRPC decodes is asked again with half the page, down to one name, which
            // always fits under etcd's own request limit (CodeRabbit on #1768).」
            let mut page = NAMES_PAGE;
            loop {
                let asked = client
                    .get(
                        from.clone(),
                        Some(
                            GetOptions::new()
                                .with_range(end.clone())
                                .with_keys_only()
                                .with_limit(page),
                        ),
                    )
                    .await;
                let got = match asked {
                    Err(etcd_client::Error::GRpcStatus(status))
                        if status.code() as i32 == GRPC_OUT_OF_RANGE && page > 1 =>
                    {
                        page /= 2;
                        continue;
                    }
                    other => other?,
                };
                for kv in got.kvs() {
                    if let Some(name) = kv.key().strip_prefix(prefix.as_bytes()) {
                        names.push(String::from_utf8_lossy(name).into_owned());
                    }
                }
                let Some(last) = got.kvs().last() else { break };
                if !got.more() {
                    break;
                }
                from = last.key().to_vec();
                from.push(0);
            }
            Ok(names)
        })
    }

    /// 「[Opus 5.5] One name's value; None when the prefix does not hold it.」
    pub fn value(&self, name: &str) -> Result<Option<Vec<u8>>> {
        let mut client = self.client.clone();
        let key = format!("{}{name}", self.prefix);
        self.runtime.block_on(async {
            let got = client.get(key, None).await?;
            Ok(got.kvs().first().map(|kv| kv.value().to_vec()))
        })
    }

    /// 「[Opus 5.5] What the prefix holds, each key by its name under the prefix, read a
    /// 「[Opus 5.5] value at a time.」」
    pub fn held(&self) -> Result<BTreeMap<String, Vec<u8>>> {
        let mut held = BTreeMap::new();
        for name in self.names()? {
            if let Some(value) = self.value(&name)? {
                held.insert(name, value);
            }
        }
        Ok(held)
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
        let names = self.names()?;
        let mut client = self.client.clone();
        let prefix = self.prefix.clone();
        for (name, value) in whole {
            if names.contains(name) && self.value(name)?.as_ref() == Some(value) {
                continue;
            }
            self.runtime
                .block_on(client.put(format!("{prefix}{name}"), value.clone(), None))?;
        }
        for name in &names {
            if !whole.contains_key(name) {
                self.runtime
                    .block_on(client.delete(format!("{prefix}{name}"), None))?;
            }
        }
        Ok(())
    }
}
