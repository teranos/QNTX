//! Attestation distillation: fold evicted attestations into aggregate summaries.
//!
//! When enforcement evicts a batch of attestations, distillation preserves their
//! aggregate data in a single "distill attestation" before deletion. The distill
//! attestation is a normal attestation — it participates in enforcement like any
//! other, enabling recursive meta-distillation.

use qntx_core::attestation::Attestation;
use serde_json::Value;
use std::collections::{HashMap, HashSet};

/// Cap on unique string values collected per attribute before switching to count-only.
const STRING_VALUES_CAP: usize = 50;

/// Build a distill attestation from a batch of evicted attestations.
///
/// The distill attestation captures:
/// - Union of all subjects
/// - Union of all predicates (original names, no prefix)
/// - Union of all actors from evicted attestations
/// - Context from the eviction context (path 1) or `_distill` (paths 2/3)
/// - Merged attributes with mechanical aggregation rules
/// - `_distill`, `_count`, `_first_seen`, `_last_seen` metadata
pub fn build_distill_attestation(evicted: &[Attestation], context: &str) -> Attestation {
    let id = format!("AS-distill-{}", uuid::Uuid::new_v4());
    let now_ms = chrono::Utc::now().timestamp_millis();

    // Union of subjects
    let mut subjects_set: Vec<String> = Vec::new();
    {
        let mut seen = HashSet::new();
        for att in evicted {
            for s in &att.subjects {
                if seen.insert(s.clone()) {
                    subjects_set.push(s.clone());
                }
            }
        }
    }

    // Collect all predicates (union, deduplicated, original names)
    let mut predicates: Vec<String> = Vec::new();
    {
        let mut seen = HashSet::new();
        for att in evicted {
            for p in &att.predicates {
                if seen.insert(p.clone()) {
                    predicates.push(p.clone());
                }
            }
        }
    }

    // Collect all actors for _actors_sample (not used as attestation actors —
    // using canonical "distill" actor to avoid inflating entity_actors_limit)
    let mut actors_sample: Vec<String> = Vec::new();
    {
        let mut seen = HashSet::new();
        for att in evicted {
            for a in &att.actors {
                if seen.insert(a.clone()) && actors_sample.len() < 50 {
                    actors_sample.push(a.clone());
                }
            }
        }
    }

    // Merge attributes
    let mut attributes = merge_attributes(evicted);

    // Store original actor set in attributes (not as attestation actors)
    attributes.insert(
        "_actors_count".to_string(),
        Value::Number((actors_sample.len() as u64).into()),
    );
    attributes.insert(
        "_actors_sample".to_string(),
        Value::Array(actors_sample.into_iter().map(Value::String).collect()),
    );

    Attestation {
        id,
        subjects: subjects_set,
        predicates,
        contexts: vec![context.to_string()],
        actors: vec!["distill".to_string()],
        timestamp: now_ms,
        source: "distill".to_string(),
        attributes,
        created_at: now_ms,
        signature: None,
        signer_did: None,
    }
}

/// Merge attributes from a batch of attestations using mechanical defaults:
///
/// - **Number** → `{min, max, sum, count}`
/// - **String** → `{values: [...unique...], count: N}` (values capped at 50)
/// - **Constant** (same value in every attestation) → kept as scalar
/// - **Already-aggregated** (`_distill: true` attestations) → merge aggregates
/// - **Null/missing** → tracked via presence count
///
/// Also injects `_distill`, `_count`, `_first_seen`, `_last_seen`.
pub fn merge_attributes(attestations: &[Attestation]) -> HashMap<String, Value> {
    let total = attestations.len();
    if total == 0 {
        return HashMap::new();
    }

    // Collect all attribute keys
    let mut all_keys = HashSet::new();
    for att in attestations {
        for key in att.attributes.keys() {
            // Skip internal distill metadata — we regenerate these
            if key == "_distill"
                || key == "_count"
                || key == "_total"
                || key == "_first_seen"
                || key == "_last_seen"
                || key == "_subjects_count"
                || key == "_subjects_sample"
                || key == "_actors_count"
                || key == "_actors_sample"
            {
                continue;
            }
            all_keys.insert(key.clone());
        }
    }

    let mut result = HashMap::new();

    for key in &all_keys {
        let values: Vec<Option<&Value>> = attestations
            .iter()
            .map(|att| att.attributes.get(key.as_str()))
            .collect();

        let merged = merge_single_attribute(&values, total);
        result.insert(key.clone(), merged);
    }

    // Inject distill metadata
    result.insert("_distill".to_string(), Value::Bool(true));

    // _count: batch size (how many attestations were in this distill cycle)
    result.insert(
        "_count".to_string(),
        Value::Number((attestations.len() as u64).into()),
    );

    // _total: transitive total of original observations this attestation represents.
    // For raw attestations, each counts as 1. For prior distill attestations,
    // use their _total (or fall back to _count for pre-_total distill attestations).
    // Note: Go stores numbers as float64 — as_u64() returns None for floats,
    // so we fall back to as_f64() to handle Go-produced sigmas.
    let total: u64 = attestations
        .iter()
        .map(|att| {
            att.attributes
                .get("_total")
                .and_then(|v| v.as_u64().or_else(|| v.as_f64().map(|f| f as u64)))
                .or_else(|| {
                    att.attributes
                        .get("_count")
                        .and_then(|v| v.as_u64().or_else(|| v.as_f64().map(|f| f as u64)))
                })
                .unwrap_or(1)
        })
        .sum();
    result.insert("_total".to_string(), Value::Number(total.into()));

    // _first_seen / _last_seen from timestamps
    let mut first = i64::MAX;
    let mut last = i64::MIN;
    for att in attestations {
        // For meta-distill, use _first_seen/_last_seen if present
        let att_first = att
            .attributes
            .get("_first_seen")
            .and_then(|v| v.as_str())
            .and_then(|s| chrono::DateTime::parse_from_rfc3339(s).ok())
            .map(|dt| dt.timestamp_millis())
            .unwrap_or(att.timestamp);
        let att_last = att
            .attributes
            .get("_last_seen")
            .and_then(|v| v.as_str())
            .and_then(|s| chrono::DateTime::parse_from_rfc3339(s).ok())
            .map(|dt| dt.timestamp_millis())
            .unwrap_or(att.timestamp);

        if att_first < first {
            first = att_first;
        }
        if att_last > last {
            last = att_last;
        }
    }
    if first != i64::MAX {
        let dt = chrono::DateTime::from_timestamp_millis(first)
            .unwrap_or_default()
            .to_rfc3339();
        result.insert("_first_seen".to_string(), Value::String(dt));
    }
    if last != i64::MIN {
        let dt = chrono::DateTime::from_timestamp_millis(last)
            .unwrap_or_default()
            .to_rfc3339();
        result.insert("_last_seen".to_string(), Value::String(dt));
    }

    result
}

/// Merge values for a single attribute key across all attestations.
fn merge_single_attribute(values: &[Option<&Value>], total: usize) -> Value {
    let present: Vec<&Value> = values.iter().filter_map(|v| *v).collect();
    let presence_count = present.len();

    if present.is_empty() {
        return Value::Null;
    }

    // Check if all present values are identical → keep as scalar constant
    if present.iter().all(|v| *v == present[0]) && presence_count == total {
        return present[0].clone();
    }

    // Check if any value is an already-aggregated object (from prior distill)
    let has_aggregated = present.iter().any(|v| is_numeric_aggregate(v));
    let has_string_aggregate = present.iter().any(|v| is_string_aggregate(v));

    // Determine the dominant type
    let all_numbers = present
        .iter()
        .all(|v| v.is_number() || is_numeric_aggregate(v));
    let all_strings = present
        .iter()
        .all(|v| v.is_string() || is_string_aggregate(v));

    if all_numbers {
        merge_numbers(&present, has_aggregated, presence_count, total)
    } else if all_strings {
        merge_strings(&present, has_string_aggregate, presence_count, total)
    } else {
        // Mixed types — treat everything as strings
        let string_vals: Vec<Value> = present
            .iter()
            .map(|v| {
                if v.is_string() {
                    (*v).clone()
                } else {
                    Value::String(v.to_string())
                }
            })
            .collect();
        let string_refs: Vec<&Value> = string_vals.iter().collect();
        merge_strings(&string_refs, false, presence_count, total)
    }
}

/// Check if a value is a numeric aggregate `{min, max, sum, count}`.
fn is_numeric_aggregate(v: &Value) -> bool {
    v.is_object()
        && v.get("min").is_some()
        && v.get("max").is_some()
        && v.get("sum").is_some()
        && v.get("count").is_some()
}

/// Check if a value is a string aggregate `{values, count}`.
fn is_string_aggregate(v: &Value) -> bool {
    v.is_object() && v.get("count").is_some() && v.get("values").is_some()
}

/// Merge numeric values, handling both raw numbers and prior aggregates.
fn merge_numbers(
    present: &[&Value],
    has_aggregated: bool,
    presence_count: usize,
    total: usize,
) -> Value {
    let mut min = f64::INFINITY;
    let mut max = f64::NEG_INFINITY;
    let mut sum = 0.0f64;
    let mut count = 0u64;

    for v in present {
        if has_aggregated && is_numeric_aggregate(v) {
            // Merge with prior aggregate
            let a_min = v.get("min").and_then(|n| n.as_f64()).unwrap_or(0.0);
            let a_max = v.get("max").and_then(|n| n.as_f64()).unwrap_or(0.0);
            let a_sum = v.get("sum").and_then(|n| n.as_f64()).unwrap_or(0.0);
            let a_count = v.get("count").and_then(|n| n.as_u64()).unwrap_or(0);
            if a_min < min {
                min = a_min;
            }
            if a_max > max {
                max = a_max;
            }
            sum += a_sum;
            count += a_count;
        } else if let Some(n) = v.as_f64() {
            if n < min {
                min = n;
            }
            if n > max {
                max = n;
            }
            sum += n;
            count += 1;
        }
    }

    let mut obj = serde_json::Map::new();
    obj.insert("min".to_string(), json_number(min));
    obj.insert("max".to_string(), json_number(max));
    obj.insert("sum".to_string(), json_number(sum));
    obj.insert("count".to_string(), Value::Number(count.into()));
    if presence_count < total {
        obj.insert("present".to_string(), Value::Number(presence_count.into()));
    }
    Value::Object(obj)
}

/// Convert f64 to a JSON number, using integer representation when possible.
fn json_number(n: f64) -> Value {
    if n.fract() == 0.0 && n.abs() < (i64::MAX as f64) {
        Value::Number((n as i64).into())
    } else {
        serde_json::Number::from_f64(n)
            .map(Value::Number)
            .unwrap_or(Value::Null)
    }
}

/// Merge string values, handling both raw strings and prior aggregates.
fn merge_strings(
    present: &[&Value],
    has_aggregated: bool,
    presence_count: usize,
    total: usize,
) -> Value {
    let mut unique_values: Vec<String> = Vec::new();
    let mut seen = HashSet::new();
    let mut count = 0u64;

    for v in present {
        if has_aggregated && is_string_aggregate(v) {
            let a_count = v.get("count").and_then(|n| n.as_u64()).unwrap_or(0);
            count += a_count;
            if let Some(vals) = v.get("values").and_then(|a| a.as_array()) {
                for val in vals {
                    if let Some(s) = val.as_str() {
                        if seen.len() < STRING_VALUES_CAP && seen.insert(s.to_string()) {
                            unique_values.push(s.to_string());
                        }
                    }
                }
            }
        } else if let Some(s) = v.as_str() {
            count += 1;
            if seen.len() < STRING_VALUES_CAP && seen.insert(s.to_string()) {
                unique_values.push(s.to_string());
            }
        }
    }

    let mut obj = serde_json::Map::new();
    obj.insert(
        "values".to_string(),
        Value::Array(unique_values.into_iter().map(Value::String).collect()),
    );
    obj.insert("count".to_string(), Value::Number(count.into()));
    if presence_count < total {
        obj.insert("present".to_string(), Value::Number(presence_count.into()));
    }
    Value::Object(obj)
}

#[cfg(test)]
mod tests {
    use super::*;
    use qntx_core::attestation::AttestationBuilder;

    #[test]
    fn test_merge_numbers() {
        let atts = vec![
            AttestationBuilder::new()
                .id("AS-1")
                .subject("X")
                .attribute("elapsed_ms".to_string(), Value::Number(100.into()))
                .build(),
            AttestationBuilder::new()
                .id("AS-2")
                .subject("X")
                .attribute("elapsed_ms".to_string(), Value::Number(200.into()))
                .build(),
            AttestationBuilder::new()
                .id("AS-3")
                .subject("X")
                .attribute("elapsed_ms".to_string(), Value::Number(50.into()))
                .build(),
        ];

        let merged = merge_attributes(&atts);
        let elapsed = merged.get("elapsed_ms").unwrap();
        assert_eq!(elapsed.get("min").unwrap(), &Value::Number(50.into()));
        assert_eq!(elapsed.get("max").unwrap(), &Value::Number(200.into()));
        assert_eq!(elapsed.get("sum").unwrap(), &Value::Number(350.into()));
        assert_eq!(elapsed.get("count").unwrap(), &Value::Number(3.into()));
        // All present, so no "present" field
        assert!(elapsed.get("present").is_none());
    }

    #[test]
    fn test_merge_strings() {
        let atts = vec![
            AttestationBuilder::new()
                .id("AS-1")
                .subject("X")
                .attribute("stage".to_string(), Value::String("connecting".into()))
                .build(),
            AttestationBuilder::new()
                .id("AS-2")
                .subject("X")
                .attribute("stage".to_string(), Value::String("discovered".into()))
                .build(),
            AttestationBuilder::new()
                .id("AS-3")
                .subject("X")
                .attribute("stage".to_string(), Value::String("connecting".into()))
                .build(),
        ];

        let merged = merge_attributes(&atts);
        let stage = merged.get("stage").unwrap();
        let values = stage.get("values").unwrap().as_array().unwrap();
        assert_eq!(values.len(), 2); // "connecting" and "discovered" deduplicated
        assert_eq!(stage.get("count").unwrap(), &Value::Number(3.into()));
    }

    #[test]
    fn test_constant_kept_as_scalar() {
        let atts = vec![
            AttestationBuilder::new()
                .id("AS-1")
                .subject("X")
                .attribute("dest_hash".to_string(), Value::String("abc123".into()))
                .build(),
            AttestationBuilder::new()
                .id("AS-2")
                .subject("X")
                .attribute("dest_hash".to_string(), Value::String("abc123".into()))
                .build(),
        ];

        let merged = merge_attributes(&atts);
        let dest = merged.get("dest_hash").unwrap();
        // Same value in all attestations → kept as scalar
        assert_eq!(dest, &Value::String("abc123".into()));
    }

    #[test]
    fn test_meta_distill_merges_aggregates() {
        // First distill produced these two attestations
        let mut attrs1 = HashMap::new();
        attrs1.insert("_distill".to_string(), Value::Bool(true));
        attrs1.insert("_count".to_string(), Value::Number(4.into()));
        attrs1.insert(
            "_first_seen".to_string(),
            Value::String("2026-05-01T00:00:00+00:00".into()),
        );
        attrs1.insert(
            "_last_seen".to_string(),
            Value::String("2026-05-05T00:00:00+00:00".into()),
        );
        let mut elapsed1 = serde_json::Map::new();
        elapsed1.insert("min".to_string(), Value::Number(10.into()));
        elapsed1.insert("max".to_string(), Value::Number(100.into()));
        elapsed1.insert("sum".to_string(), Value::Number(220.into()));
        elapsed1.insert("count".to_string(), Value::Number(4.into()));
        attrs1.insert("elapsed_ms".to_string(), Value::Object(elapsed1));

        let mut attrs2 = HashMap::new();
        attrs2.insert("_distill".to_string(), Value::Bool(true));
        attrs2.insert("_count".to_string(), Value::Number(6.into()));
        attrs2.insert(
            "_first_seen".to_string(),
            Value::String("2026-05-06T00:00:00+00:00".into()),
        );
        attrs2.insert(
            "_last_seen".to_string(),
            Value::String("2026-05-10T00:00:00+00:00".into()),
        );
        let mut elapsed2 = serde_json::Map::new();
        elapsed2.insert("min".to_string(), Value::Number(5.into()));
        elapsed2.insert("max".to_string(), Value::Number(200.into()));
        elapsed2.insert("sum".to_string(), Value::Number(500.into()));
        elapsed2.insert("count".to_string(), Value::Number(6.into()));
        attrs2.insert("elapsed_ms".to_string(), Value::Object(elapsed2));

        let att1 = Attestation {
            id: "AS-distill-1".into(),
            subjects: vec!["X".into()],
            predicates: vec!["crawl".into()],
            contexts: vec!["ctx".into()],
            actors: vec!["qntx".into()],
            timestamp: 1746057600000, // 2025-05-01
            source: "distill".into(),
            attributes: attrs1,
            created_at: 1746057600000,
            signature: None,
            signer_did: None,
        };
        let att2 = Attestation {
            id: "AS-distill-2".into(),
            subjects: vec!["X".into()],
            predicates: vec!["crawl".into()],
            contexts: vec!["ctx".into()],
            actors: vec!["qntx".into()],
            timestamp: 1746489600000, // 2025-05-06
            source: "distill".into(),
            attributes: attrs2,
            created_at: 1746489600000,
            signature: None,
            signer_did: None,
        };

        let merged = merge_attributes(&[att1, att2]);

        // _count is batch size (2 attestations in this merge)
        assert_eq!(merged.get("_count").unwrap(), &Value::Number(2.into()));
        // _total is transitive: 4 + 6 = 10 original observations
        assert_eq!(merged.get("_total").unwrap(), &Value::Number(10.into()));

        // _first_seen should be the earlier one
        assert_eq!(
            merged.get("_first_seen").unwrap(),
            &Value::String("2026-05-01T00:00:00+00:00".into())
        );
        assert_eq!(
            merged.get("_last_seen").unwrap(),
            &Value::String("2026-05-10T00:00:00+00:00".into())
        );

        // elapsed_ms should be merged: min=5, max=200, sum=720, count=10
        let elapsed = merged.get("elapsed_ms").unwrap();
        assert_eq!(elapsed.get("min").unwrap(), &Value::Number(5.into()));
        assert_eq!(elapsed.get("max").unwrap(), &Value::Number(200.into()));
        assert_eq!(elapsed.get("sum").unwrap(), &Value::Number(720.into()));
        assert_eq!(elapsed.get("count").unwrap(), &Value::Number(10.into()));
    }

    #[test]
    fn test_missing_attribute_tracked() {
        let atts = vec![
            AttestationBuilder::new()
                .id("AS-1")
                .subject("X")
                .attribute("optional".to_string(), Value::Number(42.into()))
                .build(),
            AttestationBuilder::new().id("AS-2").subject("X").build(),
        ];

        let merged = merge_attributes(&atts);
        let opt = merged.get("optional").unwrap();
        // Only present in 1 of 2, so should have "present" field
        assert_eq!(opt.get("present").unwrap(), &Value::Number(1.into()));
        assert_eq!(opt.get("min").unwrap(), &Value::Number(42.into()));
        assert_eq!(opt.get("max").unwrap(), &Value::Number(42.into()));
    }

    #[test]
    fn test_build_distill_attestation_shape() {
        let atts = vec![
            AttestationBuilder::new()
                .id("AS-1")
                .subject("X")
                .subject("Y")
                .predicate("crawl-stage-changed")
                .actor("bot")
                .context("ctx1")
                .timestamp(1000)
                .build(),
            AttestationBuilder::new()
                .id("AS-2")
                .subject("X")
                .predicate("announced")
                .actor("bot")
                .context("ctx1")
                .timestamp(2000)
                .build(),
        ];

        let distill = build_distill_attestation(&atts, "ctx1");

        assert!(distill.id.starts_with("AS-distill-"));
        assert_eq!(distill.source, "distill");
        assert_eq!(distill.actors, vec!["distill"]);
        assert_eq!(distill.contexts, vec!["ctx1"]);

        // Subjects: union of {X, Y} and {X} = {X, Y}
        assert!(distill.subjects.contains(&"X".to_string()));
        assert!(distill.subjects.contains(&"Y".to_string()));
        assert_eq!(distill.subjects.len(), 2);

        // Predicates: original names preserved
        assert!(distill
            .predicates
            .contains(&"crawl-stage-changed".to_string()));
        assert!(distill.predicates.contains(&"announced".to_string()));

        // Metadata
        assert_eq!(
            distill.attributes.get("_distill").unwrap(),
            &Value::Bool(true)
        );
        assert_eq!(
            distill.attributes.get("_count").unwrap(),
            &Value::Number(2.into())
        );
        assert_eq!(
            distill.attributes.get("_total").unwrap(),
            &Value::Number(2.into()) // no prior distills, so _total == _count
        );
    }

    #[test]
    fn test_string_values_cap() {
        // Create attestations with many distinct string values
        let atts: Vec<Attestation> = (0..60)
            .map(|i| {
                AttestationBuilder::new()
                    .id(format!("AS-{}", i))
                    .subject("X")
                    .attribute("tag".to_string(), Value::String(format!("tag-{}", i)))
                    .build()
            })
            .collect();

        let merged = merge_attributes(&atts);
        let tag = merged.get("tag").unwrap();
        let values = tag.get("values").unwrap().as_array().unwrap();
        // Capped at 50 unique values
        assert_eq!(values.len(), STRING_VALUES_CAP);
        // But count reflects all 60
        assert_eq!(tag.get("count").unwrap(), &Value::Number(60.into()));
    }

    #[test]
    fn test_total_from_float64_values() {
        // Go stores numbers as float64 — serde_json::Value::as_u64() returns None
        // for float-typed numbers. This test verifies _total is correctly read
        // from Go-produced sigmas where _total and _count are floats.
        let mut attrs1 = HashMap::new();
        attrs1.insert("_distill".to_string(), Value::Bool(true));
        attrs1.insert(
            "_count".to_string(),
            serde_json::Number::from_f64(2101.0).map(Value::Number).unwrap(),
        );
        attrs1.insert(
            "_total".to_string(),
            serde_json::Number::from_f64(2101.0).map(Value::Number).unwrap(),
        );

        let att1 = Attestation {
            id: "AS-distill-go-1".into(),
            subjects: vec!["X".into()],
            predicates: vec!["path-found".into()],
            contexts: vec!["_distill".into()],
            actors: vec!["qntx".into()],
            timestamp: 1746057600000,
            source: "distill".into(),
            attributes: attrs1,
            created_at: 1746057600000,
            signature: None,
            signer_did: None,
        };

        let mut attrs2 = HashMap::new();
        attrs2.insert("_distill".to_string(), Value::Bool(true));
        attrs2.insert(
            "_count".to_string(),
            serde_json::Number::from_f64(500.0).map(Value::Number).unwrap(),
        );
        attrs2.insert(
            "_total".to_string(),
            serde_json::Number::from_f64(500.0).map(Value::Number).unwrap(),
        );

        let att2 = Attestation {
            id: "AS-distill-go-2".into(),
            subjects: vec!["X".into()],
            predicates: vec!["path-found".into()],
            contexts: vec!["_distill".into()],
            actors: vec!["qntx".into()],
            timestamp: 1746489600000,
            source: "distill".into(),
            attributes: attrs2,
            created_at: 1746489600000,
            signature: None,
            signer_did: None,
        };

        let merged = merge_attributes(&[att1, att2]);

        // _total must be 2101 + 500 = 2601, NOT 2 (fallback to 1 per attestation)
        assert_eq!(merged.get("_total").unwrap(), &Value::Number(2601u64.into()));
        // _count is batch size
        assert_eq!(merged.get("_count").unwrap(), &Value::Number(2.into()));
    }
}
