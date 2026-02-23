//! Proto type conversion utilities
//!
//! Converts between proto-generated types (prost + custom serde) and
//! qntx_core internal types. The proto types use google.protobuf.Struct
//! for attributes; core types use HashMap<String, serde_json::Value>.

use crate::serde_struct;
use crate::Attestation as ProtoAttestation;
use qntx_core::attestation::Attestation as CoreAttestation;

/// Convert a proto Attestation to core Attestation
pub fn from_proto(proto: ProtoAttestation) -> CoreAttestation {
    CoreAttestation {
        id: proto.id,
        subjects: proto.subjects,
        predicates: proto.predicates,
        contexts: proto.contexts,
        actors: proto.actors,
        timestamp: proto.timestamp,
        source: proto.source,
        attributes: proto
            .attributes
            .as_ref()
            .map(serde_struct::struct_to_json_map)
            .unwrap_or_default(),
        created_at: proto.created_at,
    }
}

/// Convert a core Attestation to proto Attestation
pub fn to_proto(core: CoreAttestation) -> ProtoAttestation {
    let attributes = if core.attributes.is_empty() {
        None
    } else {
        Some(serde_struct::json_map_to_struct(&core.attributes))
    };

    ProtoAttestation {
        id: core.id,
        subjects: core.subjects,
        predicates: core.predicates,
        contexts: core.contexts,
        actors: core.actors,
        timestamp: core.timestamp,
        source: core.source,
        attributes,
        created_at: core.created_at,
        signature: Vec::new(),
        signer_did: String::new(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashMap;

    #[test]
    fn test_proto_roundtrip() {
        let mut attributes = HashMap::new();
        attributes.insert("key".to_string(), serde_json::json!("value"));
        attributes.insert("count".to_string(), serde_json::json!(42));

        let core = CoreAttestation {
            id: "test-123".to_string(),
            subjects: vec!["alice".to_string()],
            predicates: vec!["created".to_string()],
            contexts: vec!["test".to_string()],
            actors: vec!["system".to_string()],
            timestamp: 1234567890,
            source: "test".to_string(),
            attributes,
            created_at: 1234567890,
        };

        let proto = to_proto(core.clone());
        let back = from_proto(proto);

        assert_eq!(back.id, core.id);
        assert_eq!(back.subjects, core.subjects);
        assert_eq!(back.predicates, core.predicates);
        assert_eq!(back.timestamp, core.timestamp);
        assert_eq!(back.created_at, core.created_at);
        assert_eq!(back.attributes["key"], "value");
        // f64 roundtrip: integer becomes float in protobuf Struct
        assert_eq!(back.attributes["count"], 42.0);
    }

    #[test]
    fn test_proto_json_attributes_are_object() {
        // With google.protobuf.Struct + custom serde, attributes serialize as a
        // plain JSON object — NOT a JSON string. This prevents double-encoding.
        let mut attributes = HashMap::new();
        attributes.insert("status".to_string(), serde_json::json!("active"));
        attributes.insert("count".to_string(), serde_json::json!(42));

        let core = CoreAttestation {
            id: "AS-boundary-test".to_string(),
            subjects: vec!["USER-123".to_string()],
            predicates: vec!["logged_in".to_string()],
            contexts: vec!["web_app".to_string()],
            actors: vec!["auth_service".to_string()],
            timestamp: 1704067200,
            source: "wasm_boundary_test".to_string(),
            attributes,
            created_at: 1704067200,
        };

        let proto = to_proto(core);
        let json = serde_json::to_string(&proto).expect("serialization should succeed");
        let parsed: serde_json::Value = serde_json::from_str(&json).expect("should be valid JSON");

        // Attributes must be a JSON object, not a string
        assert!(
            parsed["attributes"].is_object(),
            "attributes must be a JSON object, got: {}",
            parsed["attributes"]
        );
        assert_eq!(parsed["attributes"]["status"], "active");
        assert_eq!(parsed["attributes"]["count"], 42.0);

        // Timestamps remain numbers
        assert!(parsed["timestamp"].is_i64());
        assert_eq!(parsed["timestamp"].as_i64().unwrap(), 1704067200);
    }

    #[test]
    fn test_empty_attributes_roundtrip() {
        let core = CoreAttestation {
            id: "test-empty".to_string(),
            subjects: vec!["s".to_string()],
            predicates: vec!["p".to_string()],
            contexts: vec!["c".to_string()],
            actors: vec!["a".to_string()],
            timestamp: 1000,
            source: "test".to_string(),
            attributes: HashMap::new(),
            created_at: 0,
        };

        let proto = to_proto(core.clone());
        assert!(proto.attributes.is_none());

        let back = from_proto(proto);
        assert!(back.attributes.is_empty());
    }

    #[test]
    fn test_json_deserialize_with_object_attributes() {
        // Simulate JSON coming from TypeScript with attributes as plain object
        let json = r#"{
            "id": "AS-from-ts",
            "subjects": ["USER-1"],
            "predicates": ["active"],
            "contexts": ["web"],
            "actors": ["system"],
            "timestamp": 1000,
            "source": "browser",
            "attributes": {"theme": "dark", "version": 2},
            "created_at": 2000
        }"#;

        let proto: ProtoAttestation =
            serde_json::from_str(json).expect("should deserialize with object attributes");
        let core = from_proto(proto);

        assert_eq!(core.attributes["theme"], "dark");
        assert_eq!(core.attributes["version"], 2.0);
    }
}
