//! Error type for the Postgres storage backend.

// "ERRORS ARE SACRED, WE NEVER DROP, SUPRESS, TRUNCATE, OR ADD LIES TO THEM"

// No variant holds a sentence and the type has no Display, so collapsing an
// error into text does not compile. The library's own error rides whole.

pub type Result<T> = std::result::Result<T, PostgresError>;

/// No `Display`: the one way out is `sacred`, typed to typed.
#[derive(Debug)]
pub enum PostgresError {
    Postgres(postgres::Error),
    Serde(serde_json::Error),
    /// The CA file named for TLS did not read.
    ReadCa {
        path: String,
        source: std::io::Error,
    },
    /// The CA file read, and a certificate in it did not parse or rustls
    /// would not take it.
    BadCa {
        path: String,
        why: String,
    },
    /// The CA file read, and held no certificate.
    NoCa {
        path: String,
    },
    /// A call panicked holding the connection, leaving it mid-statement.
    Poisoned,
    /// The store trait's own error, crossing the FFI.
    Store(ats::storage::StoreError),
    /// The connection string did not parse, or the server refused it.
    Connect {
        source: postgres::Error,
    },
    /// A namespace names the schema its tables live in, so it is held to what
    /// a schema name may be without quoting tricks.
    BadNamespace {
        value: String,
    },
    /// A migration did not apply.
    Migrate {
        version: String,
        source: postgres::Error,
    },
    /// An argument handed over the FFI was not a string.
    BadArgument {
        call: &'static str,
        argument: &'static str,
        source: qntx_ffi_common::CStrError,
    },
}

impl From<postgres::Error> for PostgresError {
    fn from(e: postgres::Error) -> Self {
        PostgresError::Postgres(e)
    }
}

impl From<serde_json::Error> for PostgresError {
    fn from(e: serde_json::Error) -> Self {
        PostgresError::Serde(e)
    }
}

/// The surface every error from this crate names.
pub const SURFACE: &str = "ats-postgres";

impl PostgresError {
    fn region(&self) -> &'static str {
        match self {
            PostgresError::Postgres(_) => "postgres",
            PostgresError::Serde(_) => "serde",
            PostgresError::ReadCa { .. } => "read-ca",
            PostgresError::BadCa { .. } => "bad-ca",
            PostgresError::NoCa { .. } => "no-ca",
            PostgresError::Poisoned => "poisoned",
            PostgresError::Store(e) => match e {
                ats::storage::StoreError::AlreadyExists(_) => "already-exists",
                ats::storage::StoreError::NotFound(_) => "not-found",
                ats::storage::StoreError::InvalidData(_) => "invalid-data",
                ats::storage::StoreError::Backend(_) => "backend",
                ats::storage::StoreError::Query(_) => "query",
                ats::storage::StoreError::Serialization(_) => "serialization",
                ats::storage::StoreError::QuotaExceeded { .. } => "quota-exceeded",
            },
            PostgresError::Connect { .. } => "connect",
            PostgresError::BadNamespace { .. } => "bad-namespace",
            PostgresError::Migrate { .. } => "migrate",
            PostgresError::BadArgument { .. } => "bad-argument",
        }
    }

    fn title(&self) -> String {
        match self {
            PostgresError::Postgres(_) => "postgres refused".to_string(),
            PostgresError::Serde(_) => "JSON did not serialize".to_string(),
            PostgresError::ReadCa { path, .. } => format!("failed to read the CA at {path}"),
            PostgresError::BadCa { path, .. } => {
                format!("the CA at {path} did not read as a certificate")
            }
            PostgresError::NoCa { path } => format!("{path} holds no CA certificate"),
            PostgresError::Poisoned => {
                "a call panicked holding the postgres connection".to_string()
            }
            PostgresError::Store(e) => e.to_string(),
            PostgresError::Connect { .. } => "failed to connect to postgres".to_string(),
            PostgresError::BadNamespace { value } => {
                format!("namespace {value:?} cannot name a schema")
            }
            PostgresError::Migrate { version, .. } => format!("migration {version} did not apply"),
            PostgresError::BadArgument { call, argument, .. } => {
                format!("{call} was handed {argument} that is not a string")
            }
        }
    }

    /// The library's own words, kept apart from the title.
    fn why(&self) -> String {
        match self {
            PostgresError::Postgres(e)
            | PostgresError::Connect { source: e }
            | PostgresError::Migrate { source: e, .. } => e.to_string(),
            PostgresError::Serde(e) => e.to_string(),
            PostgresError::ReadCa { source, .. } => source.to_string(),
            PostgresError::BadCa { why, .. } => why.clone(),
            PostgresError::BadArgument { source, .. } => source.to_string(),
            PostgresError::BadNamespace { .. }
            | PostgresError::NoCa { .. }
            | PostgresError::Poisoned
            | PostgresError::Store(_) => String::new(),
        }
    }

    fn location(&self) -> Option<String> {
        match self {
            PostgresError::ReadCa { path, .. }
            | PostgresError::BadCa { path, .. }
            | PostgresError::NoCa { path } => Some(path.clone()),
            PostgresError::Postgres(_)
            | PostgresError::Serde(_)
            | PostgresError::Connect { .. }
            | PostgresError::BadNamespace { .. }
            | PostgresError::Migrate { .. }
            | PostgresError::BadArgument { .. }
            | PostgresError::Poisoned
            | PostgresError::Store(_) => None,
        }
    }

    fn spoke(&self) -> Option<&'static str> {
        match self {
            PostgresError::Postgres(_)
            | PostgresError::Connect { .. }
            | PostgresError::Migrate { .. } => Some("postgres"),
            PostgresError::Serde(_) => Some("serde_json"),
            PostgresError::ReadCa { .. } => Some("std::io"),
            PostgresError::BadCa { .. } | PostgresError::NoCa { .. } => Some("rustls"),
            PostgresError::Store(_) => Some("ats"),
            PostgresError::Poisoned => None,
            PostgresError::BadArgument { .. } => Some("qntx-ffi-common"),
            PostgresError::BadNamespace { .. } => None,
        }
    }

    /// The one typed value this crosses every layer as. `ffi_call` names the
    /// C function it left through, and is None inside the crate.
    pub fn sacred(self, ffi_call: Option<&str>) -> laye_error::Error {
        // A clock before 1970 says how far before, as a negative instant.
        let at = match std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH) {
            Ok(since) => since.as_millis().to_string(),
            Err(before) => format!("-{}", before.duration().as_millis()),
        };
        laye_error::Error {
            id: format!("err-{SURFACE}-{at}"),
            severity: laye_error::Severity::Error,
            context: laye_error::Context {
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
            location: self.location(),
            js_stack: None,
            raw_stderr: None,
            requires_reload: false,
        }
    }

    /// The sacred value as the bytes that cross the FFI.
    /// An error that does not serialize crosses as a JSON string saying so,
    /// with its title, which the Go side keeps whole as unshaped.
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
