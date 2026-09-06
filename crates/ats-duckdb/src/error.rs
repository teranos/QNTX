//! Error type for the DuckDB storage backend.

// "ERRORS ARE SACRED, WE NEVER DROP, SUPRESS, TRUNCATE, OR ADD LIES TO THEM"

// No variant holds a sentence, so collapsing an error into text does not
// compile. The library's own error rides whole as `source`.

use thiserror::Error;

pub type Result<T> = std::result::Result<T, DuckdbError>;

/// What was being read or written when the location refused.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Object {
    Attestations,
    ParquetFiles,
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
}

#[derive(Debug, Error)]
pub enum DuckdbError {
    #[error("duckdb error: {0}")]
    Duckdb(#[from] duckdb::Error),

    #[error("io error: {0}")]
    Io(#[from] std::io::Error),

    #[error("serialization error: {0}")]
    Serde(#[from] serde_json::Error),

    #[error("failed to write {what} {path}")]
    Write {
        what: Object,
        path: String,
        #[source]
        source: duckdb::Error,
    },

    #[error("failed to read {what} under {under}")]
    Read {
        what: Object,
        under: String,
        #[source]
        source: duckdb::Error,
    },

    /// Failed, the credentials were resolved again, and failed again. Both
    /// attempts are here: the second decides, the first is what was seen.
    #[error("failed to read {what} under {under} (also failed before the credentials were resolved again: {first})")]
    ReadTwice {
        what: Object,
        under: String,
        first: Box<duckdb::Error>,
        #[source]
        source: Box<duckdb::Error>,
    },

    #[error("failed to read {what} under {under}: {first}; and the credentials could not be resolved again")]
    ReadThenNoCredentials {
        what: Object,
        under: String,
        first: Box<duckdb::Error>,
        #[source]
        source: Box<duckdb::Error>,
    },

    #[error("failed to buffer {what}")]
    Buffer {
        what: Object,
        #[source]
        source: duckdb::Error,
    },

    #[error("{what} under {path} is not readable JSON")]
    NotJSON {
        what: Object,
        path: String,
        #[source]
        source: serde_json::Error,
    },

    #[error("{path} does not read as {what}")]
    NotTOML {
        what: Object,
        path: String,
        #[source]
        source: Box<toml::de::Error>,
    },

    #[error("failed to serialize {what} {id}")]
    NotSerializable {
        what: Object,
        id: String,
        #[source]
        source: serde_json::Error,
    },

    #[error("the clock is before the unix epoch")]
    ClockBeforeEpoch(#[source] std::time::SystemTimeError),

    #[error("{which:?} {value:?} is refused: {why:?}")]
    BadName {
        which: Name,
        value: String,
        why: Refusal,
    },

    #[error("namespace {name} already exists and already has an owner")]
    NamespaceExists { name: String },

    #[error("token {id} vanished during {operation}")]
    TokenVanished { id: String, operation: String },

    #[error("linked libduckdb is {linked}, bindings were generated against {expected}")]
    VersionMismatch { linked: String, expected: String },

    #[error("expected {expected}, got {got:?}")]
    ColumnShape {
        expected: &'static str,
        got: duckdb::types::Value,
    },
}
