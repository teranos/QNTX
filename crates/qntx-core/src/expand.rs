//! Cartesian claim expansion for multi-dimensional attestations.
//!
//! Attestations are stored compactly: one row with multiple subjects, predicates,
//! contexts, and actors. Conflict detection and resolution reason about individual
//! claims — a single (subject, predicate, context, actor) tuple. This module
//! bridges the two representations.
//!
//! `expand` explodes compact attestations into individual claims.
//! `group_by_key` re-groups claims by (subject, predicate, context) for classification.
//! `dedup_source_ids` collapses claims back to unique source attestation IDs.

use serde::{Deserialize, Serialize};

/// A compact attestation as received from Go / the storage layer.
/// Mirrors the JSON shape of `types.As` on the Go side.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ExpandAttestation {
    pub id: String,
    pub subjects: Vec<String>,
    pub predicates: Vec<String>,
    pub contexts: Vec<String>,
    pub actors: Vec<String>,
    /// Unix timestamp in milliseconds
    pub timestamp_ms: i64,
}

/// A single claim extracted from a multi-dimensional attestation.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct IndividualClaim {
    pub subject: String,
    pub predicate: String,
    pub context: String,
    pub actor: String,
    pub timestamp_ms: i64,
    /// ID of the source attestation
    pub source_id: String,
}

/// A group of claims sharing the same (subject, predicate, context) key.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ClaimGroup {
    pub key: String,
    pub claims: Vec<IndividualClaim>,
}

/// Separator used to join subject, predicate, and context into a unique key.
const CLAIM_KEY_SEP: &str = "|";

/// Expand a list of compact attestations into individual claims via cartesian product.
///
/// Each attestation with S subjects, P predicates, C contexts, A actors produces
/// S × P × C × A individual claims.
pub fn expand_cartesian(attestations: &[ExpandAttestation]) -> Vec<IndividualClaim> {
    let total_capacity: usize = attestations
        .iter()
        .map(|a| a.subjects.len() * a.predicates.len() * a.contexts.len() * a.actors.len())
        .sum();

    let mut claims = Vec::with_capacity(total_capacity);

    for a in attestations {
        for subject in &a.subjects {
            for predicate in &a.predicates {
                for context in &a.contexts {
                    for actor in &a.actors {
                        claims.push(IndividualClaim {
                            subject: subject.clone(),
                            predicate: predicate.clone(),
                            context: context.clone(),
                            actor: actor.clone(),
                            timestamp_ms: a.timestamp_ms,
                            source_id: a.id.clone(),
                        });
                    }
                }
            }
        }
    }

    claims
}

/// Group claims by their (subject, predicate, context) key.
///
/// Returns groups in a deterministic order: sorted by key.
pub fn group_by_key(claims: &[IndividualClaim]) -> Vec<ClaimGroup> {
    use std::collections::BTreeMap;

    let mut map: BTreeMap<String, Vec<IndividualClaim>> = BTreeMap::new();

    for claim in claims {
        let key = format!(
            "{}{}{}{}{}",
            claim.subject, CLAIM_KEY_SEP, claim.predicate, CLAIM_KEY_SEP, claim.context
        );
        map.entry(key).or_default().push(claim.clone());
    }

    map.into_iter()
        .map(|(key, claims)| ClaimGroup { key, claims })
        .collect()
}

/// Deduplicate claims back to unique source attestation IDs, preserving order.
pub fn dedup_source_ids(claims: &[IndividualClaim]) -> Vec<String> {
    let mut seen = std::collections::HashSet::new();
    let mut ids = Vec::new();

    for claim in claims {
        if seen.insert(&claim.source_id) {
            ids.push(claim.source_id.clone());
        }
    }

    ids
}

/// Input for the WASM expand_cartesian_claims function.
#[derive(Debug, Deserialize)]
pub struct ExpandInput {
    pub attestations: Vec<ExpandAttestation>,
}

/// Output of the WASM expand_cartesian_claims function.
#[derive(Debug, Serialize)]
pub struct ExpandOutput {
    pub claims: Vec<IndividualClaim>,
    pub total: usize,
}

/// JSON entry point: deserialize input, expand, serialize output.
/// Used by both wazero and browser WASM targets.
pub fn expand_claims_json(input: &str) -> String {
    let parsed: ExpandInput = match serde_json::from_str(input) {
        Ok(v) => v,
        Err(e) => {
            return format!(r#"{{"error":"invalid expand input: {}"}}"#, e);
        }
    };

    let claims = expand_cartesian(&parsed.attestations);
    let total = claims.len();

    match serde_json::to_string(&ExpandOutput { claims, total }) {
        Ok(json) => json,
        Err(e) => format!(r#"{{"error":"serialization failed: {}"}}"#, e),
    }
}

/// Input for the WASM group_claims function.
#[derive(Debug, Deserialize)]
pub struct GroupInput {
    pub claims: Vec<IndividualClaim>,
}

/// Output of the WASM group_claims function.
#[derive(Debug, Serialize)]
pub struct GroupOutput {
    pub groups: Vec<ClaimGroup>,
    pub total_groups: usize,
}

/// JSON entry point: deserialize claims, group by key, serialize output.
pub fn group_claims_json(input: &str) -> String {
    let parsed: GroupInput = match serde_json::from_str(input) {
        Ok(v) => v,
        Err(e) => {
            return format!(r#"{{"error":"invalid group input: {}"}}"#, e);
        }
    };

    let groups = group_by_key(&parsed.claims);
    let total_groups = groups.len();

    match serde_json::to_string(&GroupOutput {
        groups,
        total_groups,
    }) {
        Ok(json) => json,
        Err(e) => format!(r#"{{"error":"serialization failed: {}"}}"#, e),
    }
}

/// Input for the WASM dedup_source_ids function.
#[derive(Debug, Deserialize)]
pub struct DedupInput {
    pub claims: Vec<IndividualClaim>,
}

/// Output of the WASM dedup_source_ids function.
#[derive(Debug, Serialize)]
pub struct DedupOutput {
    pub ids: Vec<String>,
    pub total: usize,
}

/// JSON entry point: deserialize claims, dedup source IDs, serialize output.
pub fn dedup_source_ids_json(input: &str) -> String {
    let parsed: DedupInput = match serde_json::from_str(input) {
        Ok(v) => v,
        Err(e) => {
            return format!(r#"{{"error":"invalid dedup input: {}"}}"#, e);
        }
    };

    let ids = dedup_source_ids(&parsed.claims);
    let total = ids.len();

    match serde_json::to_string(&DedupOutput { ids, total }) {
        Ok(json) => json,
        Err(e) => format!(r#"{{"error":"serialization failed: {}"}}"#, e),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_attestation(
        id: &str,
        subjects: &[&str],
        predicates: &[&str],
        contexts: &[&str],
        actors: &[&str],
        timestamp_ms: i64,
    ) -> ExpandAttestation {
        ExpandAttestation {
            id: id.to_string(),
            subjects: subjects.iter().map(|s| s.to_string()).collect(),
            predicates: predicates.iter().map(|s| s.to_string()).collect(),
            contexts: contexts.iter().map(|s| s.to_string()).collect(),
            actors: actors.iter().map(|s| s.to_string()).collect(),
            timestamp_ms,
        }
    }

    #[test]
    fn basic_cartesian_expansion() {
        let attestations = vec![make_attestation(
            "SW001",
            &["LUKE", "LEIA"],
            &["operates_in", "located_at"],
            &["REBELLION", "TATOOINE"],
            &["imperial-records"],
            1000,
        )];

        let claims = expand_cartesian(&attestations);

        // 2 × 2 × 2 × 1 = 8
        assert_eq!(claims.len(), 8);

        // Verify all combinations present
        let combos: Vec<(&str, &str, &str)> = claims
            .iter()
            .map(|c| (c.subject.as_str(), c.predicate.as_str(), c.context.as_str()))
            .collect();

        assert!(combos.contains(&("LUKE", "operates_in", "REBELLION")));
        assert!(combos.contains(&("LUKE", "operates_in", "TATOOINE")));
        assert!(combos.contains(&("LUKE", "located_at", "REBELLION")));
        assert!(combos.contains(&("LUKE", "located_at", "TATOOINE")));
        assert!(combos.contains(&("LEIA", "operates_in", "REBELLION")));
        assert!(combos.contains(&("LEIA", "operates_in", "TATOOINE")));
        assert!(combos.contains(&("LEIA", "located_at", "REBELLION")));
        assert!(combos.contains(&("LEIA", "located_at", "TATOOINE")));

        // All should reference the source
        for claim in &claims {
            assert_eq!(claim.source_id, "SW001");
            assert_eq!(claim.actor, "imperial-records");
            assert_eq!(claim.timestamp_ms, 1000);
        }
    }

    #[test]
    fn single_dimension() {
        let attestations = vec![make_attestation(
            "SW002",
            &["YODA"],
            &["trained_by"],
            &["JEDI-ORDER"],
            &["jedi-archives"],
            2000,
        )];

        let claims = expand_cartesian(&attestations);
        assert_eq!(claims.len(), 1);

        let c = &claims[0];
        assert_eq!(c.subject, "YODA");
        assert_eq!(c.predicate, "trained_by");
        assert_eq!(c.context, "JEDI-ORDER");
        assert_eq!(c.actor, "jedi-archives");
        assert_eq!(c.source_id, "SW002");
    }

    #[test]
    fn empty_attestations() {
        let claims = expand_cartesian(&[]);
        assert!(claims.is_empty());
    }

    #[test]
    fn multiple_attestations() {
        let attestations = vec![
            make_attestation("A1", &["X"], &["p"], &["c"], &["a"], 100),
            make_attestation("A2", &["Y", "Z"], &["q"], &["c"], &["a"], 200),
        ];

        let claims = expand_cartesian(&attestations);
        // 1×1×1×1 + 2×1×1×1 = 3
        assert_eq!(claims.len(), 3);
        assert_eq!(claims[0].source_id, "A1");
        assert_eq!(claims[1].source_id, "A2");
        assert_eq!(claims[2].source_id, "A2");
    }

    #[test]
    fn group_claims_by_key() {
        let claims = vec![
            IndividualClaim {
                subject: "HAN".into(),
                predicate: "smuggler".into(),
                context: "MILLENNIUM-FALCON".into(),
                actor: "rebel-intelligence".into(),
                timestamp_ms: 100,
                source_id: "as-1".into(),
            },
            IndividualClaim {
                subject: "HAN".into(),
                predicate: "smuggler".into(),
                context: "MILLENNIUM-FALCON".into(),
                actor: "imperial-bounty".into(),
                timestamp_ms: 200,
                source_id: "as-2".into(),
            },
            IndividualClaim {
                subject: "VADER".into(),
                predicate: "commands".into(),
                context: "DEATH-STAR".into(),
                actor: "imperial-records".into(),
                timestamp_ms: 300,
                source_id: "as-3".into(),
            },
        ];

        let groups = group_by_key(&claims);
        assert_eq!(groups.len(), 2);

        // BTreeMap ensures sorted order
        assert_eq!(groups[0].key, "HAN|smuggler|MILLENNIUM-FALCON");
        assert_eq!(groups[0].claims.len(), 2);
        assert_eq!(groups[1].key, "VADER|commands|DEATH-STAR");
        assert_eq!(groups[1].claims.len(), 1);
    }

    #[test]
    fn dedup_preserves_order() {
        let claims = vec![
            IndividualClaim {
                subject: "A".into(),
                predicate: "p".into(),
                context: "c".into(),
                actor: "x".into(),
                timestamp_ms: 1,
                source_id: "SW003".into(),
            },
            IndividualClaim {
                subject: "B".into(),
                predicate: "p".into(),
                context: "c".into(),
                actor: "x".into(),
                timestamp_ms: 2,
                source_id: "SW003".into(), // duplicate
            },
            IndividualClaim {
                subject: "C".into(),
                predicate: "p".into(),
                context: "c".into(),
                actor: "x".into(),
                timestamp_ms: 3,
                source_id: "SW004".into(),
            },
        ];

        let ids = dedup_source_ids(&claims);
        assert_eq!(ids, vec!["SW003", "SW004"]);
    }

    #[test]
    fn expand_claims_json_roundtrip() {
        let input = serde_json::json!({
            "attestations": [{
                "id": "SW001",
                "subjects": ["LUKE", "LEIA"],
                "predicates": ["operates_in"],
                "contexts": ["REBELLION"],
                "actors": ["imperial-records"],
                "timestamp_ms": 1000
            }]
        });

        let result = expand_claims_json(&input.to_string());
        let parsed: serde_json::Value = serde_json::from_str(&result).unwrap();

        assert!(parsed["error"].is_null(), "unexpected error: {}", result);
        assert_eq!(parsed["total"], 2);
        assert_eq!(parsed["claims"].as_array().unwrap().len(), 2);
    }

    #[test]
    fn expand_claims_json_invalid_input() {
        let result = expand_claims_json("not json");
        let parsed: serde_json::Value = serde_json::from_str(&result).unwrap();
        assert!(parsed["error"]
            .as_str()
            .unwrap()
            .contains("invalid expand input"));
    }

    #[test]
    fn group_claims_json_roundtrip() {
        let input = serde_json::json!({
            "claims": [
                {"subject": "R2D2", "predicate": "copilot_of", "context": "X-WING", "actor": "rebel-fleet", "timestamp_ms": 1, "source_id": "sw-001"},
                {"subject": "R2D2", "predicate": "copilot_of", "context": "X-WING", "actor": "astromech-logs", "timestamp_ms": 2, "source_id": "sw-002"},
                {"subject": "BB8", "predicate": "assigned_to", "context": "MILLENNIUM-FALCON", "actor": "rebel-fleet", "timestamp_ms": 3, "source_id": "sw-003"}
            ]
        });

        let result = group_claims_json(&input.to_string());
        let parsed: serde_json::Value = serde_json::from_str(&result).unwrap();

        assert!(parsed["error"].is_null(), "unexpected error: {}", result);
        assert_eq!(parsed["total_groups"], 2);
    }

    #[test]
    fn dedup_source_ids_json_roundtrip() {
        let input = serde_json::json!({
            "claims": [
                {"subject": "R2D2", "predicate": "served_on", "context": "TANTIVE-IV", "actor": "rebel-archives", "timestamp_ms": 1, "source_id": "sw-001"},
                {"subject": "C3PO", "predicate": "served_on", "context": "TANTIVE-IV", "actor": "rebel-archives", "timestamp_ms": 2, "source_id": "sw-001"},
                {"subject": "R2D2", "predicate": "hacked", "context": "DEATH-STAR", "actor": "rebel-archives", "timestamp_ms": 3, "source_id": "sw-002"}
            ]
        });

        let result = dedup_source_ids_json(&input.to_string());
        let parsed: serde_json::Value = serde_json::from_str(&result).unwrap();

        assert!(parsed["error"].is_null(), "unexpected error: {}", result);
        assert_eq!(parsed["total"], 2);
        assert_eq!(parsed["ids"].as_array().unwrap(), &["sw-001", "sw-002"]);
    }
}
