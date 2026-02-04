//! Proto type conversion utilities
//!
//! Converts between proto-generated types (qntx-proto) and core types (qntx-core)
//! for the IndexedDB storage backend. Same pattern as qntx-sqlite/src/proto_convert.rs.
//!
//! As proto types become the single source of truth across Go, Rust, and TypeScript,
//! this conversion layer will become unnecessary.

use qntx_core::attestation::Attestation as CoreAttestation;
use qntx_proto::Attestation as ProtoAttestation;

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
        attributes: serde_json::from_str(&proto.attributes).unwrap_or_default(),
        created_at: proto.created_at,
    }
}

/// Convert a core Attestation to proto Attestation
pub fn to_proto(core: CoreAttestation) -> ProtoAttestation {
    ProtoAttestation {
        id: core.id,
        subjects: core.subjects,
        predicates: core.predicates,
        contexts: core.contexts,
        actors: core.actors,
        timestamp: core.timestamp,
        source: core.source,
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
        let mut attributes = HashMap::new();
        attributes.insert("key".to_string(), serde_json::json!("value"));

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
    }
}
