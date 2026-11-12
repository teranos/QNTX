//! 「[Sonnet 5.5] The stores `ats-dev` can open behind `--backend`.」
//!
//! 「[Sonnet 5.5] `AttestationStore` is the one trait all three implement; `Store` forwards」
//! 「[Sonnet 5.5] each of its methods to the backend, so a command written against the trait」
//! 「[Sonnet 5.5] runs on any of them and what comes out is that backend's own answer.」
//! 「[Sonnet 5.5] `QueryStore` and `AliasStore` exist for `SqliteStore` only」
//! 「[Sonnet 5.5] (`crates/ats/src/storage/traits.rs`); a command that needs either says so」
//! 「[Sonnet 5.5] and stops rather than answering from something else.」

use ats::{Attestation, AttestationStore};
use ats::storage::StoreResult;
use ats_sqlite::SqliteStore;
use sacred_error::Error;

use crate::fail;

#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub enum Kind {
    Sqlite,
    Duckdb,
    Postgres,
}

impl Kind {
    pub fn parse(s: &str) -> Result<Self, Box<Error>> {
        match s {
            "sqlite" => Ok(Kind::Sqlite),
            "duckdb" => Ok(Kind::Duckdb),
            "postgres" => Ok(Kind::Postgres),
            other => Err(fail("--backend takes sqlite, duckdb or postgres", other)),
        }
    }

    pub fn name(self) -> &'static str {
        match self {
            Kind::Sqlite => "sqlite",
            Kind::Duckdb => "duckdb",
            Kind::Postgres => "postgres",
        }
    }

    /// 「[Sonnet 5.5] What `--db` is called when it is not given: SQLite's file, DuckDB's」
    /// 「[Sonnet 5.5] location, and for Postgres the url the repo's own scripts export.」
    pub fn default_db(self) -> Result<String, Box<Error>> {
        match self {
            Kind::Sqlite => Ok("ats-dev.db".into()),
            Kind::Duckdb => Ok("ats-dev-parquet".into()),
            Kind::Postgres => std::env::var("QNTX_POSTGRES_URL").map_err(|e| {
                fail(
                    "postgres needs a connection url",
                    format!("--db URL, or $ATS_DEV_DB, or $QNTX_POSTGRES_URL (scripts/with-postgres.sh sets it): {e}"),
                )
            }),
        }
    }
}

pub enum Store {
    Sqlite(SqliteStore),
    #[cfg(feature = "duckdb")]
    Duckdb(ats_duckdb::DuckdbStore),
    #[cfg(feature = "postgres")]
    Postgres(ats_postgres::PostgresStore),
}

/// 「[Sonnet 5.5] Forward to whichever backend is open.」
macro_rules! each {
    ($self:expr, $s:ident => $e:expr) => {
        match $self {
            Store::Sqlite($s) => $e,
            #[cfg(feature = "duckdb")]
            Store::Duckdb($s) => $e,
            #[cfg(feature = "postgres")]
            Store::Postgres($s) => $e,
        }
    };
}

impl Store {
    pub fn kind(&self) -> Kind {
        match self {
            Store::Sqlite(_) => Kind::Sqlite,
            #[cfg(feature = "duckdb")]
            Store::Duckdb(_) => Kind::Duckdb,
            #[cfg(feature = "postgres")]
            Store::Postgres(_) => Kind::Postgres,
        }
    }

    /// 「[Sonnet 5.5] The SQLite store, if that is what is open: the connection `put -`」
    /// 「[Sonnet 5.5] batches on, and the only store with `QueryStore` and `AliasStore`.」
    pub fn as_sqlite(&self) -> Option<&SqliteStore> {
        match self {
            Store::Sqlite(s) => Some(s),
            #[cfg(any(feature = "duckdb", feature = "postgres"))]
            _ => None,
        }
    }

    fn as_sqlite_mut(&mut self) -> Option<&mut SqliteStore> {
        match self {
            Store::Sqlite(s) => Some(s),
            #[cfg(any(feature = "duckdb", feature = "postgres"))]
            _ => None,
        }
    }

    /// 「[Sonnet 5.5] The SQLite store for a command that needs `needs` (a trait name), or the」
    /// 「[Sonnet 5.5] refusal that says which backend lacks it.」
    pub fn sqlite(&self, needs: &str) -> Result<&SqliteStore, Box<Error>> {
        self.as_sqlite().ok_or_else(|| lacks(self.kind(), needs))
    }

    pub fn sqlite_mut(&mut self, needs: &str) -> Result<&mut SqliteStore, Box<Error>> {
        let kind = self.kind();
        self.as_sqlite_mut().ok_or_else(|| lacks(kind, needs))
    }

    /// 「[Sonnet 5.5] Make what was written durable where the backend holds writes back: the」
    /// 「[Sonnet 5.5] DuckDB store keeps them in a buffer until `flush`, and only its `Drop`」
    /// 「[Sonnet 5.5] flushes, where a failure can be logged and not returned. A command says」
    /// 「[Sonnet 5.5] "written" only after this.」
    pub fn persist(&self) -> Result<(), Box<Error>> {
        match self {
            Store::Sqlite(_) => Ok(()),
            #[cfg(feature = "duckdb")]
            Store::Duckdb(s) => s.flush().map(|_| ()).map_err(|e| Box::new(e.sacred(""))),
            #[cfg(feature = "postgres")]
            Store::Postgres(_) => Ok(()),
        }
    }
}

fn lacks(kind: Kind, needs: &str) -> Box<Error> {
    fail(
        &format!("{} does not implement {needs}", kind.name()),
        format!("only SqliteStore does (crates/ats/src/storage/traits.rs); {} implements AttestationStore alone", kind.name()),
    )
}

impl AttestationStore for Store {
    fn put(&mut self, a: Attestation) -> StoreResult<()> {
        each!(self, s => s.put(a))
    }
    fn get(&self, id: &str) -> StoreResult<Option<Attestation>> {
        each!(self, s => s.get(id))
    }
    fn exists(&self, id: &str) -> StoreResult<bool> {
        each!(self, s => s.exists(id))
    }
    fn delete(&mut self, id: &str) -> StoreResult<bool> {
        each!(self, s => s.delete(id))
    }
    fn update(&mut self, a: Attestation) -> StoreResult<()> {
        each!(self, s => s.update(a))
    }
    fn ids(&self) -> StoreResult<Vec<String>> {
        each!(self, s => s.ids())
    }
    fn count(&self) -> StoreResult<usize> {
        each!(self, s => s.count())
    }
    fn clear(&mut self) -> StoreResult<()> {
        each!(self, s => s.clear())
    }
}
