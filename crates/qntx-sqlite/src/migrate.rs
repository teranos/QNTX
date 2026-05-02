//! Database migration runner
//!
//! Embeds Go's migration SQL files and applies them to SQLite databases.
//! This ensures schema compatibility between Go and Rust implementations.

use rusqlite::Connection;

use crate::error::Result;

/// All migration files embedded from Go's db/sqlite/migrations/.
/// Rust owns the full schema — Go routes SQL through Rust's connection.
const MIGRATIONS: &[(&str, &str)] = &[
    (
        "000",
        include_str!("../../../db/sqlite/migrations/000_create_schema_migrations.sql"),
    ),
    (
        "001",
        include_str!("../../../db/sqlite/migrations/001_create_attestations_table.sql"),
    ),
    (
        "002",
        include_str!("../../../db/sqlite/migrations/002_create_async_ix_jobs_table.sql"),
    ),
    (
        "003",
        include_str!("../../../db/sqlite/migrations/003_create_scheduled_pulse_jobs_table.sql"),
    ),
    (
        "005",
        include_str!("../../../db/sqlite/migrations/005_daemon_config.sql"),
    ),
    (
        "006",
        include_str!("../../../db/sqlite/migrations/006_create_job_checkpoints_table.sql"),
    ),
    (
        "007",
        include_str!("../../../db/sqlite/migrations/007_create_pulse_executions_table.sql"),
    ),
    (
        "008",
        include_str!("../../../db/sqlite/migrations/008_create_task_logs_table.sql"),
    ),
    (
        "009",
        include_str!("../../../db/sqlite/migrations/009_create_aliases_table.sql"),
    ),
    (
        "010",
        include_str!("../../../db/sqlite/migrations/010_create_storage_events_table.sql"),
    ),
    (
        "011",
        include_str!("../../../db/sqlite/migrations/011_create_ai_model_usage_table.sql"),
    ),
    (
        "012",
        include_str!("../../../db/sqlite/migrations/012_aliases_collate_nocase.sql"),
    ),
    (
        "013",
        include_str!("../../../db/sqlite/migrations/013_add_limit_value_to_storage_events.sql"),
    ),
    (
        "014",
        include_str!(
            "../../../db/sqlite/migrations/014_add_eviction_details_to_storage_events.sql"
        ),
    ),
    (
        "015",
        include_str!("../../../db/sqlite/migrations/015_add_error_details_to_async_jobs.sql"),
    ),
    (
        "017",
        include_str!("../../../db/sqlite/migrations/017_create_watchers_table.sql"),
    ),
    (
        "018",
        include_str!("../../../db/sqlite/migrations/018_add_ax_query_to_watchers.sql"),
    ),
    (
        "019",
        include_str!("../../../db/sqlite/migrations/019_create_canvas_state_tables.sql"),
    ),
    (
        "020",
        include_str!("../../../db/sqlite/migrations/020_multi_glyph_compositions.sql"),
    ),
    (
        "021",
        include_str!("../../../db/sqlite/migrations/021_dag_composition_edges.sql"),
    ),
    (
        "022",
        include_str!("../../../db/sqlite/migrations/022_add_content_to_canvas_glyphs.sql"),
    ),
    (
        "023",
        include_str!("../../../db/sqlite/migrations/023_composition_edge_cursors.sql"),
    ),
    // Optional migrations (sqlite-vec dependent) — Rust has sqlite-vec loaded
    (
        "024",
        include_str!("../../../db/sqlite/migrations/024_optional_create_embeddings_table.sql"),
    ),
    (
        "025",
        include_str!("../../../db/sqlite/migrations/025_add_canvas_id_to_canvas_glyphs.sql"),
    ),
    (
        "026",
        include_str!("../../../db/sqlite/migrations/026_add_semantic_query_to_watchers.sql"),
    ),
    (
        "029",
        include_str!(
            "../../../db/sqlite/migrations/029_optional_add_cluster_columns_to_embeddings.sql"
        ),
    ),
    (
        "030",
        include_str!("../../../db/sqlite/migrations/030_optional_create_cluster_centroids.sql"),
    ),
    (
        "031",
        include_str!("../../../db/sqlite/migrations/031_optional_add_cluster_id_to_watchers.sql"),
    ),
    (
        "032",
        include_str!(
            "../../../db/sqlite/migrations/032_optional_add_projection_columns_to_embeddings.sql"
        ),
    ),
    (
        "033",
        include_str!("../../../db/sqlite/migrations/033_add_upstream_semantic_to_watchers.sql"),
    ),
    (
        "034",
        include_str!("../../../db/sqlite/migrations/034_optional_create_embedding_projections.sql"),
    ),
    (
        "035",
        include_str!("../../../db/sqlite/migrations/035_optional_create_cluster_tracking.sql"),
    ),
    (
        "036",
        include_str!("../../../db/sqlite/migrations/036_optional_add_cluster_labeled_at.sql"),
    ),
    (
        "037",
        include_str!("../../../db/sqlite/migrations/037_create_minimized_windows.sql"),
    ),
    (
        "038",
        include_str!("../../../db/sqlite/migrations/038_create_webauthn_credentials.sql"),
    ),
    (
        "039",
        include_str!("../../../db/sqlite/migrations/039_create_node_identity.sql"),
    ),
    (
        "041",
        include_str!("../../../db/sqlite/migrations/041_add_plugin_name_to_canvas_glyphs.sql"),
    ),
    (
        "042",
        include_str!("../../../db/sqlite/migrations/042_add_plugin_version_to_async_jobs.sql"),
    ),
    (
        "043",
        include_str!("../../../db/sqlite/migrations/043_create_watcher_execution_queue.sql"),
    ),
    (
        "044",
        include_str!(
            "../../../db/sqlite/migrations/044_rename_max_fires_per_minute_to_per_second.sql"
        ),
    ),
    (
        "045",
        include_str!("../../../db/sqlite/migrations/045_add_attribute_filters_to_watchers.sql"),
    ),
    (
        "046",
        include_str!(
            "../../../db/sqlite/migrations/046_optional_add_z_to_embedding_projections.sql"
        ),
    ),
    (
        "047",
        include_str!("../../../db/sqlite/migrations/047_relax_composition_edge_fk.sql"),
    ),
    (
        "048",
        include_str!("../../../db/sqlite/migrations/048_create_attestation_junction_tables.sql"),
    ),
];

/// Versions whose migrations are allowed to fail (they depend on sqlite-vec).
/// Matches Go's logic: filenames containing "optional" are skipped on error.
const OPTIONAL_VERSIONS: &[&str] = &[
    "024", "029", "030", "031", "032", "034", "035", "036", "046",
];

/// Apply all pending migrations to the database
///
/// Creates the schema_migrations table if it doesn't exist,
/// then applies any migrations that haven't been applied yet.
/// Optional migrations (sqlite-vec dependent) are silently skipped on failure.
///
/// # Errors
///
/// Returns an error if any mandatory migration fails to apply.
pub fn migrate(conn: &Connection) -> Result<()> {
    // Enable foreign keys
    conn.execute("PRAGMA foreign_keys = ON", [])?;

    // Apply each migration in order
    for (version, sql) in MIGRATIONS {
        let optional = OPTIONAL_VERSIONS.contains(version);
        if let Err(e) = apply_migration(conn, version, sql) {
            if optional {
                // Silently skip — matches Go's "optional" migration behavior
                continue;
            }
            return Err(e);
        }
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

        // Count applied migrations (optional ones may have been skipped)
        let count: i64 = conn
            .query_row("SELECT COUNT(*) FROM schema_migrations", [], |row| {
                row.get(0)
            })
            .unwrap();

        let mandatory_count = MIGRATIONS.len() - OPTIONAL_VERSIONS.len();
        // At least all mandatory migrations must be applied
        assert!(count >= mandatory_count as i64);
    }
}
