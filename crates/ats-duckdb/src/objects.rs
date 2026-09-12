//! One object, one request. Tokens, Users, namespace definitions and the node
//! identity are single small records, and writing one is a PUT. Reading one
//! is a GET. Through DuckDB the request went out and the answer came back as a
//! status code and a phrase, while the body S3 sent — the reason — sat one
//! field away and never crossed. Here the whole answer rides on the error.

// "WE DONT DROP IT" / "WE DONT TRUNCATE" / "WE DONT HIDE ERRORS"
// "WE DONT MAKE UP REASONS"

use std::path::{Path, PathBuf};

use aws_sdk_s3::error::SdkError;
use aws_sdk_s3::operation::delete_object::DeleteObjectError;
use aws_sdk_s3::operation::get_object::GetObjectError;
use aws_sdk_s3::operation::list_objects_v2::ListObjectsV2Error;
use aws_sdk_s3::operation::put_object::PutObjectError;
use aws_sdk_s3::primitives::ByteStream;
use aws_smithy_runtime_api::client::orchestrator::HttpResponse;

use crate::error::{DuckdbError, Name, Object, Refusal, Result};

/// Where objects live: on this filesystem, or in one S3 bucket.
pub(crate) enum Objects {
    Local,
    S3(Bucket),
}

/// One bucket, reached through one client on one runtime. The client resolves
/// credentials itself, the same provider chain DuckDB was handed, and refreshes
/// them before they expire, so nothing here resolves them again on failure.
pub(crate) struct Bucket {
    runtime: tokio::runtime::Runtime,
    client: aws_sdk_s3::Client,
    name: String,
}

/// What S3 answered, or what stopped the request reaching it. The SDK's own
/// value, generic over the operation, so nothing is rendered on the way in.
#[derive(Debug)]
pub enum S3Failure {
    Put(SdkError<PutObjectError, HttpResponse>),
    Get(SdkError<GetObjectError, HttpResponse>),
    List(SdkError<ListObjectsV2Error, HttpResponse>),
    Delete(SdkError<DeleteObjectError, HttpResponse>),
    /// The object was found and its bytes did not all arrive.
    Body(aws_smithy_types::byte_stream::error::Error),
}

impl S3Failure {
    /// The response, when S3 answered at all.
    fn response(&self) -> Option<&HttpResponse> {
        match self {
            S3Failure::Put(e) => e.raw_response(),
            S3Failure::Get(e) => e.raw_response(),
            S3Failure::List(e) => e.raw_response(),
            S3Failure::Delete(e) => e.raw_response(),
            S3Failure::Body(_) => None,
        }
    }

    /// What S3 said, in S3's words: the status line and the body it sent.
    /// When nothing answered, the SDK's account of why the request never
    /// completed, source by source.
    pub(crate) fn said(&self) -> String {
        if let Some(response) = self.response() {
            let status = response.status();
            let body = response
                .body()
                .bytes()
                .map(|b| String::from_utf8_lossy(b).into_owned())
                .unwrap_or_default();
            return format!("HTTP {status}\n{body}");
        }
        self.chain()
    }

    /// Every source in the SDK's error, outermost first.
    pub(crate) fn chain(&self) -> String {
        use aws_smithy_types::error::display::DisplayErrorContext as Whole;
        match self {
            S3Failure::Put(e) => Whole(e).to_string(),
            S3Failure::Get(e) => Whole(e).to_string(),
            S3Failure::List(e) => Whole(e).to_string(),
            S3Failure::Delete(e) => Whole(e).to_string(),
            S3Failure::Body(e) => Whole(e).to_string(),
        }
    }

    /// The request ids S3 stamped on the answer, for AWS's side of the trace.
    pub(crate) fn request_ids(&self) -> Vec<String> {
        let Some(response) = self.response() else {
            return Vec::new();
        };
        ["x-amz-request-id", "x-amz-id-2"]
            .iter()
            .filter_map(|name| {
                response
                    .headers()
                    .get(*name)
                    .map(|v| format!("{name}: {v}"))
            })
            .collect()
    }
}

/// The host's credential is a file the SSM agent rewrites as the role's token
/// rotates. The SDK's profile provider parses that file once and keeps it for
/// the life of the process (aws-config, profile/credentials.rs: "Parsed file
/// contents will be cached indefinitely"), so the node presented a token past
/// its expiry and S3 answered ExpiredToken. This resolves the whole default
/// chain again every time the SDK's cache asks, and says the answer is good
/// for ten minutes, so the cache asks again.
#[derive(Debug)]
struct Rotating {
    http: aws_smithy_runtime_api::client::http::SharedHttpClient,
}

/// How long one resolution stands before the file is read again. Shorter than
/// any rotation the agent does, so a token is never presented after the file
/// stopped saying it.
const RESOLVED_FOR: std::time::Duration = std::time::Duration::from_secs(600);

impl aws_credential_types::provider::ProvideCredentials for Rotating {
    fn provide_credentials<'a>(
        &'a self,
    ) -> aws_credential_types::provider::future::ProvideCredentials<'a>
    where
        Self: 'a,
    {
        aws_credential_types::provider::future::ProvideCredentials::new(async {
            let config = aws_config::provider_config::ProviderConfig::default()
                .with_http_client(self.http.clone())
                .load_default_region()
                .await;
            let chain =
                aws_config::default_provider::credentials::DefaultCredentialsChain::builder()
                    .configure(config)
                    .build()
                    .await;
            let found = chain.provide_credentials().await?;
            let until = std::time::SystemTime::now() + RESOLVED_FOR;
            // A credential that already says when it ends keeps the earlier
            // of the two: nothing here extends what the issuer said.
            let expiry = match found.expiry() {
                Some(theirs) if theirs < until => theirs,
                _ => until,
            };
            Ok(aws_credential_types::Credentials::new(
                found.access_key_id(),
                found.secret_access_key(),
                found.session_token().map(str::to_string),
                Some(expiry),
                "rotating",
            ))
        })
    }
}

/// Which request it was.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Request {
    Put,
    Get,
    List,
    Delete,
}

impl std::fmt::Display for Request {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(match self {
            Request::Put => "PUT",
            Request::Get => "GET",
            Request::List => "LIST",
            Request::Delete => "DELETE",
        })
    }
}

impl Objects {
    /// Reach the location. `s3://bucket/...` is a bucket; anything else is a
    /// path on this filesystem, with or without `file://` in front.
    pub(crate) fn open(location: &str) -> Result<Self> {
        let Some(rest) = location.strip_prefix("s3://") else {
            return Ok(Objects::Local);
        };
        let name = rest.split('/').next().unwrap_or_default().to_string();
        if name.is_empty() {
            return Err(DuckdbError::BadName {
                which: Name::Location,
                value: location.to_string(),
                why: Refusal::NoBucket,
            });
        }

        let runtime = tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()
            .map_err(DuckdbError::Runtime)?;
        let client = runtime.block_on(async {
            let http = aws_smithy_http_client::Builder::new()
                .tls_provider(aws_smithy_http_client::tls::Provider::Rustls(
                    aws_smithy_http_client::tls::rustls_provider::CryptoMode::Ring,
                ))
                .build_https();
            let config = aws_config::defaults(aws_config::BehaviorVersion::latest())
                .http_client(http.clone())
                .credentials_provider(Rotating { http })
                .load()
                .await;
            aws_sdk_s3::Client::new(&config)
        });
        Ok(Objects::S3(Bucket {
            runtime,
            client,
            name,
        }))
    }

    /// Write the object at `path`, replacing what was there. The parent is
    /// made on a filesystem; a bucket has no directories.
    pub(crate) fn put(&self, what: Object, path: &str, body: Vec<u8>) -> Result<()> {
        match self {
            Objects::Local => {
                let file = local(path);
                if let Some(parent) = file.parent() {
                    std::fs::create_dir_all(parent).map_err(|source| DuckdbError::WriteFile {
                        what: what.clone(),
                        path: path.to_string(),
                        source,
                    })?;
                }
                std::fs::write(&file, body).map_err(|source| DuckdbError::WriteFile {
                    what,
                    path: path.to_string(),
                    source,
                })
            }
            Objects::S3(bucket) => {
                let key = bucket.key(path)?;
                let sent = bucket.runtime.block_on(
                    bucket
                        .client
                        .put_object()
                        .bucket(&bucket.name)
                        .key(&key)
                        .body(ByteStream::from(body))
                        .send(),
                );
                sent.map(|_| ()).map_err(|e| DuckdbError::S3 {
                    request: Request::Put,
                    what,
                    path: path.to_string(),
                    source: Box::new(S3Failure::Put(e)),
                })
            }
        }
    }

    /// Read the object at `path`. `None` is the object not being there, which
    /// is an answer and not a failure: a store that has never written one.
    pub(crate) fn get(&self, what: Object, path: &str) -> Result<Option<Vec<u8>>> {
        match self {
            Objects::Local => match std::fs::read(local(path)) {
                Ok(bytes) => Ok(Some(bytes)),
                Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(None),
                Err(source) => Err(DuckdbError::ReadFile {
                    what,
                    path: path.to_string(),
                    source,
                }),
            },
            Objects::S3(bucket) => {
                let key = bucket.key(path)?;
                let got = bucket.runtime.block_on(
                    bucket
                        .client
                        .get_object()
                        .bucket(&bucket.name)
                        .key(&key)
                        .send(),
                );
                let output = match got {
                    Ok(output) => output,
                    Err(e) => {
                        if let SdkError::ServiceError(service) = &e {
                            if matches!(service.err(), GetObjectError::NoSuchKey(_)) {
                                return Ok(None);
                            }
                        }
                        return Err(DuckdbError::S3 {
                            request: Request::Get,
                            what,
                            path: path.to_string(),
                            source: Box::new(S3Failure::Get(e)),
                        });
                    }
                };
                let bytes = bucket
                    .runtime
                    .block_on(output.body.collect())
                    .map_err(|e| DuckdbError::S3 {
                        request: Request::Get,
                        what,
                        path: path.to_string(),
                        source: Box::new(S3Failure::Body(e)),
                    })?;
                Ok(Some(bytes.into_bytes().to_vec()))
            }
        }
    }

    /// Every object under `prefix`, however deep, as full paths in the same
    /// form `prefix` was given. A prefix holding nothing is an empty list; a
    /// prefix that cannot be listed is an error, and the two are not confused.
    pub(crate) fn list(&self, what: Object, prefix: &str) -> Result<Vec<String>> {
        match self {
            Objects::Local => {
                let root = local(prefix);
                let mut found = Vec::new();
                match walk(&root, &mut found) {
                    Ok(()) => {}
                    Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(Vec::new()),
                    Err(source) => {
                        return Err(DuckdbError::ReadFile {
                            what,
                            path: prefix.to_string(),
                            source,
                        })
                    }
                }
                let root_text = root.to_string_lossy().into_owned();
                Ok(found
                    .into_iter()
                    .map(|p| {
                        let full = p.to_string_lossy().into_owned();
                        let rel = full.strip_prefix(&root_text).unwrap_or(&full);
                        format!(
                            "{}/{}",
                            prefix.trim_end_matches('/'),
                            rel.trim_start_matches('/')
                        )
                    })
                    .collect())
            }
            Objects::S3(bucket) => {
                let key = bucket.key(prefix)?;
                let key = format!("{}/", key.trim_end_matches('/'));
                let mut found = Vec::new();
                let mut pages = bucket
                    .client
                    .list_objects_v2()
                    .bucket(&bucket.name)
                    .prefix(&key)
                    .into_paginator()
                    .send();
                loop {
                    let page = bucket.runtime.block_on(pages.next());
                    let Some(page) = page else { break };
                    let page = page.map_err(|e| DuckdbError::S3 {
                        request: Request::List,
                        what: what.clone(),
                        path: prefix.to_string(),
                        source: Box::new(S3Failure::List(e)),
                    })?;
                    for object in page.contents() {
                        if let Some(k) = object.key() {
                            found.push(format!("s3://{}/{k}", bucket.name));
                        }
                    }
                }
                Ok(found)
            }
        }
    }

    /// Remove the object at `path`. An absent object is the state this asks
    /// for, so it answers ok; compaction finishes an interrupted run by
    /// deleting the same sources a second time.
    pub(crate) fn delete(&self, what: Object, path: &str) -> Result<()> {
        match self {
            Objects::Local => match std::fs::remove_file(local(path)) {
                Ok(()) => Ok(()),
                Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(()),
                Err(source) => Err(DuckdbError::WriteFile {
                    what,
                    path: path.to_string(),
                    source,
                }),
            },
            Objects::S3(bucket) => {
                let key = bucket.key(path)?;
                let sent = bucket.runtime.block_on(
                    bucket
                        .client
                        .delete_object()
                        .bucket(&bucket.name)
                        .key(&key)
                        .send(),
                );
                sent.map(|_| ()).map_err(|e| DuckdbError::S3 {
                    request: Request::Delete,
                    what,
                    path: path.to_string(),
                    source: Box::new(S3Failure::Delete(e)),
                })
            }
        }
    }
}

impl Bucket {
    /// The key a path names inside this bucket. A path outside it is refused:
    /// this client reaches one bucket, and a wrong path is a wrong path.
    fn key(&self, path: &str) -> Result<String> {
        let inside = format!("s3://{}/", self.name);
        match path.strip_prefix(&inside) {
            Some(key) if !key.is_empty() => Ok(key.to_string()),
            _ => Err(DuckdbError::BadName {
                which: Name::Location,
                value: path.to_string(),
                why: Refusal::OutsideTheBucket,
            }),
        }
    }
}

/// The filesystem path a location names, with `file://` taken off.
fn local(path: &str) -> PathBuf {
    PathBuf::from(path.strip_prefix("file://").unwrap_or(path))
}

/// Every file under `dir`, recursively, in no particular order.
fn walk(dir: &Path, found: &mut Vec<PathBuf>) -> std::io::Result<()> {
    for entry in std::fs::read_dir(dir)? {
        let entry = entry?;
        let path = entry.path();
        if entry.file_type()?.is_dir() {
            walk(&path, found)?;
        } else {
            found.push(path);
        }
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_local_prefix_holding_nothing_lists_nothing() {
        let dir = tempfile::tempdir().unwrap();
        let objects = Objects::open(&format!("file://{}", dir.path().display())).unwrap();
        let under = format!("file://{}/nothing/here", dir.path().display());
        assert_eq!(
            objects.list(Object::Tokens, &under).unwrap(),
            Vec::<String>::new()
        );
    }

    #[test]
    fn a_local_object_round_trips_and_is_listed_by_its_full_path() {
        let dir = tempfile::tempdir().unwrap();
        let base = format!("file://{}", dir.path().display());
        let objects = Objects::open(&base).unwrap();
        let path = format!("{base}/system/access_tokens/abc.json");
        objects.put(Object::Token, &path, b"{}".to_vec()).unwrap();

        assert_eq!(
            objects.get(Object::Token, &path).unwrap(),
            Some(b"{}".to_vec())
        );
        assert_eq!(
            objects
                .list(Object::Tokens, &format!("{base}/system"))
                .unwrap(),
            vec![path]
        );
    }

    #[test]
    fn an_absent_local_object_is_none() {
        let dir = tempfile::tempdir().unwrap();
        let base = format!("file://{}", dir.path().display());
        let objects = Objects::open(&base).unwrap();
        assert_eq!(
            objects
                .get(Object::Token, &format!("{base}/x.json"))
                .unwrap(),
            None
        );
    }

    #[test]
    fn a_bucket_refuses_a_path_outside_it() {
        let bucket = Bucket {
            runtime: tokio::runtime::Builder::new_current_thread()
                .build()
                .unwrap(),
            client: aws_sdk_s3::Client::from_conf(
                aws_sdk_s3::Config::builder()
                    .behavior_version(aws_sdk_s3::config::BehaviorVersion::latest())
                    .region(aws_sdk_s3::config::Region::new("eu-central-1"))
                    .build(),
            ),
            name: "park".to_string(),
        };
        assert_eq!(bucket.key("s3://park/a/b.json").unwrap(), "a/b.json");
        assert!(bucket.key("s3://other/a/b.json").is_err());
    }
}
