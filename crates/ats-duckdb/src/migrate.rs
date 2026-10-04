//! Migration runner for the DuckDB backend.
//!
//! Same shape as `ats_sqlite::migrate`: embed SQL files at compile time,
//! apply each once, record applied versions in a `schema_migrations` table.
//! Application code never issues DDL directly.

use duckdb::Connection;

use crate::error::Result;

const MIGRATIONS: &[(&str, &str)] = &[
    (
        "000",
        include_str!("../../../db/duckdb/migrations/000_create_schema_migrations.sql"),
    ),
    (
        "001",
        include_str!("../../../db/duckdb/migrations/001_create_attestations_table.sql"),
    ),
];

/// Apply all pending migrations to the DuckDB connection.
/// Creates `schema_migrations` if missing, then applies each migration once.
pub fn migrate(conn: &Connection) -> Result<()> {
    for (version, sql) in MIGRATIONS {
        apply_migration(conn, version, sql)?;
    }
    Ok(())
}

/// The tables the migrations leave standing, applied by this runner in the
/// DuckDB this crate links. make parity reads its IN THE RECORD column from
/// here rather than replaying the migrations in another engine.
pub fn schema_tables() -> Result<Vec<String>> {
    let conn = Connection::open_in_memory()?;
    crate::assert_library_version(&conn)?;
    migrate(&conn)?;
    let mut statement = conn.prepare(
        "SELECT table_name FROM information_schema.tables WHERE table_type = 'BASE TABLE' ORDER BY table_name",
    )?;
    let tables = statement
        .query_map([], |row| row.get::<_, String>(0))?
        .collect::<std::result::Result<Vec<_>, _>>()?;
    Ok(tables)
}

fn apply_migration(conn: &Connection, version: &str, sql: &str) -> Result<()> {
    if is_migration_applied(conn, version)? {
        return Ok(());
    }

    let start = std::time::Instant::now();
    eprintln!("ats-duckdb: applying migration {}", version);

    conn.execute_batch(sql)?;
    record_migration(conn, version)?;

    eprintln!(
        "ats-duckdb: migration {} applied in {:.1}s",
        version,
        start.elapsed().as_secs_f64()
    );
    Ok(())
}

fn is_migration_applied(conn: &Connection, version: &str) -> Result<bool> {
    // information_schema.tables is available in DuckDB
    let table_exists: bool = conn
        .prepare("SELECT 1 FROM information_schema.tables WHERE table_name = 'schema_migrations'")?
        .exists([])?;

    if !table_exists {
        return Ok(false);
    }

    let exists = conn
        .prepare("SELECT 1 FROM schema_migrations WHERE version = ?")?
        .exists([version])?;

    Ok(exists)
}

fn record_migration(conn: &Connection, version: &str) -> Result<()> {
    conn.execute(
        "INSERT INTO schema_migrations (version, applied_at) VALUES (?, CURRENT_TIMESTAMP)",
        [version],
    )?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn schema_tables_are_what_the_migrations_leave() {
        let tables = schema_tables().unwrap();
        assert!(tables.contains(&"attestations".to_string()), "{tables:?}");
        assert!(
            tables.contains(&"schema_migrations".to_string()),
            "{tables:?}"
        );
    }

    #[test]
    fn migrate_creates_schema_migrations() {
        let conn = Connection::open_in_memory().unwrap();
        migrate(&conn).unwrap();
        let exists: bool = conn
            .prepare(
                "SELECT 1 FROM information_schema.tables WHERE table_name = 'schema_migrations'",
            )
            .unwrap()
            .exists([])
            .unwrap();
        assert!(exists);
    }

    #[test]
    fn migrate_creates_attestations_table() {
        let conn = Connection::open_in_memory().unwrap();
        migrate(&conn).unwrap();
        let exists: bool = conn
            .prepare("SELECT 1 FROM information_schema.tables WHERE table_name = 'attestations'")
            .unwrap()
            .exists([])
            .unwrap();
        assert!(exists);
    }

    #[test]
    fn migrate_is_idempotent() {
        let conn = Connection::open_in_memory().unwrap();
        migrate(&conn).unwrap();
        migrate(&conn).unwrap();
    }
}
