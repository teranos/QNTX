//! Proto type conversion utilities
//!
//! PROTO MIGRATION NOTE (see ADR-006):
//! This module demonstrates using proto-generated types from qntx-proto
//! as the canonical source of truth for type definitions. We convert
//! between proto types and internal representations as needed.
//!
//! Benefits of using proto types directly:
//! - Single source of truth across Go, Rust, and TypeScript
//! - No heavy gRPC dependencies (qntx-proto is types-only)
//! - Automatic serialization with serde support
//!
//! This is part of the gradual migration from typegen to proto.

use crate::Attestation as ProtoAttestation;
use qntx_core::attestation::Attestation as CoreAttestation;

/// Convert a proto Attestation to core Attestation
///
/// This demonstrates the proto → internal type conversion pattern.
/// As we migrate fully to proto, this conversion layer will become unnecessary.
pub fn from_proto(proto: ProtoAttestation) -> CoreAttestation {
    CoreAttestation {
        id: proto.id,
        subjects: proto.subjects,
        predicates: proto.predicates,
        contexts: proto.contexts,
        actors: proto.actors,
        timestamp: proto.timestamp,
        source: proto.source,
        // Proto uses JSON string, core uses HashMap
        attributes: serde_json::from_str(&proto.attributes).unwrap_or_default(),
        created_at: proto.created_at,
    }
}

/// Convert a core Attestation to proto Attestation
///
/// This demonstrates the internal → proto type conversion pattern.
/// Proto types are used for wire format and cross-language compatibility.
pub fn to_proto(core: CoreAttestation) -> ProtoAttestation {
    ProtoAttestation {
        id: core.id,
        subjects: core.subjects,
        predicates: core.predicates,
        contexts: core.contexts,
        actors: core.actors,
        timestamp: core.timestamp,
        source: core.source,
        // Core uses HashMap, proto uses JSON string
        attributes: serde_json::to_string(&core.attributes).unwrap_or_else(|_| "{}".to_string()),
        created_at: core.created_at,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashMap;

    #[test]
    fn test_proto_roundtrip() {
        // Create a core attestation
        let mut attributes = HashMap::new();
        attributes.insert("key".to_string(), serde_json::json!("value"));

        let core = CoreAttestation {
            id: "test-123".to_string(),
            subjects: vec!["alice".to_string()],
            predicates: vec!["created".to_string()],
            contexts: vec!["test".to_string()],
            actors: vec!["system".to_string()],
            timestamp: 1234567890, // Unix timestamp
            source: "test".to_string(),
            attributes,
            created_at: 1234567890, // Unix timestamp
        };

        // Convert to proto and back
        let proto = to_proto(core.clone());
        let back = from_proto(proto);

        // Check key fields match
        assert_eq!(back.id, core.id);
        assert_eq!(back.subjects, core.subjects);
        assert_eq!(back.predicates, core.predicates);
        assert_eq!(back.timestamp, core.timestamp);
        assert_eq!(back.created_at, core.created_at);
    }

    #[test]
    fn test_proto_json_format_matches_typescript_expectations() {
        // CRITICAL: This test validates the JSON format matches what TypeScript expects
        // at the WASM→TypeScript boundary (web/ts/qntx-wasm.ts uses proto-generated types)
        let mut attributes = HashMap::new();
        attributes.insert("status".to_string(), serde_json::json!("active"));
        attributes.insert("count".to_string(), serde_json::json!(42));

        let core = CoreAttestation {
            id: "AS-boundary-test".to_string(),
            subjects: vec!["USER-123".to_string()],
            predicates: vec!["logged_in".to_string()],
            contexts: vec!["web_app".to_string()],
            actors: vec!["auth_service".to_string()],
            timestamp: 1704067200, // 2024-01-01 00:00:00 UTC
            source: "wasm_boundary_test".to_string(),
            attributes,
            created_at: 1704067200,
        };

        // Convert to proto and serialize to JSON (what WASM does)
        let proto = to_proto(core);
        let json = serde_json::to_string(&proto).expect("serialization should succeed");

        // Parse as JSON value to inspect structure
        let parsed: serde_json::Value = serde_json::from_str(&json).expect("should be valid JSON");

        // CRITICAL: Timestamps must be numbers (i64), NOT ISO strings
        assert!(
            parsed["timestamp"].is_i64(),
            "timestamp must be a number for TypeScript compatibility"
        );
        assert_eq!(parsed["timestamp"].as_i64().unwrap(), 1704067200);
        assert!(
            parsed["created_at"].is_i64(),
            "created_at must be a number for TypeScript compatibility"
        );

        // CRITICAL: Attributes must be a JSON string, NOT an object
        assert!(
            parsed["attributes"].is_string(),
            "attributes must be a JSON string (proto schema requirement)"
        );
        let attributes_json = parsed["attributes"].as_str().unwrap();
        let attributes_parsed: serde_json::Value =
            serde_json::from_str(attributes_json).expect("attributes should contain valid JSON");
        assert_eq!(attributes_parsed["status"], "active");
        assert_eq!(attributes_parsed["count"], 42);

        // Verify all required fields are present
        assert_eq!(parsed["id"].as_str().unwrap(), "AS-boundary-test");
        assert_eq!(parsed["subjects"].as_array().unwrap().len(), 1);
        assert_eq!(parsed["predicates"].as_array().unwrap().len(), 1);
        assert_eq!(parsed["contexts"].as_array().unwrap().len(), 1);
        assert_eq!(parsed["actors"].as_array().unwrap().len(), 1);
        assert_eq!(parsed["source"].as_str().unwrap(), "wasm_boundary_test");
    }
}
