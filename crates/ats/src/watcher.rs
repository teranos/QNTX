//! Watcher filter matching.
//!
//! Given an attestation and a set of watcher filters, determines which watchers
//! match. Designed for batched evaluation: one call per attestation, all watchers
//! checked in a single pass.

use serde::{Deserialize, Serialize};
use serde_json::Value;

/// A watcher's structural filter — the fields an attestation must overlap with.
#[derive(Debug, Clone, Deserialize)]
pub struct WatcherFilter {
    pub id: String,
    #[serde(default)]
    pub subjects: Vec<String>,
    #[serde(default)]
    pub predicates: Vec<String>,
    #[serde(default)]
    pub contexts: Vec<String>,
    #[serde(default)]
    pub actors: Vec<String>,
    #[serde(default)]
    pub time_start_ms: Option<i64>,
    #[serde(default)]
    pub time_end_ms: Option<i64>,
    #[serde(default)]
    pub attribute_filters: Vec<AttributeFilter>,
}

/// A single attribute filter — dot-path navigation + operator.
#[derive(Debug, Clone, Deserialize)]
pub struct AttributeFilter {
    pub path: String,
    pub op: String,
    pub value: String,
}

/// An attestation to match against watcher filters.
#[derive(Debug, Clone, Deserialize)]
pub struct MatchAttestation {
    #[serde(default)]
    pub subjects: Vec<String>,
    #[serde(default)]
    pub predicates: Vec<String>,
    #[serde(default)]
    pub contexts: Vec<String>,
    #[serde(default)]
    pub actors: Vec<String>,
    #[serde(default)]
    pub timestamp_ms: i64,
    #[serde(default)]
    pub attributes: Option<Value>,
}

/// Input for batch watcher matching.
#[derive(Debug, Clone, Deserialize)]
pub struct MatchInput {
    pub attestation: MatchAttestation,
    pub watchers: Vec<WatcherFilter>,
}

/// Output from batch watcher matching.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MatchOutput {
    pub matched_ids: Vec<String>,
}

/// Match an attestation against all watcher filters, return IDs of matched watchers.
pub fn match_watchers(input: &MatchInput) -> MatchOutput {
    let mut matched_ids = Vec::new();

    for watcher in &input.watchers {
        if matches_filter(&input.attestation, watcher) {
            matched_ids.push(watcher.id.clone());
        }
    }

    MatchOutput { matched_ids }
}

/// Check if an attestation matches a single watcher's structural filter.
fn matches_filter(att: &MatchAttestation, watcher: &WatcherFilter) -> bool {
    if !watcher.subjects.is_empty() && !has_overlap_ci(&watcher.subjects, &att.subjects) {
        return false;
    }
    if !watcher.predicates.is_empty() && !has_overlap_ci(&watcher.predicates, &att.predicates) {
        return false;
    }
    if !watcher.contexts.is_empty() && !has_overlap_ci(&watcher.contexts, &att.contexts) {
        return false;
    }
    if !watcher.actors.is_empty() && !has_overlap_ci(&watcher.actors, &att.actors) {
        return false;
    }

    if let Some(start) = watcher.time_start_ms {
        if att.timestamp_ms < start {
            return false;
        }
    }
    if let Some(end) = watcher.time_end_ms {
        if att.timestamp_ms > end {
            return false;
        }
    }

    for af in &watcher.attribute_filters {
        if !matches_attribute_filter(&att.attributes, af) {
            return false;
        }
    }

    true
}

/// Case-insensitive overlap check between two string slices.
fn has_overlap_ci(filter: &[String], values: &[String]) -> bool {
    for v in values {
        let lower = v.to_ascii_lowercase();
        for f in filter {
            if f.to_ascii_lowercase() == lower {
                return true;
            }
        }
    }
    false
}

/// Check a single attribute filter against attestation attributes.
fn matches_attribute_filter(attrs: &Option<Value>, af: &AttributeFilter) -> bool {
    let attrs = match attrs {
        Some(v) => v,
        None => return false,
    };

    let resolved = resolve_attr_path(attrs, &af.path);
    let resolved = match resolved {
        Some(s) => s,
        None => return false,
    };

    match af.op.as_str() {
        "equals" => resolved == af.value,
        "contains" => resolved.contains(&af.value),
        _ => false,
    }
}

/// Navigate a dot-separated path through nested JSON objects.
/// Returns the string value at the path, or None.
fn resolve_attr_path<'a>(value: &'a Value, path: &str) -> Option<&'a str> {
    let mut current = value;

    for part in path.split('.') {
        current = current.get(part)?;
    }

    current.as_str()
}

/// JSON entry point for WASM.
pub fn match_watchers_json(input: &str) -> String {
    let parsed: MatchInput = match serde_json::from_str(input) {
        Ok(v) => v,
        Err(e) => {
            return format!(
                r#"{{"error":"invalid input: {}"}}"#,
                e.to_string().replace('"', "\\\"")
            );
        }
    };

    let output = match_watchers(&parsed);
    serde_json::to_string(&output)
        .unwrap_or_else(|e| format!(r#"{{"error":"serialization failed: {}"}}"#, e))
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn make_attestation(
        subjects: &[&str],
        predicates: &[&str],
        contexts: &[&str],
        actors: &[&str],
    ) -> MatchAttestation {
        MatchAttestation {
            subjects: subjects.iter().map(|s| s.to_string()).collect(),
            predicates: predicates.iter().map(|s| s.to_string()).collect(),
            contexts: contexts.iter().map(|s| s.to_string()).collect(),
            actors: actors.iter().map(|s| s.to_string()).collect(),
            timestamp_ms: 1_000_000,
            attributes: None,
        }
    }

    fn make_filter(id: &str, subjects: &[&str], predicates: &[&str]) -> WatcherFilter {
        WatcherFilter {
            id: id.to_string(),
            subjects: subjects.iter().map(|s| s.to_string()).collect(),
            predicates: predicates.iter().map(|s| s.to_string()).collect(),
            contexts: Vec::new(),
            actors: Vec::new(),
            time_start_ms: None,
            time_end_ms: None,
            attribute_filters: Vec::new(),
        }
    }

    #[test]
    fn test_empty_filter_matches_all() {
        let att = make_attestation(&["ALICE"], &["crawled"], &["web"], &["levi"]);
        let watcher = make_filter("w1", &[], &[]);
        let input = MatchInput {
            attestation: att,
            watchers: vec![watcher],
        };
        let output = match_watchers(&input);
        assert_eq!(output.matched_ids, vec!["w1"]);
    }

    #[test]
    fn test_subject_match() {
        let att = make_attestation(&["ALICE"], &["crawled"], &[], &[]);
        let w1 = make_filter("w1", &["ALICE"], &[]);
        let w2 = make_filter("w2", &["BOB"], &[]);
        let input = MatchInput {
            attestation: att,
            watchers: vec![w1, w2],
        };
        let output = match_watchers(&input);
        assert_eq!(output.matched_ids, vec!["w1"]);
    }

    #[test]
    fn test_case_insensitive_overlap() {
        let att = make_attestation(&["Alice"], &["Crawled"], &[], &[]);
        let w1 = make_filter("w1", &["alice"], &["crawled"]);
        let input = MatchInput {
            attestation: att,
            watchers: vec![w1],
        };
        let output = match_watchers(&input);
        assert_eq!(output.matched_ids, vec!["w1"]);
    }

    #[test]
    fn test_predicate_mismatch() {
        let att = make_attestation(&["ALICE"], &["crawled"], &[], &[]);
        let w1 = make_filter("w1", &["ALICE"], &["announced"]);
        let input = MatchInput {
            attestation: att,
            watchers: vec![w1],
        };
        let output = match_watchers(&input);
        assert!(output.matched_ids.is_empty());
    }

    #[test]
    fn test_time_range() {
        let mut att = make_attestation(&["ALICE"], &[], &[], &[]);
        att.timestamp_ms = 5000;

        let mut w1 = make_filter("w1", &[], &[]);
        w1.time_start_ms = Some(3000);
        w1.time_end_ms = Some(7000);

        let mut w2 = make_filter("w2", &[], &[]);
        w2.time_start_ms = Some(6000);

        let input = MatchInput {
            attestation: att,
            watchers: vec![w1, w2],
        };
        let output = match_watchers(&input);
        assert_eq!(output.matched_ids, vec!["w1"]);
    }

    #[test]
    fn test_attribute_filter_equals() {
        let mut att = make_attestation(&[], &["crawled"], &[], &[]);
        att.attributes = Some(json!({"stage": "complete", "depth": "3"}));

        let w1 = WatcherFilter {
            id: "w1".to_string(),
            subjects: Vec::new(),
            predicates: Vec::new(),
            contexts: Vec::new(),
            actors: Vec::new(),
            time_start_ms: None,
            time_end_ms: None,
            attribute_filters: vec![AttributeFilter {
                path: "stage".to_string(),
                op: "equals".to_string(),
                value: "complete".to_string(),
            }],
        };

        let input = MatchInput {
            attestation: att,
            watchers: vec![w1],
        };
        let output = match_watchers(&input);
        assert_eq!(output.matched_ids, vec!["w1"]);
    }

    #[test]
    fn test_attribute_filter_contains() {
        let mut att = make_attestation(&[], &[], &[], &[]);
        att.attributes = Some(json!({"url": "https://example.com/page/123"}));

        let w1 = WatcherFilter {
            id: "w1".to_string(),
            subjects: Vec::new(),
            predicates: Vec::new(),
            contexts: Vec::new(),
            actors: Vec::new(),
            time_start_ms: None,
            time_end_ms: None,
            attribute_filters: vec![AttributeFilter {
                path: "url".to_string(),
                op: "contains".to_string(),
                value: "example.com".to_string(),
            }],
        };

        let input = MatchInput {
            attestation: att,
            watchers: vec![w1],
        };
        let output = match_watchers(&input);
        assert_eq!(output.matched_ids, vec!["w1"]);
    }

    #[test]
    fn test_nested_attribute_path() {
        let mut att = make_attestation(&[], &[], &[], &[]);
        att.attributes = Some(json!({"tool_input": {"command": "git status"}}));

        let w1 = WatcherFilter {
            id: "w1".to_string(),
            subjects: Vec::new(),
            predicates: Vec::new(),
            contexts: Vec::new(),
            actors: Vec::new(),
            time_start_ms: None,
            time_end_ms: None,
            attribute_filters: vec![AttributeFilter {
                path: "tool_input.command".to_string(),
                op: "contains".to_string(),
                value: "git".to_string(),
            }],
        };

        let input = MatchInput {
            attestation: att,
            watchers: vec![w1],
        };
        let output = match_watchers(&input);
        assert_eq!(output.matched_ids, vec!["w1"]);
    }

    #[test]
    fn test_batch_multiple_watchers() {
        let att = make_attestation(
            &["NODE1"],
            &["announced", "crawled"],
            &["reticulum"],
            &["levi"],
        );

        let w1 = make_filter("w1", &["NODE1"], &["announced"]);
        let w2 = make_filter("w2", &["NODE1"], &["crawled"]);
        let w3 = make_filter("w3", &["NODE2"], &[]);
        let w4 = make_filter("w4", &[], &["announced"]);

        let input = MatchInput {
            attestation: att,
            watchers: vec![w1, w2, w3, w4],
        };
        let output = match_watchers(&input);
        assert_eq!(output.matched_ids, vec!["w1", "w2", "w4"]);
    }

    #[test]
    fn test_json_roundtrip() {
        let input_json = r#"{
            "attestation": {
                "subjects": ["ALICE"],
                "predicates": ["crawled"],
                "contexts": [],
                "actors": ["levi"],
                "timestamp_ms": 1000000,
                "attributes": {"stage": "complete"}
            },
            "watchers": [
                {"id": "w1", "subjects": ["ALICE"], "predicates": [], "contexts": [], "actors": [], "attribute_filters": []},
                {"id": "w2", "subjects": ["BOB"], "predicates": [], "contexts": [], "actors": [], "attribute_filters": []}
            ]
        }"#;

        let result = match_watchers_json(input_json);
        let output: MatchOutput = serde_json::from_str(&result).unwrap();
        assert_eq!(output.matched_ids, vec!["w1"]);
    }
}
