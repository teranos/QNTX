//! Watcher filter matching.
//!
//! Given an attestation and a set of watcher filters, determines which watchers
//! match. Designed for batched evaluation: one call per attestation, all watchers
//! checked in a single pass.
//!
//! A watcher asking `over` measures each attestation as it arrives, between the
//! start and end the types it is of name (crate::ax::over). Those definitions
//! live in the store, which this crate does not reach from here: a call without
//! them answers every other watcher and says which types to read, and the call
//! that gives them answers the rest.

use std::collections::HashMap;

use serde::{Deserialize, Serialize};
use serde_json::Value;

use crate::attestation::{names_meet, Attestation};
use crate::ax::over;
use crate::span::Span;

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
    /// `over 5y`: only attestations longer than the span.
    #[serde(default)]
    pub over: Option<Span>,
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
    /// The definitions of the types the attestation is of, once read: every
    /// `[type] is type` attestation naming one of its predicates.
    #[serde(default)]
    pub types: Option<Vec<MatchAttestation>>,
}

/// Output from batch watcher matching.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MatchOutput {
    pub matched_ids: Vec<String>,
    /// What to read before the watchers asking `over` are answered: given no
    /// type definitions, a watcher that matches but for its length is in
    /// neither `matched_ids` nor out of them. Read these, give them as
    /// `types`, and ask again. Never more than one.
    #[serde(default)]
    pub read: Vec<TypesRead>,
}

/// The definitions to read: the types an attestation is of.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct TypesRead {
    pub types: Vec<String>,
}

/// Match an attestation against all watcher filters, return IDs of matched watchers.
pub fn match_watchers(input: &MatchInput) -> MatchOutput {
    let attestation = as_attestation(&input.attestation);
    let ends: Option<HashMap<String, over::Ends>> = input
        .types
        .as_ref()
        .map(|types| over::ends_of(&types.iter().map(as_attestation).collect::<Vec<_>>()));

    let mut matched_ids = Vec::new();
    let mut unmeasured = false;
    for watcher in &input.watchers {
        if !matches_filter(&input.attestation, watcher) {
            continue;
        }
        let longer = match (&watcher.over, &ends) {
            (None, _) => true,
            (Some(span), Some(ends)) => over::is_over(&attestation, span, ends),
            (Some(_), None) => {
                unmeasured = true;
                false
            }
        };
        if longer {
            matched_ids.push(watcher.id.clone());
        }
    }

    let read = match unmeasured {
        true => vec![TypesRead {
            types: over::types_of(std::slice::from_ref(&attestation)).subjects,
        }],
        false => Vec::new(),
    };
    MatchOutput { matched_ids, read }
}

/// The attestation as the over reading takes it. What it is not given, an id
/// and a source, plays no part in a length.
fn as_attestation(a: &MatchAttestation) -> Attestation {
    Attestation {
        id: String::new(),
        subjects: a.subjects.clone(),
        predicates: a.predicates.clone(),
        contexts: a.contexts.clone(),
        actors: a.actors.clone(),
        timestamp: a.timestamp_ms,
        source: String::new(),
        attributes: match &a.attributes {
            Some(Value::Object(map)) => map.clone().into_iter().collect(),
            Some(_) | None => HashMap::new(),
        },
        created_at: a.timestamp_ms,
        signature: None,
        signer_did: None,
    }
}

/// Check if an attestation matches a single watcher's structural filter.
fn matches_filter(att: &MatchAttestation, watcher: &WatcherFilter) -> bool {
    let named = names_meet(&watcher.subjects, &att.subjects)
        && names_meet(&watcher.predicates, &att.predicates)
        && names_meet(&watcher.contexts, &att.contexts)
        && names_meet(&watcher.actors, &att.actors);
    if !named {
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
            over: None,
        }
    }

    #[test]
    fn test_empty_filter_matches_all() {
        let att = make_attestation(&["ALICE"], &["crawled"], &["web"], &["levi"]);
        let watcher = make_filter("w1", &[], &[]);
        let input = MatchInput {
            types: None,
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
            types: None,
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
            types: None,
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
            types: None,
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
            types: None,
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
            over: None,
        };

        let input = MatchInput {
            types: None,
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
            over: None,
        };

        let input = MatchInput {
            types: None,
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
            over: None,
        };

        let input = MatchInput {
            types: None,
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
            types: None,
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

    fn employed(from: &str, until: &str) -> MatchAttestation {
        MatchAttestation {
            attributes: Some(json!({"from": from, "until": until})),
            ..make_attestation(&["ALICE"], &["employed"], &[], &["hr"])
        }
    }

    fn employed_type() -> MatchAttestation {
        MatchAttestation {
            attributes: Some(json!({"start_field": "from", "end_field": "until"})),
            ..make_attestation(&["employed"], &["type"], &[], &["employed"])
        }
    }

    fn over(id: &str, span: &str) -> WatcherFilter {
        WatcherFilter {
            over: Some(Span::parse(span).unwrap()),
            ..make_filter(id, &["ALICE"], &[])
        }
    }

    #[test]
    fn over_asks_for_the_types_then_measures_by_them() {
        let watchers = vec![
            over("long", "5y"),
            over("short", "1mo"),
            make_filter("any", &[], &[]),
        ];
        let first = match_watchers(&MatchInput {
            attestation: employed("2018-03-01", "2020-03-01"),
            watchers: watchers.clone(),
            types: None,
        });
        // Every watcher not asking over is answered; the rest wait on the types.
        assert_eq!(first.matched_ids, vec!["any"]);
        assert_eq!(
            first.read,
            vec![TypesRead {
                types: vec!["employed".into()]
            }]
        );

        let second = match_watchers(&MatchInput {
            attestation: employed("2018-03-01", "2020-03-01"),
            watchers,
            types: Some(vec![employed_type()]),
        });
        assert_eq!(second.matched_ids, vec!["short", "any"]);
        assert_eq!(second.read, vec![]);
    }

    #[test]
    fn over_reads_nothing_for_an_attestation_no_watcher_could_match() {
        let output = match_watchers(&MatchInput {
            attestation: MatchAttestation {
                subjects: vec!["BOB".into()],
                ..employed("2018-03-01", "2020-03-01")
            },
            watchers: vec![over("long", "1y")],
            types: None,
        });
        assert_eq!(output.matched_ids, Vec::<String>::new());
        assert_eq!(output.read, vec![]);
    }

    #[test]
    fn a_type_naming_no_ends_gives_no_length() {
        let output = match_watchers(&MatchInput {
            attestation: employed("2018-03-01", "2030-03-01"),
            watchers: vec![over("long", "1y")],
            types: Some(vec![]),
        });
        assert_eq!(output.matched_ids, Vec::<String>::new());
        assert_eq!(output.read, vec![]);
    }

    #[test]
    fn over_crosses_as_its_text() {
        let input = r#"{
            "attestation": {"subjects": ["ALICE"], "predicates": ["employed"], "timestamp_ms": 1,
                            "attributes": {"from": "2018-03-01", "until": "2024-03-01"}},
            "watchers": [{"id": "w", "over": "5y"}],
            "types": [{"subjects": ["employed"], "predicates": ["type"], "timestamp_ms": 1,
                       "attributes": {"start_field": "from", "end_field": "until"}}]
        }"#;
        let output: MatchOutput = serde_json::from_str(&match_watchers_json(input)).unwrap();
        assert_eq!(output.matched_ids, vec!["w"]);
    }
}
