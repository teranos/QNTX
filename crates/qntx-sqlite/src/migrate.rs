//! Database migration runner
//!
//! Embeds Go's migration SQL files and applies them to SQLite databases.
//! This ensures schema compatibility between Go and Rust implementations.

use rusqlite::Connection;

use crate::error::Result;

/// Migration files embedded from Go's db/sqlite/migrations/
/// Only includes migrations needed for core attestation storage
const MIGRATIONS: &[(&str, &str)] = &[
    (
        "000",
        include_str!("../../../db/sqlite/migrations/000_create_schema_migrations.sql"),
    ),
    (
        "001",
        include_str!("../../../db/sqlite/migrations/001_create_attestations_table.sql"),
    ),
];

/// Apply all pending migrations to the database
///
/// Creates the schema_migrations table if it doesn't exist,
/// then applies any migrations that haven't been applied yet.
///
/// # Errors
///
/// Returns an error if any migration fails to apply.
pub fn migrate(conn: &Connection) -> Result<()> {
    // Enable foreign keys
    conn.execute("PRAGMA foreign_keys = ON", [])?;

    // Apply each migration in order
    for (version, sql) in MIGRATIONS {
        apply_migration(conn, version, sql)?;
    }

    Ok(())
}

/// Apply a single migration if it hasn't been applied yet
fn apply_migration(conn: &Connection, version: &str, sql: &str) -> Result<()> {
    // Check if migration has already been applied
    if is_migration_applied(conn, version)? {
        return Ok(());
    }

    // Apply migration in a transaction
    let tx = conn.unchecked_transaction()?;

    // Execute the migration SQL
    tx.execute_batch(sql)?;

    // Record that migration was applied
    record_migration(&tx, version)?;

    tx.commit()?;

    Ok(())
}

/// Check if a migration has already been applied
fn is_migration_applied(conn: &Connection, version: &str) -> Result<bool> {
    // Check if schema_migrations table exists
    let table_exists: bool = conn
        .prepare("SELECT name FROM sqlite_master WHERE type='table' AND name='schema_migrations'")?
        .exists([])?;

    if !table_exists {
        return Ok(false);
    }

    // Check if this version exists in schema_migrations
    let exists = conn
        .prepare("SELECT 1 FROM schema_migrations WHERE version = ?")?
        .exists([version])?;

    Ok(exists)
}

/// Record that a migration has been applied
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
    fn test_migrate_creates_schema_migrations() {
        let conn = Connection::open_in_memory().unwrap();
        migrate(&conn).unwrap();

        // Verify schema_migrations table exists
        let exists: bool = conn
            .prepare(
                "SELECT name FROM sqlite_master WHERE type='table' AND name='schema_migrations'",
            )
            .unwrap()
            .exists([])
            .unwrap();

        assert!(exists);
    }

    #[test]
    fn test_migrate_creates_attestations_table() {
        let conn = Connection::open_in_memory().unwrap();
        migrate(&conn).unwrap();

        // Verify attestations table exists
        let exists: bool = conn
            .prepare("SELECT name FROM sqlite_master WHERE type='table' AND name='attestations'")
            .unwrap()
            .exists([])
            .unwrap();

        assert!(exists);
    }

    #[test]
    fn test_migrate_is_idempotent() {
        let conn = Connection::open_in_memory().unwrap();

        // Run migrations twice
        migrate(&conn).unwrap();
        migrate(&conn).unwrap();

        // Should not fail
    }

    #[test]
    fn test_migration_records_in_schema_migrations() {
        let conn = Connection::open_in_memory().unwrap();
        migrate(&conn).unwrap();

        // Check that both migrations were recorded
        let count: i64 = conn
            .query_row("SELECT COUNT(*) FROM schema_migrations", [], |row| {
                row.get(0)
            })
            .unwrap();

        assert_eq!(count, 2); // 000 and 001
    }
}
