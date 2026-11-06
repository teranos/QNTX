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

/// Deserialize a JSON string from SQLite to Vec<String>.
/// Handles JSON "null" and empty strings as empty vec.
pub fn deserialize_string_vec(json: &str) -> Result<Vec<String>> {
    if json == "null" || json.is_empty() {
        return Ok(Vec::new());
    }
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

/// Deserialize attributes from SQLite JSON string to HashMap.
/// Handles SQL NULL, JSON "null", and empty strings as empty attributes.
pub fn deserialize_attributes(json: Option<String>) -> Result<HashMap<String, Value>> {
    match json {
        Some(json_str) if json_str != "null" && !json_str.is_empty() => {
            Ok(serde_json::from_str(&json_str)?)
        }
        _ => Ok(HashMap::new()),
    }
}

/// Unix timestamp milliseconds as a SQLite DATETIME string (RFC3339), or `None`
/// when the instant is outside the dates `chrono` holds.
pub fn try_timestamp_to_sql(timestamp_ms: i64) -> Option<String> {
    // Floored, so -1500 ms is -2 s + 500 ms, not -1 s and a negative remainder
    // that wraps as u32 and is no instant.
    let secs = timestamp_ms.div_euclid(1000);
    let nanos = (timestamp_ms.rem_euclid(1000) * 1_000_000) as u32;
    chrono::DateTime::from_timestamp(secs, nanos).map(|dt| dt.to_rfc3339())
}

/// [`try_timestamp_to_sql`], and "now" for an instant outside the dates held.
///
/// Only for a filter's bound, which names a window and writes nothing. What is
/// written asks [`try_timestamp_to_sql`]: a date that cannot be stored is
/// refused, not stored as another.
pub fn timestamp_to_sql(timestamp_ms: i64) -> String {
    try_timestamp_to_sql(timestamp_ms).unwrap_or_else(|| chrono::Utc::now().to_rfc3339())
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
    fn a_negative_timestamp_with_a_sub_second_keeps_its_instant() {
        // -1500 ms used to wrap its negative remainder as u32 and come back as now.
        for ms in [-1500_i64, -1, -1000, -86_400_001, 0, 1500] {
            let back = sql_to_timestamp(&timestamp_to_sql(ms)).unwrap();
            assert_eq!(back, ms, "{ms} ms did not survive the round trip");
        }
    }

    #[test]
    fn an_instant_outside_the_dates_held_is_none_not_now() {
        assert_eq!(try_timestamp_to_sql(i64::MAX), None);
        assert_eq!(try_timestamp_to_sql(i64::MIN), None);
    }

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
    fn test_deserialize_string_vec_null() {
        let vec = deserialize_string_vec("null").unwrap();
        assert!(vec.is_empty());
    }

    #[test]
    fn test_deserialize_string_vec_empty() {
        let vec = deserialize_string_vec("").unwrap();
        assert!(vec.is_empty());
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
