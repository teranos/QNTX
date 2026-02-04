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

use qntx_core::attestation::Attestation as CoreAttestation;
use crate::Attestation as ProtoAttestation;

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
        // Both proto and core use i64 (Unix seconds)
        timestamp: proto.timestamp,
        source: proto.source,
        // Proto uses JSON string, core uses HashMap
        attributes: serde_json::from_str(&proto.attributes).unwrap_or_default(),
        // Both proto and core use i64 (Unix seconds)
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
        // Both core and proto use i64 (Unix seconds)
        timestamp: core.timestamp,
        source: core.source,
        // Core uses HashMap, proto uses JSON string
        attributes: serde_json::to_string(&core.attributes).unwrap_or_else(|_| "{}".to_string()),
        // Both core and proto use i64 (Unix seconds)
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
}
