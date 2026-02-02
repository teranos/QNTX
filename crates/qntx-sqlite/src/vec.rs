//! SQLite vector extension integration
//!
//! This module handles sqlite-vec initialization for vector similarity search.

use rusqlite::Connection;

/// Load sqlite-vec extension into a connection
///
/// This must be called for each connection that needs vector support.
/// The extension provides:
/// - vec0 virtual table for vector storage
/// - vec_distance_* functions for similarity search
/// - FLOAT32_BLOB type for efficient vector storage
pub fn load_vec_extension(conn: &Connection) -> Result<(), rusqlite::Error> {
    // Load the sqlite-vec extension
    // Note: sqlite-vec is statically linked, so we just need to call the init function
    unsafe {
        let rc =
            sqlite_vec::sqlite3_vec_init(conn.handle(), std::ptr::null_mut(), std::ptr::null());

        if rc != rusqlite::ffi::SQLITE_OK {
            return Err(rusqlite::Error::SqliteFailure(
                rusqlite::ffi::Error::new(rc),
                Some("Failed to initialize sqlite-vec extension".to_string()),
            ));
        }
    }

    Ok(())
}
