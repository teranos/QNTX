//! Error type for the DuckDB storage backend.

// "ERRORS ARE SACRED, WE NEVER DROP, SUPRESS, TRUNCATE, OR ADD LIES TO THEM"

// No variant holds a sentence and the type has no Display, so collapsing an
// error into text does not compile. The library's own error rides whole.

pub type Result<T> = std::result::Result<T, DuckdbError>;

/// What was being read or written when the location refused.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Object {
    Attestations,
    ParquetFiles,
    /// The record of a compaction that is underway: which files it merged.
    Compaction,
    Token,
    Tokens,
    User,
    Users,
    Watcher,
    Watchers,
    FireEvent,
    FireEvents,
    /// The fires of one watcher, by its id.
    FiresOf(String),
    Schedule,
    Schedules,
    Tick,
    Ticks,
    NodeIdentity,
    Namespace,
    Namespaces,
    NamespaceDefinition,
}

impl std::fmt::Display for Object {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        if let Object::FiresOf(watcher) = self {
            return write!(f, "the fires of {watcher}");
        }
        let word = match self {
            Object::Attestations => "attestations",
            Object::ParquetFiles => "the Parquet files",
            Object::Compaction => "the compaction underway",
            Object::Token => "an access token object",
            Object::Tokens => "the access tokens",
            Object::User => "a User object",
            Object::Users => "the Users",
            Object::Watcher => "a watcher object",
            Object::Watchers => "the watcher objects",
            Object::FireEvent => "a fire event",
            Object::FireEvents => "fire events",
            Object::FiresOf(_) => "the fires",
            Object::Schedule => "a schedule object",
            Object::Schedules => "the schedule objects",
            Object::Tick => "a tick",
            Object::Ticks => "ticks",
            Object::NodeIdentity => "the node identity",
            Object::Namespace => "the namespace",
            Object::Namespaces => "what the location holds",
            Object::NamespaceDefinition => "a namespace definition",
        };
        f.write_str(word)
    }
}

/// Which name was refused, when a name cannot be used.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Name {
    Location,
    NamespaceName,
    UserID,
    DefinitionField,
}

/// Why a name was refused.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Refusal {
    CarriesAQuote,
    CarriesAQuoteBackslashOrLineBreak,
    NotAPathSegment,
    NotAnObjectName,
    NoBucket,
    OutsideTheBucket,
}
/// No `Display`: a value that cannot be formatted cannot be flattened. The one
/// way out is `sacred`, typed to typed.
#[derive(Debug)]
pub enum DuckdbError {
    Duckdb(duckdb::Error),
    Io(std::io::Error),
    Serde(serde_json::Error),
    Write {
        what: Object,
        path: String,
        source: duckdb::Error,
    },
    Read {
        what: Object,
        under: String,
        source: duckdb::Error,
    },
    /// Failed, the credentials were resolved again, and failed again. Both
    /// attempts are here: the second decides, the first is what was seen.
    ReadTwice {
        what: Object,
        under: String,
        first: Box<duckdb::Error>,
        source: Box<duckdb::Error>,
    },
    ReadThenNoCredentials {
        what: Object,
        under: String,
        first: Box<duckdb::Error>,
        source: Box<duckdb::Error>,
    },
    Buffer {
        what: Object,
        source: duckdb::Error,
    },
    NotJSON {
        what: Object,
        path: String,
        source: serde_json::Error,
    },
    NotTOML {
        what: Object,
        path: String,
        source: Box<toml::de::Error>,
    },
    NotSerializable {
        what: Object,
        id: String,
        source: serde_json::Error,
    },
    ClockBeforeEpoch(std::time::SystemTimeError),
    BadName {
        which: Name,
        value: String,
        why: Refusal,
    },
    NamespaceExists {
        name: String,
    },
    TokenVanished {
        id: String,
        operation: String,
    },
    VersionMismatch {
        linked: String,
        expected: String,
    },
    ColumnShape {
        expected: &'static str,
        got: duckdb::types::Value,
    },
    /// An argument handed over the FFI was not a string.
    BadArgument {
        call: &'static str,
        argument: &'static str,
        source: qntx_ffi_common::CStrError,
    },
    /// Nothing under that id, where something was asked for by id.
    NotFound {
        what: Object,
        id: String,
        operation: String,
    },
    /// A store did not open. The failure inside rides whole.
    Open {
        what: Object,
        location: String,
        namespace: Option<String>,
        source: Box<DuckdbError>,
    },
    /// S3 refused one request, or the request never reached it. The SDK's
    /// value rides whole, and with it the response S3 sent, body included.
    S3 {
        request: crate::objects::Request,
        what: Object,
        path: String,
        source: Box<crate::objects::S3Failure>,
    },
    /// The filesystem refused one object.
    WriteFile {
        what: Object,
        path: String,
        source: std::io::Error,
    },
    ReadFile {
        what: Object,
        path: String,
        source: std::io::Error,
    },
    /// The async runtime the S3 client needs did not start.
    Runtime(std::io::Error),
}

impl From<duckdb::Error> for DuckdbError {
    fn from(e: duckdb::Error) -> Self {
        DuckdbError::Duckdb(e)
    }
}

impl From<std::io::Error> for DuckdbError {
    fn from(e: std::io::Error) -> Self {
        DuckdbError::Io(e)
    }
}

impl From<serde_json::Error> for DuckdbError {
    fn from(e: serde_json::Error) -> Self {
        DuckdbError::Serde(e)
    }
}

/// The surface every error from this crate names.
pub const SURFACE: &str = "ats-duckdb";

impl DuckdbError {
    /// The variant's name, for the region field: what kind of failure.
    fn region(&self) -> &'static str {
        match self {
            DuckdbError::Duckdb(_) => "duckdb",
            DuckdbError::Io(_) => "io",
            DuckdbError::Serde(_) => "serde",
            DuckdbError::Write { .. } => "write",
            DuckdbError::Read { .. } => "read",
            DuckdbError::ReadTwice { .. } => "read-twice",
            DuckdbError::ReadThenNoCredentials { .. } => "read-then-no-credentials",
            DuckdbError::Buffer { .. } => "buffer",
            DuckdbError::NotJSON { .. } => "not-json",
            DuckdbError::NotTOML { .. } => "not-toml",
            DuckdbError::NotSerializable { .. } => "not-serializable",
            DuckdbError::ClockBeforeEpoch(_) => "clock-before-epoch",
            DuckdbError::BadName { .. } => "bad-name",
            DuckdbError::NamespaceExists { .. } => "namespace-exists",
            DuckdbError::TokenVanished { .. } => "token-vanished",
            DuckdbError::VersionMismatch { .. } => "version-mismatch",
            DuckdbError::ColumnShape { .. } => "column-shape",
            DuckdbError::BadArgument { .. } => "bad-argument",
            DuckdbError::NotFound { .. } => "not-found",
            DuckdbError::Open { .. } => "open",
            DuckdbError::S3 { .. } => "s3",
            DuckdbError::WriteFile { .. } => "write-file",
            DuckdbError::ReadFile { .. } => "read-file",
            DuckdbError::Runtime(_) => "runtime",
        }
    }

    /// What was being done, in words, from the fields. Rendered here at the
    /// edge and nowhere else.
    fn title(&self) -> String {
        match self {
            DuckdbError::Duckdb(_) => "duckdb refused".to_string(),
            DuckdbError::Io(_) => "the filesystem refused".to_string(),
            DuckdbError::Serde(_) => "JSON did not serialize".to_string(),
            DuckdbError::Write { what, path, .. } => format!("failed to write {what} {path}"),
            DuckdbError::Read { what, under, .. } => format!("failed to read {what} under {under}"),
            DuckdbError::ReadTwice { what, under, .. } => format!(
                "failed to read {what} under {under}, before and after the credentials were resolved again"
            ),
            DuckdbError::ReadThenNoCredentials { what, under, .. } => format!(
                "failed to read {what} under {under}, and the credentials could not be resolved again"
            ),
            DuckdbError::Buffer { what, .. } => format!("failed to buffer {what}"),
            DuckdbError::NotJSON { what, path, .. } => format!("{what} under {path} is not readable JSON"),
            DuckdbError::NotTOML { what, path, .. } => format!("{path} does not read as {what}"),
            DuckdbError::NotSerializable { what, id, .. } => format!("failed to serialize {what} {id}"),
            DuckdbError::ClockBeforeEpoch(_) => "the clock is before the unix epoch".to_string(),
            DuckdbError::BadName { which, value, why } => format!("{which:?} {value:?} is refused: {why:?}"),
            DuckdbError::NamespaceExists { name } => {
                format!("namespace {name} already exists and already has an owner")
            }
            DuckdbError::TokenVanished { id, operation } => format!("token {id} vanished during {operation}"),
            DuckdbError::VersionMismatch { linked, expected } => {
                format!("linked libduckdb is {linked}, bindings were generated against {expected}")
            }
            DuckdbError::ColumnShape { expected, got } => format!("expected {expected}, got {got:?}"),
            DuckdbError::BadArgument { call, argument, .. } => {
                format!("{call} was handed {argument} that is not a string")
            }
            DuckdbError::NotFound { what, id, operation } => format!("no {what} matched {id} on {operation}"),
            DuckdbError::Open { what, location, namespace: Some(ns), source } => {
                format!("failed to open {what} at {location} for {ns}: {}", source.title())
            }
            DuckdbError::Open { what, location, namespace: None, source } => {
                format!("failed to open {what} at {location}: {}", source.title())
            }
            DuckdbError::S3 { request, what, path, .. } => {
                format!("S3 did not complete {request} of {what} at {path}")
            }
            DuckdbError::WriteFile { what, path, .. } => format!("failed to write {what} {path}"),
            DuckdbError::ReadFile { what, path, .. } => format!("failed to read {what} {path}"),
            DuckdbError::Runtime(_) => "the async runtime for S3 did not start".to_string(),
        }
    }

    /// The library's own words, when a library spoke. Kept apart from the
    /// title so nobody mistakes duckdb's sentence for this crate's finding.
    fn why(&self) -> String {
        match self {
            DuckdbError::Duckdb(e)
            | DuckdbError::Write { source: e, .. }
            | DuckdbError::Read { source: e, .. }
            | DuckdbError::Buffer { source: e, .. } => e.to_string(),
            DuckdbError::ReadTwice { source, .. }
            | DuckdbError::ReadThenNoCredentials { source, .. } => source.to_string(),
            DuckdbError::Io(e)
            | DuckdbError::WriteFile { source: e, .. }
            | DuckdbError::ReadFile { source: e, .. }
            | DuckdbError::Runtime(e) => e.to_string(),
            // S3's own words: the status line and the body it sent.
            DuckdbError::S3 { source, .. } => source.said(),
            DuckdbError::Serde(e)
            | DuckdbError::NotJSON { source: e, .. }
            | DuckdbError::NotSerializable { source: e, .. } => e.to_string(),
            DuckdbError::NotTOML { source, .. } => source.to_string(),
            DuckdbError::ClockBeforeEpoch(e) => e.to_string(),
            DuckdbError::BadArgument { source, .. } => source.to_string(),
            DuckdbError::Open { source, .. } => source.why(),
            DuckdbError::BadName { .. }
            | DuckdbError::NamespaceExists { .. }
            | DuckdbError::TokenVanished { .. }
            | DuckdbError::NotFound { .. }
            | DuckdbError::VersionMismatch { .. }
            | DuckdbError::ColumnShape { .. } => String::new(),
        }
    }

    /// Where, when a location is part of it.
    fn location(&self) -> Option<String> {
        match self {
            DuckdbError::Write { path, .. }
            | DuckdbError::S3 { path, .. }
            | DuckdbError::WriteFile { path, .. }
            | DuckdbError::ReadFile { path, .. }
            | DuckdbError::NotJSON { path, .. }
            | DuckdbError::NotTOML { path, .. } => Some(path.clone()),
            DuckdbError::Read { under, .. }
            | DuckdbError::ReadTwice { under, .. }
            | DuckdbError::ReadThenNoCredentials { under, .. } => Some(under.clone()),
            DuckdbError::Open {
                location, source, ..
            } => Some(source.location().unwrap_or_else(|| location.clone())),
            _ => None,
        }
    }

    /// Every attempt, oldest first, for the two-attempt variants.
    fn trace(&self) -> Vec<String> {
        match self {
            DuckdbError::ReadTwice { first, source, .. }
            | DuckdbError::ReadThenNoCredentials { first, source, .. } => {
                vec![first.to_string(), source.to_string()]
            }
            DuckdbError::Open { source, .. } => source.trace(),
            // The SDK's chain of sources, then the request ids S3 stamped on
            // its answer, so AWS can be asked about the same request.
            DuckdbError::S3 { source, .. } => {
                let mut trace = vec![source.chain()];
                trace.extend(source.request_ids());
                trace
            }
            _ => Vec::new(),
        }
    }

    /// Which library spoke in `why`, if one did.
    fn spoke(&self) -> Option<&'static str> {
        match self {
            DuckdbError::Duckdb(_)
            | DuckdbError::Write { .. }
            | DuckdbError::Read { .. }
            | DuckdbError::ReadTwice { .. }
            | DuckdbError::ReadThenNoCredentials { .. }
            | DuckdbError::Buffer { .. } => Some("duckdb"),
            DuckdbError::Io(_)
            | DuckdbError::WriteFile { .. }
            | DuckdbError::ReadFile { .. }
            | DuckdbError::Runtime(_) => Some("std::io"),
            DuckdbError::S3 { .. } => Some("s3"),
            DuckdbError::Serde(_)
            | DuckdbError::NotJSON { .. }
            | DuckdbError::NotSerializable { .. } => Some("serde_json"),
            DuckdbError::NotTOML { .. } => Some("toml"),
            DuckdbError::ClockBeforeEpoch(_) => Some("std::time"),
            DuckdbError::BadArgument { .. } => Some("qntx-ffi-common"),
            DuckdbError::Open { source, .. } => source.spoke(),
            _ => None,
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
            trace: self.trace(),
            // The whole value, so nothing in it is lost on the way.
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

    /// The sacred value as the bytes that cross the FFI. Serialising the shape
    /// cannot fail for these fields; if it ever did, the failure is itself
    /// carried rather than replaced with a sentence.
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
                fallback.trace = Vec::new();
                fallback.raw = None;
                serde_json::to_string(&fallback).unwrap_or_default()
            }
        }
    }
}
