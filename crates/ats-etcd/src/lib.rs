//! 「[Opus 5.5] etcd window onto QNTX attestations. A namespace is a key prefix,
//! 「[Opus 5.5] `/attestations/<namespace>/`, and each attestation is one key under it,」
//! 「[Opus 5.5] held by a lease of SPAN.」」

// "「[Opus 5.5] which would be around 3 days」"

// "「[Opus 5.5] isnt the right framing to think of etcd another place where attestations can go? while QNTX holds it durably?」"

pub mod error;
pub mod keys;

#[cfg(feature = "ffi")]
pub mod ffi;

use std::time::Duration;

use ats::attestation::Attestation;
use ats::storage::{AttestationStore, StoreError};
use etcd_client::{
    Client, Compare, CompareOp, ConnectOptions, DeleteOptions, GetOptions, PutOptions, Txn, TxnOp,
};
use tokio::runtime::Runtime;

pub use crate::error::{EtcdError, Result};
pub use crate::keys::EtcdKeys;

type StoreResult<T> = std::result::Result<T, StoreError>;

/// 「[Opus 5.5] The attestation as the node hands it over the FFI: its JSON for the
/// proto, with the attributes read from that JSON as they are. Through the proto's
/// number an integer past 2^53 was rounded, and kept so with no error
/// (CodeRabbit on #1765).」
pub fn attestation_of_node_json(json: &str) -> Result<Attestation> {
    let proto: qntx_proto::Attestation = serde_json::from_str(json)?;
    let mut attestation = qntx_proto::proto_convert::from_proto(proto);
    let raw: serde_json::Value = serde_json::from_str(json)?;
    if let Some(serde_json::Value::Object(attributes)) = raw.get("attributes") {
        attestation.attributes = attributes.clone().into_iter().collect();
    }
    Ok(attestation)
}

/// 「[Opus 5.5] One etcd call, and once more when it fails: a connection cut under the
/// client, as a laptop's sleep cuts one, fails the first call after it, and the
/// client connects again for the next (l, 2026-11-19 08:03:02). Says whether it
/// was asked twice. Every call it runs is one that may be asked again.」
pub(crate) async fn once_more<T, F, Fut>(
    mut call: F,
) -> std::result::Result<(T, bool), etcd_client::Error>
where
    F: FnMut() -> Fut,
    Fut: std::future::Future<Output = std::result::Result<T, etcd_client::Error>>,
{
    match call().await {
        Ok(v) => Ok((v, false)),
        Err(_) => call().await.map(|v| (v, true)),
    }
}

/// 「[Opus 5.5] How long an attestation stays in the window.」
pub const SPAN: Duration = Duration::from_secs(72 * 60 * 60);

/// 「[Opus 5.5] How long open waits for etcd to answer.」
const OPEN_TIMEOUT: Duration = Duration::from_secs(5);

/// 「[Opus 5.5] The key prefix a namespace's attestations live under. A namespace
/// 「[Opus 5.5] is held to lowercase letters, digits, `-` and `_`, as in ats-postgres, so no」
/// 「[Opus 5.5] namespace's prefix is the start of another's.」」
pub fn prefix(namespace: &str) -> Result<String> {
    let fits = !namespace.is_empty()
        && namespace
            .bytes()
            .all(|b| b.is_ascii_lowercase() || b.is_ascii_digit() || b == b'-' || b == b'_');
    if !fits {
        return Err(EtcdError::BadNamespace {
            value: namespace.to_string(),
        });
    }
    Ok(format!("/attestations/{namespace}/"))
}

/// 「[Opus 5.5] One namespace's window in etcd. The client is async; the store
/// 「[Opus 5.5] drives it on a runtime of its own, so the trait stays synchronous.」」
pub struct EtcdStore {
    runtime: Runtime,
    client: Client,
    prefix: String,
}

fn backend(e: EtcdError) -> StoreError {
    StoreError::Backend(e.sacred_json(None))
}

fn refused(e: etcd_client::Error) -> StoreError {
    backend(EtcdError::Etcd(e))
}

fn serde(e: serde_json::Error) -> StoreError {
    backend(EtcdError::Serde(e))
}

impl EtcdStore {
    /// 「[Opus 5.5] Connect to the etcd at `endpoint`, for one namespace.」
    pub fn open(endpoint: &str, namespace: &str) -> Result<Self> {
        let prefix = prefix(namespace)?;
        let runtime = tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()
            .map_err(EtcdError::Runtime)?;
        // 「[Opus 5.5] connect does not dial: the channel is made when first asked.
        // 「[Opus 5.5] status is asked here, so an etcd nobody answers for is refused at open.」」
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
            prefix,
        })
    }

    fn key(&self, id: &str) -> String {
        format!("{}{id}", self.prefix)
    }

    /// 「[Opus 5.5] The lease an attestation's key was put under, as granted. None when
    /// 「[Opus 5.5] the window does not hold it.」」
    pub fn granted(&self, id: &str) -> StoreResult<Option<Duration>> {
        let mut client = self.client.clone();
        let key = self.key(id);
        self.runtime.block_on(async {
            let got = client.get(key, None).await.map_err(refused)?;
            let Some(kv) = got.kvs().first() else {
                return Ok(None);
            };
            if kv.lease() == 0 {
                return Ok(None);
            }
            let ttl = client
                .lease_time_to_live(kv.lease(), None)
                .await
                .map_err(refused)?;
            let secs = u64::try_from(ttl.granted_ttl())
                .map_err(|e| StoreError::InvalidData(e.to_string()))?;
            Ok(Some(Duration::from_secs(secs)))
        })
    }
}

impl AttestationStore for EtcdStore {
    fn put(&mut self, attestation: Attestation) -> StoreResult<()> {
        attestation.writable()?;
        let body = serde_json::to_vec(&attestation).map_err(serde)?;
        let key = self.key(&attestation.id);
        let client = self.client.clone();
        self.runtime.block_on(async {
            let attempt = || {
                let mut client = client.clone();
                let key = key.clone();
                let body = body.clone();
                async move {
                    let lease = client.lease_grant(SPAN.as_secs() as i64, None).await?.id();
                    let txn = Txn::new()
                        .when([Compare::create_revision(key.clone(), CompareOp::Equal, 0)])
                        .and_then([TxnOp::put(
                            key,
                            body,
                            Some(PutOptions::new().with_lease(lease)),
                        )]);
                    if client.txn(txn).await?.succeeded() {
                        return Ok(true);
                    }
                    // 「[Opus 5.5] The key was there: the lease granted for it holds nothing.」
                    client.lease_revoke(lease).await?;
                    Ok(false)
                }
            };
            let (landed, retried) = once_more(attempt).await.map_err(refused)?;
            if landed {
                return Ok(());
            }
            // 「[Opus 5.5] Asked again, the key that is there may be the first ask's, whose
            // answer the cut connection lost: holding this very attestation, it landed.」
            if retried {
                let held = client
                    .clone()
                    .get(key.clone(), None)
                    .await
                    .map_err(refused)?;
                if held.kvs().first().map(|kv| kv.value()) == Some(body.as_slice()) {
                    return Ok(());
                }
            }
            Err(StoreError::AlreadyExists(attestation.id.clone()))
        })
    }

    fn get(&self, id: &str) -> StoreResult<Option<Attestation>> {
        let client = self.client.clone();
        let key = self.key(id);
        self.runtime.block_on(async {
            let attempt = || {
                let mut client = client.clone();
                let key = key.clone();
                async move { client.get(key, None).await }
            };
            let (got, _) = once_more(attempt).await.map_err(refused)?;
            match got.kvs().first() {
                Some(kv) => serde_json::from_slice(kv.value()).map(Some).map_err(serde),
                None => Ok(None),
            }
        })
    }

    fn delete(&mut self, id: &str) -> StoreResult<bool> {
        let mut client = self.client.clone();
        let key = self.key(id);
        self.runtime.block_on(async {
            let gone = client.delete(key, None).await.map_err(refused)?;
            Ok(gone.deleted() == 1)
        })
    }

    fn update(&mut self, attestation: Attestation) -> StoreResult<()> {
        attestation.writable()?;
        let body = serde_json::to_vec(&attestation).map_err(serde)?;
        let key = self.key(&attestation.id);
        let mut client = self.client.clone();
        self.runtime.block_on(async {
            // 「[Opus 5.5] An update keeps the key's lease: the window is counted from the put.」
            let txn = Txn::new()
                .when([Compare::create_revision(key.clone(), CompareOp::Greater, 0)])
                .and_then([TxnOp::put(
                    key,
                    body,
                    Some(PutOptions::new().with_ignore_lease()),
                )]);
            if client.txn(txn).await.map_err(refused)?.succeeded() {
                return Ok(());
            }
            Err(StoreError::NotFound(attestation.id.clone()))
        })
    }

    fn ids(&self) -> StoreResult<Vec<String>> {
        let mut client = self.client.clone();
        let prefix = self.prefix.clone();
        self.runtime.block_on(async {
            let got = client
                .get(
                    prefix.clone(),
                    Some(GetOptions::new().with_prefix().with_keys_only()),
                )
                .await
                .map_err(refused)?;
            got.kvs()
                .iter()
                .map(|kv| {
                    let key = kv
                        .key_str()
                        .map_err(|e| StoreError::InvalidData(e.to_string()))?;
                    key.strip_prefix(&prefix)
                        .map(str::to_string)
                        .ok_or_else(|| StoreError::InvalidData(key.to_string()))
                })
                .collect()
        })
    }

    fn count(&self) -> StoreResult<usize> {
        let mut client = self.client.clone();
        let prefix = self.prefix.clone();
        self.runtime.block_on(async {
            let got = client
                .get(
                    prefix,
                    Some(GetOptions::new().with_prefix().with_count_only()),
                )
                .await
                .map_err(refused)?;
            usize::try_from(got.count()).map_err(|e| StoreError::InvalidData(e.to_string()))
        })
    }

    fn clear(&mut self) -> StoreResult<()> {
        let mut client = self.client.clone();
        let prefix = self.prefix.clone();
        self.runtime.block_on(async {
            client
                .delete(prefix, Some(DeleteOptions::new().with_prefix()))
                .await
                .map_err(refused)?;
            Ok(())
        })
    }
}
