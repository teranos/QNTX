//! 「[Opus 5.5] Keys under one prefix in etcd, set whole: what the node holds as a
//! 「[Opus 5.5] map is kept there key by key, so a reader watches the prefix and has it」
//! 「[Opus 5.5] as the node does. No lease: a key stands until the node says it is gone.」」

// "「[Opus 5.5] our gap is kv」"

use std::collections::BTreeMap;
use std::sync::Mutex;

use etcd_client::{Client, Compare, CompareOp, ConnectOptions, GetOptions, Txn, TxnOp};
use tokio::runtime::Runtime;

use crate::error::{EtcdError, Result};
use crate::once_more;

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
    /// The client, and the token it signed in with, held until etcd forgets the
    /// token and it is signed in again as `user`: q stopped reading who holds a
    /// name when its token went unused past etcd's --auth-token-ttl.
    client: Mutex<Client>,
    endpoint: String,
    user: Option<(String, String)>,
    prefix: String,
}

/// A client of the etcd at `endpoint`, signed in as `user` where etcd
/// authenticates anyone, and as nobody where it does not yet.
fn connect(runtime: &Runtime, endpoint: &str, user: Option<(&str, &str)>) -> Result<Client> {
    let options = || {
        ConnectOptions::new()
            .with_connect_timeout(OPEN_TIMEOUT)
            .with_timeout(OPEN_TIMEOUT)
    };
    runtime
        .block_on(async {
            let asked = match user {
                Some((name, password)) => {
                    match Client::connect([endpoint], Some(options().with_user(name, password)))
                        .await
                    {
                        Err(e) if e.to_string().contains("authentication is not enabled") => {
                            Client::connect([endpoint], Some(options())).await
                        }
                        connected => connected,
                    }
                }
                None => Client::connect([endpoint], Some(options())).await,
            };
            let mut client = asked?;
            client.status().await?;
            Ok::<Client, etcd_client::Error>(client)
        })
        .map_err(|source| EtcdError::Connect {
            endpoint: endpoint.to_string(),
            source,
        })
}

impl EtcdKeys {
    /// 「[Opus 5.5] Connect to the etcd at `endpoint`, for the keys under `prefix`.」
    pub fn open(endpoint: &str, prefix: &str) -> Result<Self> {
        Self::open_as(endpoint, prefix, None)
    }

    // "names and collections and just smaller things, messages say's status"
    //
    /// 「[Opus 5.5] Connect as `user` and its password, whose role reaches only the keys」
    /// 「[Opus 5.5] the node keeps. An etcd that authenticates nobody yet is asked as nobody,」
    /// 「[Opus 5.5] so each node is given its user before authentication is enabled.」
    pub fn open_as(endpoint: &str, prefix: &str, user: Option<(&str, &str)>) -> Result<Self> {
        if !fits(prefix) {
            return Err(EtcdError::BadPrefix {
                value: prefix.to_string(),
            });
        }
        let runtime = tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()
            .map_err(EtcdError::Runtime)?;
        let client = connect(&runtime, endpoint, user)?;
        Ok(Self {
            runtime,
            client: Mutex::new(client),
            endpoint: endpoint.to_string(),
            user: user.map(|(name, password)| (name.to_string(), password.to_string())),
            prefix: prefix.to_string(),
        })
    }

    /// The client as it is now.
    fn client(&self) -> Client {
        self.client
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
            .clone()
    }

    /// What `once` answers, asked again once signed in afresh where etcd forgot
    /// the token it was asked with. A request etcd refuses for its token is
    /// refused before anything in it is done, so asking it again does it once.
    fn asked<T>(&self, once: impl Fn() -> Result<T>) -> Result<T> {
        match once() {
            Err(e) if e.stale_token() => {
                let user = self
                    .user
                    .as_ref()
                    .map(|(name, password)| (name.as_str(), password.as_str()));
                let fresh = connect(&self.runtime, &self.endpoint, user)?;
                *self
                    .client
                    .lock()
                    .unwrap_or_else(std::sync::PoisonError::into_inner) = fresh;
                once()
            }
            answered => answered,
        }
    }

    /// 「[Opus 5.5] The names under the prefix, read without their values and a page at
    /// 「[Opus 5.5] a time: one answer holding every value passed gRPC's 4 MiB, and then」
    /// 「[Opus 5.5] nothing under the prefix could be set or deleted (CodeRabbit on #1768).」」
    pub fn names(&self) -> Result<Vec<String>> {
        self.asked(|| self.names_once())
    }

    fn names_once(&self) -> Result<Vec<String>> {
        let client = self.client();
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
                let asked = once_more(|| {
                    let mut client = client.clone();
                    let (from, end) = (from.clone(), end.clone());
                    async move {
                        client
                            .get(
                                from,
                                Some(
                                    GetOptions::new()
                                        .with_range(end)
                                        .with_keys_only()
                                        .with_limit(page),
                                ),
                            )
                            .await
                    }
                })
                .await
                .map(|(got, _)| got);
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
        let key = format!("{}{name}", self.prefix);
        self.asked(|| {
            let client = self.client();
            self.runtime.block_on(async {
                let (got, _) = once_more(|| {
                    let mut client = client.clone();
                    let key = key.clone();
                    async move { client.get(key, None).await }
                })
                .await?;
                Ok(got.kvs().first().map(|kv| kv.value().to_vec()))
            })
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
        let key = format!("{}{name}", self.prefix);
        self.asked(|| {
            let mut client = self.client();
            let key = key.clone();
            self.runtime.block_on(async {
                let got = client.get(key, None).await?;
                Ok(got.kvs().first().map(|kv| kv.mod_revision()))
            })
        })
    }

    /// 「[Opus 5.5] The prefix holds `whole` from here on: a key whose value changed or
    /// 「[Opus 5.5] that is new is put, one that is not in it is deleted, and one that holds」
    /// 「[Opus 5.5] the same is left, so a watcher is told only what changed. Each is its own」
    /// 「[Opus 5.5] request: etcd refuses a request over its --max-request-bytes, and a key's」
    /// 「[Opus 5.5] value alone is held under that by its writer.」」
    pub fn set(&mut self, whole: &BTreeMap<String, Vec<u8>>) -> Result<()> {
        let names = self.names()?;
        let prefix = self.prefix.clone();
        // 「[Opus 5.5] A put and a delete of a key are each asked once more when they fail:
        // asked twice, each leaves the key as it was asked to.」
        for (name, value) in whole {
            if names.contains(name) && self.value(name)?.as_ref() == Some(value) {
                continue;
            }
            let key = format!("{prefix}{name}");
            self.asked(|| {
                let client = self.client();
                self.runtime
                    .block_on(once_more(|| {
                        let mut client = client.clone();
                        let (key, value) = (key.clone(), value.clone());
                        async move { client.put(key, value, None).await }
                    }))
                    .map(|_| ())
                    .map_err(EtcdError::from)
            })?;
        }
        for name in &names {
            if !whole.contains_key(name) {
                let key = format!("{prefix}{name}");
                self.asked(|| {
                    let client = self.client();
                    self.runtime
                        .block_on(once_more(|| {
                            let mut client = client.clone();
                            let key = key.clone();
                            async move { client.delete(key, None).await }
                        }))
                        .map(|_| ())
                        .map_err(EtcdError::from)
                })?;
            }
        }
        Ok(())
    }

    // "LAYE OWNS IDENTITY"
    //
    // 「[Opus 5.5] read and swap take whole keys, each starting at `/`, and not names under
    // 「[Opus 5.5] the prefix: a name is taken under /names/ and /named/ at once, in one」
    // 「[Opus 5.5] transaction, as laye takes it (laye-front names.rs).」」

    /// 「[Opus 5.5] Each key's value and the revision it was last put at; a key etcd does
    /// 「[Opus 5.5] not hold is left out.」」
    pub fn read(&self, keys: &[String]) -> Result<BTreeMap<String, (Vec<u8>, i64)>> {
        for key in keys {
            whole(key)?;
        }
        self.asked(|| {
            let client = self.client();
            self.runtime.block_on(async {
                let mut read = BTreeMap::new();
                for key in keys {
                    let (got, _) = once_more(|| {
                        let mut client = client.clone();
                        let key = key.clone();
                        async move { client.get(key, None).await }
                    })
                    .await?;
                    if let Some(kv) = got.kvs().first() {
                        read.insert(key.clone(), (kv.value().to_vec(), kv.mod_revision()));
                    }
                }
                Ok(read)
            })
        })
    }

    /// 「[Opus 5.5] Puts `put` and deletes `delete` only while each key in `unchanged` is at
    /// 「[Opus 5.5] the revision named, 0 for a key etcd does not hold. False when one moved:」
    /// 「[Opus 5.5] another wrote between the reading and this, and nothing was written. Asked」
    /// 「[Opus 5.5] once: a second asking of one that was taken would be said as not taken.」」
    pub fn swap(
        &self,
        unchanged: &BTreeMap<String, i64>,
        put: &BTreeMap<String, Vec<u8>>,
        delete: &[String],
    ) -> Result<bool> {
        for key in unchanged.keys().chain(put.keys()).chain(delete) {
            whole(key)?;
        }
        // A swap etcd refused for its token did nothing, and is asked again
        // signed in afresh.
        self.asked(|| {
            let compares: Vec<Compare> = unchanged
                .iter()
                .map(|(key, revision)| {
                    Compare::mod_revision(key.as_str(), CompareOp::Equal, *revision)
                })
                .collect();
            let ops: Vec<TxnOp> = put
                .iter()
                .map(|(key, value)| TxnOp::put(key.as_str(), value.clone(), None))
                .chain(delete.iter().map(|key| TxnOp::delete(key.as_str(), None)))
                .collect();
            let mut client = self.client();
            self.runtime.block_on(async {
                let done = client.txn(Txn::new().when(compares).and_then(ops)).await?;
                Ok(done.succeeded())
            })
        })
    }
}

/// 「[Opus 5.5] A whole key starts at `/`.」
fn whole(key: &str) -> Result<()> {
    if key.len() > 1 && key.starts_with('/') {
        Ok(())
    } else {
        Err(EtcdError::BadPrefix {
            value: key.to_string(),
        })
    }
}
