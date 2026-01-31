//! JSON serialization helpers for SQLite storage
//!
//! Handles conversion between Rust types and SQLite JSON columns,
//! matching the format used by the Go implementation.

use serde_json::Value;
use std::collections::HashMap;

use crate::error::Result;

/// Serialize a Vec<String> to JSON string for SQLite storage
pub fn serialize_string_vec(vec: &[String]) -> Result<String> {
    Ok(serde_json::to_string(vec)?)
}

/// Deserialize a JSON string from SQLite to Vec<String>
pub fn deserialize_string_vec(json: &str) -> Result<Vec<String>> {
    Ok(serde_json::from_str(json)?)
}

/// Serialize attributes HashMap to JSON string for SQLite storage
pub fn serialize_attributes(attrs: &HashMap<String, Value>) -> Result<Option<String>> {
    if attrs.is_empty() {
        Ok(None)
    } else {
        Ok(Some(serde_json::to_string(attrs)?))
    }
}

/// Deserialize attributes from SQLite JSON string to HashMap
pub fn deserialize_attributes(json: Option<String>) -> Result<HashMap<String, Value>> {
    match json {
        Some(json_str) => Ok(serde_json::from_str(&json_str)?),
        None => Ok(HashMap::new()),
    }
}

/// Convert Unix timestamp milliseconds to SQLite DATETIME string (RFC3339)
pub fn timestamp_to_sql(timestamp_ms: i64) -> String {
    // Convert milliseconds to seconds + nanoseconds
    let secs = timestamp_ms / 1000;
    let nanos = ((timestamp_ms % 1000) * 1_000_000) as u32;

    // Create DateTime and format as RFC3339
    match chrono::DateTime::from_timestamp(secs, nanos) {
        Some(dt) => dt.to_rfc3339(),
        None => chrono::Utc::now().to_rfc3339(), // Fallback to now if invalid
    }
}

/// Convert SQLite DATETIME string (RFC3339) to Unix timestamp milliseconds
pub fn sql_to_timestamp(datetime_str: &str) -> Result<i64> {
    use chrono::DateTime;

    let dt = DateTime::parse_from_rfc3339(datetime_str)
        .map_err(|e| crate::error::SqliteError::Migration(format!("Invalid datetime: {}", e)))?;

    Ok(dt.timestamp_millis())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_serialize_string_vec() {
        let vec = vec!["ALICE".to_string(), "BOB".to_string()];
        let json = serialize_string_vec(&vec).unwrap();
        assert_eq!(json, r#"["ALICE","BOB"]"#);
    }

    #[test]
    fn test_deserialize_string_vec() {
        let json = r#"["ALICE","BOB"]"#;
        let vec = deserialize_string_vec(json).unwrap();
        assert_eq!(vec, vec!["ALICE", "BOB"]);
    }

    #[test]
    fn test_serialize_empty_attributes() {
        let attrs = HashMap::new();
        let json = serialize_attributes(&attrs).unwrap();
        assert_eq!(json, None);
    }

    #[test]
    fn test_serialize_attributes() {
        let mut attrs = HashMap::new();
        attrs.insert("key".to_string(), serde_json::json!("value"));
        let json = serialize_attributes(&attrs).unwrap().unwrap();
        assert!(json.contains("key"));
        assert!(json.contains("value"));
    }

    #[test]
    fn test_deserialize_none_attributes() {
        let attrs = deserialize_attributes(None).unwrap();
        assert!(attrs.is_empty());
    }

    #[test]
    fn test_timestamp_roundtrip() {
        let original = 1704067200000; // 2024-01-01 00:00:00 UTC
        let sql_str = timestamp_to_sql(original);
        let restored = sql_to_timestamp(&sql_str).unwrap();
        assert_eq!(original, restored);
    }
}
