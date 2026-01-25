//! SQLite storage backend implementing AttestationStore trait

use rusqlite::{Connection, OptionalExtension};
use qntx_core::{
    attestation::Attestation,
    storage::{AttestationStore, StoreError},
};

type StoreResult<T> = Result<T, StoreError>;

use crate::json::{
    deserialize_attributes, deserialize_string_vec, serialize_attributes, serialize_string_vec,
    sql_to_timestamp, timestamp_to_sql,
};

/// SQLite-backed attestation store
pub struct SqliteStore {
    conn: Connection,
}

impl SqliteStore {
    /// Create a new SQLite store from a connection
    ///
    /// The connection should already have migrations applied.
    /// Use [`crate::migrate::migrate`] to initialize a fresh database.
    pub fn new(conn: Connection) -> Self {
        Self { conn }
    }

    /// Create a new in-memory SQLite store (for testing)
    pub fn in_memory() -> crate::error::Result<Self> {
        let conn = Connection::open_in_memory()?;
        crate::migrate::migrate(&conn)?;
        Ok(Self::new(conn))
    }

    /// Create a new file-backed SQLite store
    pub fn open(path: impl AsRef<std::path::Path>) -> crate::error::Result<Self> {
        let conn = Connection::open(path)?;
        conn.execute("PRAGMA foreign_keys = ON", [])?;
        crate::migrate::migrate(&conn)?;
        Ok(Self::new(conn))
    }

    /// Get a reference to the underlying connection
    pub fn connection(&self) -> &Connection {
        &self.conn
    }
}

impl AttestationStore for SqliteStore {
    fn put(&mut self, attestation: Attestation) -> StoreResult<()> {
        // Check if attestation already exists
        if self.exists(&attestation.id)? {
            return Err(StoreError::AlreadyExists(attestation.id.clone()));
        }

        // Serialize JSON fields
        let subjects_json = serialize_string_vec(&attestation.subjects)
            .map_err(|e| StoreError::Serialization(e.to_string()))?;
        let predicates_json = serialize_string_vec(&attestation.predicates)
            .map_err(|e| StoreError::Serialization(e.to_string()))?;
        let contexts_json = serialize_string_vec(&attestation.contexts)
            .map_err(|e| StoreError::Serialization(e.to_string()))?;
        let actors_json = serialize_string_vec(&attestation.actors)
            .map_err(|e| StoreError::Serialization(e.to_string()))?;
        let attributes_json = serialize_attributes(&attestation.attributes)
            .map_err(|e| StoreError::Serialization(e.to_string()))?;

        // Convert timestamp to SQL format
        let timestamp_sql = timestamp_to_sql(attestation.timestamp);
        let created_at_sql = timestamp_to_sql(attestation.created_at);

        // Insert into database
        self.conn.execute(
            "INSERT INTO attestations (id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at)
             VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
            rusqlite::params![
                attestation.id,
                subjects_json,
                predicates_json,
                contexts_json,
                actors_json,
                timestamp_sql,
                attestation.source,
                attributes_json,
                created_at_sql,
            ],
        ).map_err(|e| StoreError::Backend(e.to_string()))?;

        Ok(())
    }

    fn get(&self, id: &str) -> StoreResult<Option<Attestation>> {
        let mut stmt = self
            .conn
            .prepare(
                "SELECT id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at
                 FROM attestations
                 WHERE id = ?",
            )
            .map_err(|e| StoreError::Backend(e.to_string()))?;

        let result: Option<(String, String, String, String, String, String, String, Option<String>, String)> = stmt
            .query_row([id], |row| {
                Ok((
                    row.get::<_, String>(0)?,
                    row.get::<_, String>(1)?,
                    row.get::<_, String>(2)?,
                    row.get::<_, String>(3)?,
                    row.get::<_, String>(4)?,
                    row.get::<_, String>(5)?,
                    row.get::<_, String>(6)?,
                    row.get::<_, Option<String>>(7)?,
                    row.get::<_, String>(8)?,
                ))
            })
            .optional()
            .map_err(|e: rusqlite::Error| StoreError::Backend(e.to_string()))?;

        match result {
            None => Ok(None),
            Some(row_data) => {
                // Deserialize JSON fields
                let subjects = deserialize_string_vec(&row_data.1)
                    .map_err(|e| StoreError::Serialization(e.to_string()))?;
                let predicates = deserialize_string_vec(&row_data.2)
                    .map_err(|e| StoreError::Serialization(e.to_string()))?;
                let contexts = deserialize_string_vec(&row_data.3)
                    .map_err(|e| StoreError::Serialization(e.to_string()))?;
                let actors = deserialize_string_vec(&row_data.4)
                    .map_err(|e| StoreError::Serialization(e.to_string()))?;
                let attributes = deserialize_attributes(row_data.7.clone())
                    .map_err(|e| StoreError::Serialization(e.to_string()))?;

                // Convert timestamps from SQL format
                let timestamp = sql_to_timestamp(&row_data.5)
                    .map_err(|e| StoreError::Serialization(e.to_string()))?;
                let created_at = sql_to_timestamp(&row_data.8)
                    .map_err(|e| StoreError::Serialization(e.to_string()))?;

                Ok(Some(Attestation {
                    id: row_data.0,
                    subjects,
                    predicates,
                    contexts,
                    actors,
                    timestamp,
                    source: row_data.6,
                    attributes,
                    created_at,
                }))
            }
        }
    }

    fn delete(&mut self, id: &str) -> StoreResult<bool> {
        let rows_affected = self
            .conn
            .execute("DELETE FROM attestations WHERE id = ?", [id])
            .map_err(|e| StoreError::Backend(e.to_string()))?;

        Ok(rows_affected > 0)
    }

    fn update(&mut self, attestation: Attestation) -> StoreResult<()> {
        // Check if attestation exists
        if !self.exists(&attestation.id)? {
            return Err(StoreError::NotFound(attestation.id.clone()));
        }

        // Serialize JSON fields
        let subjects_json = serialize_string_vec(&attestation.subjects)
            .map_err(|e| StoreError::Serialization(e.to_string()))?;
        let predicates_json = serialize_string_vec(&attestation.predicates)
            .map_err(|e| StoreError::Serialization(e.to_string()))?;
        let contexts_json = serialize_string_vec(&attestation.contexts)
            .map_err(|e| StoreError::Serialization(e.to_string()))?;
        let actors_json = serialize_string_vec(&attestation.actors)
            .map_err(|e| StoreError::Serialization(e.to_string()))?;
        let attributes_json = serialize_attributes(&attestation.attributes)
            .map_err(|e| StoreError::Serialization(e.to_string()))?;

        // Convert timestamp to SQL format
        let timestamp_sql = timestamp_to_sql(attestation.timestamp);

        // Update in database (don't update created_at)
        self.conn.execute(
            "UPDATE attestations
             SET subjects = ?, predicates = ?, contexts = ?, actors = ?,
                 timestamp = ?, source = ?, attributes = ?
             WHERE id = ?",
            rusqlite::params![
                subjects_json,
                predicates_json,
                contexts_json,
                actors_json,
                timestamp_sql,
                attestation.source,
                attributes_json,
                attestation.id,
            ],
        ).map_err(|e| StoreError::Backend(e.to_string()))?;

        Ok(())
    }

    fn ids(&self) -> StoreResult<Vec<String>> {
        let mut stmt = self
            .conn
            .prepare("SELECT id FROM attestations ORDER BY created_at DESC")
            .map_err(|e| StoreError::Backend(e.to_string()))?;

        let ids = stmt
            .query_map([], |row| row.get::<_, String>(0))
            .map_err(|e| StoreError::Backend(e.to_string()))?
            .collect::<Result<Vec<String>, rusqlite::Error>>()
            .map_err(|e| StoreError::Backend(e.to_string()))?;

        Ok(ids)
    }

    fn clear(&mut self) -> StoreResult<()> {
        self.conn
            .execute("DELETE FROM attestations", [])
            .map_err(|e| StoreError::Backend(e.to_string()))?;

        Ok(())
    }
}
