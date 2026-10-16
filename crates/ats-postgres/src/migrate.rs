//! Migration runner for the Postgres backend.
//!
//! Same shape as `ats_sqlite::migrate` and `ats_duckdb::migrate`: the SQL files
//! are embedded at compile time, each applies once, and the applied versions
//! are recorded in `schema_migrations`. Application code never issues DDL.
//!
//! A namespace is a schema. Each migration runs in one transaction with the
//! namespace's schema as its search path, so it lands there and nowhere else.

use postgres::Client;

use crate::error::{PostgresError, Result};

include!(concat!(env!("OUT_DIR"), "/migrations.rs"));

/// Make the namespace's schema and apply every pending migration in it.
pub fn migrate(client: &mut Client, namespace: &str) -> Result<()> {
    let schema = crate::schema(namespace)?;
    client.batch_execute(&format!("CREATE SCHEMA IF NOT EXISTS {schema}"))?;
    for (version, sql) in MIGRATIONS {
        apply(client, namespace, &schema, version, sql)?;
    }
    Ok(())
}

fn apply(
    client: &mut Client,
    namespace: &str,
    schema: &str,
    version: &str,
    sql: &str,
) -> Result<()> {
    let failed = |source| PostgresError::Migrate {
        version: version.to_string(),
        source,
    };
    let mut tx = client.transaction().map_err(failed)?;
    tx.batch_execute(&format!("SET LOCAL search_path TO {schema}"))
        .map_err(failed)?;

    let recorded: bool = tx
        .query_one(
            "SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = $1 AND table_name = 'schema_migrations')",
            &[&namespace],
        )
        .map_err(failed)?
        .get(0);
    if recorded {
        let applied: bool = tx
            .query_one(
                "SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)",
                &[&version],
            )
            .map_err(failed)?
            .get(0);
        if applied {
            return tx.commit().map_err(failed);
        }
    }

    let start = std::time::Instant::now();
    eprintln!("ats-postgres: applying migration {version} in {namespace}");
    tx.batch_execute(sql).map_err(failed)?;
    tx.execute(
        "INSERT INTO schema_migrations (version) VALUES ($1)",
        &[&version],
    )
    .map_err(failed)?;
    tx.commit().map_err(failed)?;
    eprintln!(
        "ats-postgres: migration {version} applied in {:.1}s",
        start.elapsed().as_secs_f64()
    );
    Ok(())
}
