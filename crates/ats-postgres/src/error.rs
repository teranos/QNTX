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
    /// The CA file read, and held no certificate rustls would take.
    BadCa {
        path: String,
        why: String,
    },
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
            PostgresError::BadCa { path, .. } => format!("{path} holds no CA certificate"),
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
            PostgresError::BadNamespace { .. } => String::new(),
        }
    }

    fn location(&self) -> Option<String> {
        match self {
            PostgresError::ReadCa { path, .. } | PostgresError::BadCa { path, .. } => {
                Some(path.clone())
            }
            _ => None,
        }
    }

    fn spoke(&self) -> Option<&'static str> {
        match self {
            PostgresError::Postgres(_)
            | PostgresError::Connect { .. }
            | PostgresError::Migrate { .. } => Some("postgres"),
            PostgresError::Serde(_) => Some("serde_json"),
            PostgresError::ReadCa { .. } => Some("std::io"),
            PostgresError::BadCa { .. } => Some("rustls"),
            PostgresError::BadArgument { .. } => Some("qntx-ffi-common"),
            PostgresError::BadNamespace { .. } => None,
        }
    }

    /// The one typed value this crosses every layer as. `ffi_call` names the
    /// C function it left through, or is empty inside the crate.
    pub fn sacred(self, ffi_call: &str) -> laye_error::Error {
        let at = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map(|d| d.as_millis().to_string())
            .unwrap_or_default();
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
            ffi_call: (!ffi_call.is_empty()).then(|| ffi_call.to_string()),
            location: self.location(),
            js_stack: None,
            raw_stderr: None,
            requires_reload: false,
        }
    }

    /// The sacred value as the bytes that cross the FFI.
    pub fn sacred_json(self, ffi_call: &str) -> String {
        let sacred = self.sacred(ffi_call);
        match serde_json::to_string(&sacred) {
            Ok(json) => json,
            Err(e) => {
                let mut fallback = sacred;
                fallback.title = format!(
                    "{} (and the error itself did not serialize: {e})",
                    fallback.title
                );
                fallback.raw = None;
                serde_json::to_string(&fallback).unwrap_or_default()
            }
        }
    }
}
