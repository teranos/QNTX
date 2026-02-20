//! Custom serde for prost_types::Struct ↔ JSON object.
//!
//! prost_types::Struct doesn't implement Serialize/Deserialize.
//! This module provides serialization functions that convert between
//! prost_types::Struct and plain JSON objects, used via #[serde(with)].

use prost_types::value::Kind;
use serde::{Deserialize, Deserializer, Serialize, Serializer};
use std::collections::HashMap;

/// Serialize Option<prost_types::Struct> as a JSON object (or skip if None).
pub fn serialize_option_struct<S>(
    value: &Option<prost_types::Struct>,
    serializer: S,
) -> Result<S::Ok, S::Error>
where
    S: Serializer,
{
    match value {
        Some(s) => {
            let map = struct_to_json_map(s);
            map.serialize(serializer)
        }
        None => serializer.serialize_none(),
    }
}

/// Deserialize a JSON object into Option<prost_types::Struct>.
pub fn deserialize_option_struct<'de, D>(
    deserializer: D,
) -> Result<Option<prost_types::Struct>, D::Error>
where
    D: Deserializer<'de>,
{
    let opt: Option<HashMap<String, serde_json::Value>> = Option::deserialize(deserializer)?;
    Ok(opt.map(|m| json_map_to_struct(&m)))
}

/// Convert prost_types::Struct → HashMap<String, serde_json::Value>
pub fn struct_to_json_map(s: &prost_types::Struct) -> HashMap<String, serde_json::Value> {
    s.fields
        .iter()
        .map(|(k, v)| (k.clone(), prost_value_to_json(v)))
        .collect()
}

/// Convert HashMap<String, serde_json::Value> → prost_types::Struct
pub fn json_map_to_struct(m: &HashMap<String, serde_json::Value>) -> prost_types::Struct {
    prost_types::Struct {
        fields: m
            .iter()
            .map(|(k, v)| (k.clone(), json_to_prost_value(v)))
            .collect(),
    }
}

fn prost_value_to_json(v: &prost_types::Value) -> serde_json::Value {
    match &v.kind {
        Some(Kind::NullValue(_)) => serde_json::Value::Null,
        Some(Kind::NumberValue(n)) => serde_json::json!(*n),
        Some(Kind::StringValue(s)) => serde_json::Value::String(s.clone()),
        Some(Kind::BoolValue(b)) => serde_json::Value::Bool(*b),
        Some(Kind::StructValue(s)) => {
            serde_json::Value::Object(struct_to_json_map(s).into_iter().collect())
        }
        Some(Kind::ListValue(l)) => {
            serde_json::Value::Array(l.values.iter().map(prost_value_to_json).collect())
        }
        None => serde_json::Value::Null,
    }
}

fn json_to_prost_value(v: &serde_json::Value) -> prost_types::Value {
    let kind = match v {
        serde_json::Value::Null => Kind::NullValue(0),
        serde_json::Value::Bool(b) => Kind::BoolValue(*b),
        serde_json::Value::Number(n) => Kind::NumberValue(n.as_f64().unwrap_or(0.0)),
        serde_json::Value::String(s) => Kind::StringValue(s.clone()),
        serde_json::Value::Array(arr) => Kind::ListValue(prost_types::ListValue {
            values: arr.iter().map(json_to_prost_value).collect(),
        }),
        serde_json::Value::Object(obj) => {
            let map: HashMap<String, serde_json::Value> =
                obj.iter().map(|(k, v)| (k.clone(), v.clone())).collect();
            Kind::StructValue(json_map_to_struct(&map))
        }
    };
    prost_types::Value { kind: Some(kind) }
}
