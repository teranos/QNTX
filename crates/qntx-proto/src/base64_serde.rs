//! Base64 serde for Vec<u8> fields.
//!
//! Go's encoding/json serializes []byte as base64 strings. Rust's serde
//! default for Vec<u8> is an integer array. This module bridges that gap
//! so proto types can be serialized/deserialized across the Go↔Rust FFI
//! boundary using JSON.

use serde::{Deserialize, Deserializer, Serializer};

use base64::engine::general_purpose::STANDARD;
use base64::Engine;

/// Serialize Vec<u8> as a base64 string (matching Go's []byte JSON encoding).
pub fn serialize<S>(bytes: &Vec<u8>, serializer: S) -> Result<S::Ok, S::Error>
where
    S: Serializer,
{
    if bytes.is_empty() {
        // Match Go's omitempty behavior — serialize empty as empty string
        serializer.serialize_str("")
    } else {
        serializer.serialize_str(&STANDARD.encode(bytes))
    }
}

/// Deserialize a base64 string into Vec<u8>.
pub fn deserialize<'de, D>(deserializer: D) -> Result<Vec<u8>, D::Error>
where
    D: Deserializer<'de>,
{
    let s: String = String::deserialize(deserializer)?;
    if s.is_empty() {
        return Ok(Vec::new());
    }
    STANDARD.decode(&s).map_err(serde::de::Error::custom)
}
