//! 「[Opus 5.5] Error type for the etcd storage backend, in ats-postgres's shape:
//! 「[Opus 5.5] no variant holds a sentence and the type has no Display, so the library's」
//! 「[Opus 5.5] own error rides whole and leaves only through `sacred`.」」

pub type Result<T> = std::result::Result<T, EtcdError>;

#[derive(Debug)]
pub enum EtcdError {
    /// 「[Opus 5.5] etcd answered with an error, or did not answer.」
    Etcd(etcd_client::Error),
    Serde(serde_json::Error),
    /// 「[Opus 5.5] The runtime the client's calls are driven on did not start.」
    Runtime(std::io::Error),
    /// 「[Opus 5.5] The endpoint named did not take a connection.」
    Connect {
        endpoint: String,
        source: etcd_client::Error,
    },
    /// 「[Opus 5.5] A namespace is a key prefix, so it is held to what a schema name may be in ats-postgres.」
    BadNamespace {
        value: String,
    },
    /// 「[Opus 5.5] Keys are kept under a prefix that starts and ends at `/`.」
    BadPrefix {
        value: String,
    },
    /// 「[Opus 5.5] An argument handed over the FFI was not a string.」
    BadArgument {
        call: &'static str,
        argument: &'static str,
        source: qntx_ffi_common::CStrError,
    },
    /// 「[Opus 5.5] A pointer handed over the FFI was null.」
    NullPointer {
        call: &'static str,
        argument: &'static str,
    },
    /// 「[Opus 5.5] An argument handed over the FFI was longer than the call takes.」
    TooLong {
        call: &'static str,
        argument: &'static str,
        length: usize,
        most: usize,
    },
    /// 「[Opus 5.5] The store trait's own error, crossing the FFI.」
    Store(ats::storage::StoreError),
}

impl From<etcd_client::Error> for EtcdError {
    fn from(e: etcd_client::Error) -> Self {
        EtcdError::Etcd(e)
    }
}

impl From<serde_json::Error> for EtcdError {
    fn from(e: serde_json::Error) -> Self {
        EtcdError::Serde(e)
    }
}

impl EtcdError {
    /// Whether etcd refused for a token it no longer holds: one left unused past
    /// etcd's --auth-token-ttl is forgotten, and asking again with it is refused
    /// every time.
    pub(crate) fn stale_token(&self) -> bool {
        matches!(self, EtcdError::Etcd(e) if e.to_string().contains("invalid auth token"))
    }
}

/// 「[Opus 5.5] The surface every error from this crate names.」
pub const SURFACE: &str = "ats-etcd";

impl EtcdError {
    fn region(&self) -> &'static str {
        match self {
            EtcdError::Etcd(_) => "etcd",
            EtcdError::Serde(_) => "serde",
            EtcdError::Runtime(_) => "runtime",
            EtcdError::Connect { .. } => "connect",
            EtcdError::BadNamespace { .. } => "bad-namespace",
            EtcdError::BadPrefix { .. } => "bad-prefix",
            EtcdError::BadArgument { .. } => "bad-argument",
            EtcdError::NullPointer { .. } => "null-pointer",
            EtcdError::TooLong { .. } => "too-long",
            EtcdError::Store(e) => match e {
                ats::storage::StoreError::AlreadyExists(_) => "already-exists",
                ats::storage::StoreError::NotFound(_) => "not-found",
                ats::storage::StoreError::Unwritable(_) => qntx_ffi_common::store::UNWRITABLE,
                ats::storage::StoreError::InvalidData(_) => "invalid-data",
                ats::storage::StoreError::Backend(_) => "backend",
                ats::storage::StoreError::Query(_) => "query",
                ats::storage::StoreError::Serialization(_) => "serialization",
            },
        }
    }

    fn title(&self) -> String {
        match self {
            EtcdError::Etcd(_) => "etcd refused".to_string(),
            EtcdError::Serde(_) => "JSON did not serialize".to_string(),
            EtcdError::Runtime(_) => "the tokio runtime did not start".to_string(),
            EtcdError::Connect { endpoint, .. } => {
                format!("failed to connect to etcd at {endpoint}")
            }
            EtcdError::BadNamespace { value } => {
                format!("namespace {value:?} cannot name a key prefix")
            }
            EtcdError::BadPrefix { value } => {
                format!("prefix {value:?} does not start and end at /")
            }
            EtcdError::NullPointer { call, argument } => {
                format!("{call} was handed a null {argument}")
            }
            EtcdError::TooLong {
                call,
                argument,
                length,
                most,
            } => {
                format!("{call} was handed {argument} of {length} bytes, past the {most} it takes")
            }
            EtcdError::BadArgument { call, argument, .. } => {
                format!("{call} was handed {argument} that is not a string")
            }
            EtcdError::Store(e) => e.to_string(),
        }
    }

    /// 「[Opus 5.5] The library's own words, kept apart from the title.」
    fn why(&self) -> String {
        match self {
            EtcdError::Etcd(e) | EtcdError::Connect { source: e, .. } => e.to_string(),
            EtcdError::Serde(e) => e.to_string(),
            EtcdError::Runtime(e) => e.to_string(),
            EtcdError::BadArgument { source, .. } => source.to_string(),
            EtcdError::BadNamespace { .. }
            | EtcdError::BadPrefix { .. }
            | EtcdError::NullPointer { .. }
            | EtcdError::TooLong { .. }
            | EtcdError::Store(_) => String::new(),
        }
    }

    fn spoke(&self) -> Option<&'static str> {
        match self {
            EtcdError::Etcd(_) | EtcdError::Connect { .. } => Some("etcd-client"),
            EtcdError::Serde(_) => Some("serde_json"),
            EtcdError::Runtime(_) => Some("tokio"),
            EtcdError::BadArgument { .. } => Some("qntx-ffi-common"),
            EtcdError::Store(_) => Some("ats"),
            EtcdError::BadNamespace { .. }
            | EtcdError::BadPrefix { .. }
            | EtcdError::NullPointer { .. }
            | EtcdError::TooLong { .. } => None,
        }
    }

    /// 「[Opus 5.5] The one typed value this crosses every layer as. `ffi_call` names the
    /// 「[Opus 5.5] C function it left through, and is None inside the crate.」」
    pub fn sacred(self, ffi_call: Option<&str>) -> sacred_error::Error {
        let at = match std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH) {
            Ok(since) => since.as_millis().to_string(),
            Err(before) => format!("-{}", before.duration().as_millis()),
        };
        sacred_error::Error {
            id: format!("err-{SURFACE}-{at}"),
            severity: sacred_error::Severity::Error,
            context: sacred_error::Context {
                surface: SURFACE.to_string(),
                region: Some(self.region().to_string()),
                anchor: None,
            },
            title: self.title(),
            why: self.why(),
            trace: Vec::new(),
            raw: Some(format!("{self:?}")),
            at,
            source: self.spoke().map(str::to_string),
            ffi_call: ffi_call.map(str::to_string),
            location: None,
            js_stack: None,
            raw_stderr: None,
            requires_reload: false,
        }
    }

    /// 「[Opus 5.5] The sacred value as JSON. An error that does not serialize is a
    /// 「[Opus 5.5] JSON string saying so, with its title.」」
    pub fn sacred_json(self, ffi_call: Option<&str>) -> String {
        let sacred = self.sacred(ffi_call);
        match serde_json::to_string(&sacred) {
            Ok(json) => json,
            Err(e) => serde_json::Value::String(format!(
                "{} (and the error itself did not serialize: {e})",
                sacred.title
            ))
            .to_string(),
        }
    }
}
