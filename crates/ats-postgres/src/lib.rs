//! Postgres storage backend for QNTX attestations.
//!
//! "You build QNTX's third storage backend on Supabase's free tier, running
//! Supabase Postgres 17.11.0.003."
//!
//! A namespace is a schema: its tables live there, and every statement names
//! the schema rather than leaning on a session's search path, which a pooler
//! does not keep from one transaction to the next.

pub mod error;
pub mod migrate;

#[cfg(feature = "ffi")]
pub mod ffi;

use std::collections::HashMap;
use std::sync::{Arc, Mutex, MutexGuard};

use ats::attestation::Attestation;
use ats::storage::{AttestationStore, StoreError};
use postgres::config::SslMode;
use postgres::error::SqlState;
use postgres::types::ToSql;
use postgres::{Client, NoTls, Row};
use rustls::pki_types::pem::PemObject;
use serde::Deserialize;

use crate::error::{PostgresError, Result};

type StoreResult<T> = std::result::Result<T, StoreError>;

/// Every column of an attestation, in the order rows are read.
const COLUMNS: &str = "id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at, signature, signer_did";

/// What a filter query asks for. A field left out does not constrain the
/// query; a field given constrains it, an empty list to nothing. The limit is
/// always given: 0 is no rows.
#[derive(Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct QueryFilter {
    pub subjects: Option<Vec<String>>,
    pub predicates: Option<Vec<String>>,
    pub contexts: Option<Vec<String>>,
    pub actors: Option<Vec<String>>,
    pub source: Option<String>,
    pub time_start: Option<i64>,
    pub time_end: Option<i64>,
    pub limit: u32,
}

/// The schema a namespace's tables live in, quoted. A namespace is held to
/// lowercase letters, digits, `-` and `_`, so the quoting has nothing to escape.
pub fn schema(namespace: &str) -> Result<String> {
    // A Postgres identifier is 1 to 63 bytes.
    let fits = (1..=63).contains(&namespace.len())
        && namespace
            .bytes()
            .all(|b| b.is_ascii_lowercase() || b.is_ascii_digit() || b == b'-' || b == b'_');
    if !fits {
        return Err(PostgresError::BadNamespace {
            value: namespace.to_string(),
        });
    }
    Ok(format!("\"{namespace}\""))
}

/// Connect to `url`. The url says whether it is TLS, as libpq's does:
/// `sslmode=disable` is in the clear, and anything else verifies the server
/// against the CA at `ca`.
pub fn connect(url: &str, ca: &str) -> Result<Client> {
    let config: postgres::Config = url
        .parse()
        .map_err(|source| PostgresError::Connect { source })?;
    if config.get_ssl_mode() == SslMode::Disable {
        return config
            .connect(NoTls)
            .map_err(|source| PostgresError::Connect { source });
    }
    let pem = std::fs::read(ca).map_err(|source| PostgresError::ReadCa {
        path: ca.to_string(),
        source,
    })?;
    let certs = rustls::pki_types::CertificateDer::pem_slice_iter(&pem)
        .collect::<std::result::Result<Vec<_>, _>>()
        .map_err(|e| PostgresError::BadCa {
            path: ca.to_string(),
            why: format!("{e:?}"),
        })?;
    let Some(_) = certs.first() else {
        return Err(PostgresError::NoCa {
            path: ca.to_string(),
        });
    };
    let mut roots = rustls::RootCertStore::empty();
    for cert in certs {
        roots.add(cert).map_err(|e| PostgresError::BadCa {
            path: ca.to_string(),
            why: e.to_string(),
        })?;
    }
    let tls_config = rustls::ClientConfig::builder_with_provider(Arc::new(
        rustls::crypto::ring::default_provider(),
    ))
    .with_safe_default_protocol_versions()
    .map_err(|e| PostgresError::BadCa {
        path: ca.to_string(),
        why: e.to_string(),
    })?
    .with_root_certificates(roots)
    .with_no_client_auth();
    let tls = tokio_postgres_rustls::MakeRustlsConnect::new(tls_config);
    config
        .connect(tls)
        .map_err(|source| PostgresError::Connect { source })
}

/// The tables the migrations leave standing in a namespace, applied by this
/// crate's runner, and the version the server says it is. make parity reads
/// both from here.
pub fn schema_tables(url: &str, ca: &str, namespace: &str) -> Result<(Vec<String>, String)> {
    let mut client = connect(url, ca)?;
    migrate::migrate(&mut client, namespace)?;
    let rows = client.query(
        "SELECT table_name::text FROM information_schema.tables WHERE table_schema = $1 AND table_type = 'BASE TABLE' ORDER BY table_name",
        &[&namespace],
    )?;
    let tables = rows.iter().map(|row| row.get::<_, String>(0)).collect();
    let version: String = client.query_one("SHOW server_version", &[])?.get(0);
    Ok((tables, version))
}

/// One namespace's attestations in Postgres.
pub struct PostgresStore {
    client: Mutex<Client>,
    table: String,
}

fn backend(e: PostgresError) -> StoreError {
    StoreError::Backend(e.sacred_json(None))
}

fn refused(e: postgres::Error) -> StoreError {
    backend(PostgresError::Postgres(e))
}

impl PostgresStore {
    /// Connect, make the namespace's schema and apply its migrations.
    pub fn open(url: &str, ca: &str, namespace: &str) -> Result<Self> {
        let table = format!("{}.attestations", schema(namespace)?);
        let mut client = connect(url, ca)?;
        migrate::migrate(&mut client, namespace)?;
        Ok(Self {
            client: Mutex::new(client),
            table,
        })
    }

    /// The connection. A call that panicked holding it left it mid-statement,
    /// so it is refused rather than handed on.
    fn client(&self) -> StoreResult<MutexGuard<'_, Client>> {
        match self.client.lock() {
            Ok(client) => Ok(client),
            Err(_poisoned) => Err(backend(PostgresError::Poisoned)),
        }
    }

    /// Attestations matching the filter, newest first.
    pub fn query(&self, filter: &QueryFilter) -> StoreResult<Vec<Attestation>> {
        let mut conds: Vec<String> = Vec::new();
        let mut binds: Vec<Box<dyn ToSql + Sync>> = Vec::new();
        let mut bind = |cond: &str, value: Box<dyn ToSql + Sync>| {
            binds.push(value);
            conds.push(cond.replace('?', &format!("${}", binds.len())));
        };
        if let Some(subjects) = &filter.subjects {
            bind("subjects && ?::text[]", Box::new(subjects.clone()));
        }
        if let Some(predicates) = &filter.predicates {
            bind("predicates && ?::text[]", Box::new(predicates.clone()));
        }
        if let Some(contexts) = &filter.contexts {
            bind("contexts && ?::text[]", Box::new(contexts.clone()));
        }
        if let Some(actors) = &filter.actors {
            bind("actors && ?::text[]", Box::new(actors.clone()));
        }
        if let Some(source) = &filter.source {
            bind("source = ?", Box::new(source.clone()));
        }
        if let Some(ts) = filter.time_start {
            bind("timestamp >= ?", Box::new(ts));
        }
        if let Some(te) = filter.time_end {
            bind("timestamp <= ?", Box::new(te));
        }

        let mut sql = format!("SELECT {COLUMNS} FROM {} WHERE TRUE", self.table);
        for cond in &conds {
            sql.push_str(" AND ");
            sql.push_str(cond);
        }
        // limit is a u32 serde already read — inline safely.
        sql.push_str(&format!(" ORDER BY timestamp DESC LIMIT {}", filter.limit));

        let params: Vec<&(dyn ToSql + Sync)> = binds.iter().map(|b| b.as_ref()).collect();
        let rows = self.client()?.query(&sql, &params).map_err(refused)?;
        rows.iter().map(row_to_attestation).collect()
    }

    /// Write a batch the landing file sent, in one transaction. A row the
    /// record already holds is one a send cut short already wrote, so it is
    /// left as it is. The count is the rows of the batch the record now holds.
    pub fn write_batch(&self, attestations: &[Attestation]) -> StoreResult<usize> {
        let mut client = self.client()?;
        let mut tx = client.transaction().map_err(refused)?;
        let sql = format!(
            "INSERT INTO {} ({COLUMNS}) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) ON CONFLICT (id) DO NOTHING",
            self.table
        );
        let statement = tx.prepare(&sql).map_err(refused)?;
        for attestation in attestations {
            let attributes = attributes_json(attestation)?;
            tx.execute(&statement, &insert_params(attestation, &attributes))
                .map_err(refused)?;
        }
        tx.commit().map_err(refused)?;
        Ok(attestations.len())
    }
}

/// Attributes are written as the JSON they are, `{}` for none.
fn attributes_json(attestation: &Attestation) -> StoreResult<String> {
    serde_json::to_string(&attestation.attributes).map_err(|e| backend(PostgresError::Serde(e)))
}

fn insert_params<'a>(a: &'a Attestation, attributes: &'a String) -> [&'a (dyn ToSql + Sync); 11] {
    [
        &a.id,
        &a.subjects,
        &a.predicates,
        &a.contexts,
        &a.actors,
        &a.timestamp,
        &a.source,
        attributes,
        &a.created_at,
        &a.signature,
        &a.signer_did,
    ]
}

fn row_to_attestation(row: &Row) -> StoreResult<Attestation> {
    let attributes: String = row.try_get(7).map_err(refused)?;
    let attributes = serde_json::from_str::<HashMap<String, serde_json::Value>>(&attributes)
        .map_err(|e| backend(PostgresError::Serde(e)))?;
    Ok(Attestation {
        id: row.get(0),
        subjects: row.get(1),
        predicates: row.get(2),
        contexts: row.get(3),
        actors: row.get(4),
        timestamp: row.get(5),
        source: row.get(6),
        attributes,
        created_at: row.get(8),
        signature: row.get(9),
        signer_did: row.get(10),
    })
}

impl AttestationStore for PostgresStore {
    fn put(&mut self, attestation: Attestation) -> StoreResult<()> {
        let attributes = attributes_json(&attestation)?;
        let sql = format!(
            "INSERT INTO {} ({COLUMNS}) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)",
            self.table
        );
        match self
            .client()?
            .execute(&sql, &insert_params(&attestation, &attributes))
        {
            Ok(_) => Ok(()),
            Err(e) if e.code() == Some(&SqlState::UNIQUE_VIOLATION) => {
                Err(StoreError::AlreadyExists(attestation.id.clone()))
            }
            Err(e) => Err(refused(e)),
        }
    }

    fn get(&self, id: &str) -> StoreResult<Option<Attestation>> {
        let sql = format!("SELECT {COLUMNS} FROM {} WHERE id = $1", self.table);
        let row = self.client()?.query_opt(&sql, &[&id]).map_err(refused)?;
        row.as_ref().map(row_to_attestation).transpose()
    }

    fn exists(&self, id: &str) -> StoreResult<bool> {
        let sql = format!("SELECT EXISTS (SELECT 1 FROM {} WHERE id = $1)", self.table);
        let row = self.client()?.query_one(&sql, &[&id]).map_err(refused)?;
        Ok(row.get(0))
    }

    fn delete(&mut self, id: &str) -> StoreResult<bool> {
        let sql = format!("DELETE FROM {} WHERE id = $1", self.table);
        let rows = self.client()?.execute(&sql, &[&id]).map_err(refused)?;
        // id is the primary key, so a delete removes one row or none.
        Ok(rows == 1)
    }

    fn update(&mut self, attestation: Attestation) -> StoreResult<()> {
        let attributes = attributes_json(&attestation)?;
        let sql = format!(
            "UPDATE {} SET subjects = $2, predicates = $3, contexts = $4, actors = $5, timestamp = $6, source = $7, attributes = $8, created_at = $9, signature = $10, signer_did = $11 WHERE id = $1",
            self.table
        );
        let rows = self
            .client()?
            .execute(&sql, &insert_params(&attestation, &attributes))
            .map_err(refused)?;
        // id is the primary key, so an update reaches one row or none.
        if rows == 1 {
            return Ok(());
        }
        Err(StoreError::NotFound(attestation.id.clone()))
    }

    fn ids(&self) -> StoreResult<Vec<String>> {
        let sql = format!("SELECT id FROM {} ORDER BY id", self.table);
        let rows = self.client()?.query(&sql, &[]).map_err(refused)?;
        Ok(rows.iter().map(|row| row.get(0)).collect())
    }

    fn count(&self) -> StoreResult<usize> {
        let sql = format!("SELECT count(*) FROM {}", self.table);
        let count: i64 = self.client()?.query_one(&sql, &[]).map_err(refused)?.get(0);
        usize::try_from(count).map_err(|e| StoreError::InvalidData(e.to_string()))
    }

    fn clear(&mut self) -> StoreResult<()> {
        let sql = format!("DELETE FROM {}", self.table);
        self.client()?.execute(&sql, &[]).map_err(refused)?;
        Ok(())
    }
}
