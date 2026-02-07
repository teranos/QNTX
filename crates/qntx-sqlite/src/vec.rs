//! SQLite vector extension integration
//!
//! This module handles sqlite-vec initialization for vector similarity search.

use std::sync::Once;

// Ensure sqlite-vec is initialized only once globally
static INIT: Once = Once::new();

/// Initialize sqlite-vec extension globally
///
/// This MUST be called once at program startup BEFORE opening any connections.
/// The sqlite-vec extension uses auto-extension to register itself with SQLite.
pub fn init_vec_extension() {
    INIT.call_once(|| {
        unsafe {
            // Register sqlite-vec as an auto-extension so it loads for all connections
            // This must happen before any connections are created
            rusqlite::ffi::sqlite3_auto_extension(Some(std::mem::transmute(
                sqlite_vec::sqlite3_vec_init as *const (),
            )));
        }
    });
}

#[cfg(test)]
mod tests {
    use super::*;
    use rusqlite::Connection;

    #[test]
    fn test_vec_extension_loads() -> Result<(), rusqlite::Error> {
        // Initialize the extension BEFORE creating any connections
        init_vec_extension();

        // Create in-memory database - extension will be auto-loaded
        let conn = Connection::open_in_memory()?;

        // Test that vec_version() function exists
        let version: String = conn.query_row("SELECT vec_version()", [], |row| row.get(0))?;
        println!("sqlite-vec version: {}", version);
        assert!(!version.is_empty());

        Ok(())
    }

    #[test]
    fn test_vec0_table_creation() -> Result<(), rusqlite::Error> {
        init_vec_extension();
        let conn = Connection::open_in_memory()?;

        // Create a vec0 virtual table
        conn.execute(
            "CREATE VIRTUAL TABLE test_vecs USING vec0(
                id TEXT PRIMARY KEY,
                embedding FLOAT32[384]
            )",
            [],
        )?;

        // Verify table was created
        let count: i32 = conn.query_row(
            "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='test_vecs'",
            [],
            |row| row.get(0),
        )?;
        assert_eq!(count, 1);

        Ok(())
    }

    #[test]
    fn test_float32_blob_type() -> Result<(), rusqlite::Error> {
        init_vec_extension();
        let conn = Connection::open_in_memory()?;

        // Create table with FLOAT32_BLOB type
        conn.execute(
            "CREATE TABLE test_embeddings (
                id INTEGER PRIMARY KEY,
                vec FLOAT32_BLOB(384)
            )",
            [],
        )?;

        // Insert a test vector (all zeros for now)
        let zeros = vec![0.0f32; 384];
        let blob = zeros
            .iter()
            .flat_map(|f| f.to_le_bytes())
            .collect::<Vec<u8>>();

        conn.execute(
            "INSERT INTO test_embeddings (id, vec) VALUES (?, ?)",
            rusqlite::params![1, blob],
        )?;

        // Verify insertion
        let count: i32 = conn.query_row(
            "SELECT COUNT(*) FROM test_embeddings",
            [],
            |row| row.get(0),
        )?;
        assert_eq!(count, 1);

        Ok(())
    }

    #[test]
    fn test_vector_distance_functions() -> Result<(), rusqlite::Error> {
        init_vec_extension();
        let conn = Connection::open_in_memory()?;

        // Create vectors
        let v1 = vec![1.0f32, 0.0, 0.0];
        let v2 = vec![0.0f32, 1.0, 0.0];

        let blob1 = v1.iter().flat_map(|f| f.to_le_bytes()).collect::<Vec<u8>>();
        let blob2 = v2.iter().flat_map(|f| f.to_le_bytes()).collect::<Vec<u8>>();

        // Calculate L2 distance (should be sqrt(2) ≈ 1.414)
        let distance: f32 = conn.query_row(
            "SELECT vec_distance_l2(?, ?)",
            rusqlite::params![blob1, blob2],
            |row| row.get(0),
        )?;

        println!("L2 distance: {}", distance);
        assert!((distance - 1.414).abs() < 0.01);

        Ok(())
    }

    #[test]
    fn test_migration_with_vectors() -> Result<(), rusqlite::Error> {
        init_vec_extension();
        let conn = Connection::open_in_memory()?;

        // Run the embeddings migration
        let migration_sql = include_str!("../../../db/sqlite/migrations/019_create_embeddings_table.sql");
        conn.execute_batch(migration_sql)?;

        // Verify tables were created
        let embeddings_count: i32 = conn.query_row(
            "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='embeddings'",
            [],
            |row| row.get(0),
        )?;
        assert_eq!(embeddings_count, 1);

        let vec_count: i32 = conn.query_row(
            "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='vec_embeddings'",
            [],
            |row| row.get(0),
        )?;
        assert_eq!(vec_count, 1);

        Ok(())
    }
}
