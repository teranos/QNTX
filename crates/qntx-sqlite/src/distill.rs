//! Attestation distillation: fold evicted attestations into sigmas (Σ).
//!
//! When enforcement evicts a batch of attestations, distillation preserves their
//! aggregate data in a single sigma before deletion. A sigma is a normal
//! attestation — it participates in enforcement like any other, enabling
//! recursive meta-distillation (sigma of sigmas).

use chrono::Datelike;
use qntx_core::attestation::Attestation;
use serde_json::Value;
use std::collections::{HashMap, HashSet};

/// Cap on unique string values collected per attribute before switching to count-only.
const STRING_VALUES_CAP: usize = 50;

/// Maximum histogram keys before coarsening to next tier.
const HISTOGRAM_KEY_BUDGET: usize = 200;

/// Build a sigma (Σ) from a batch of evicted attestations.
///
/// The sigma captures:
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
        actors: vec!["system:distill".to_string()],
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
/// - **String** → `{frequencies: {val: count, ...}, count: N}` (entries capped at 50)
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
                || key == "_histogram"
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

    // Build temporal histogram from attestation timestamps
    let histogram = build_histogram(attestations);
    if !histogram.is_empty() {
        let map: serde_json::Map<String, Value> = histogram
            .into_iter()
            .map(|(k, v)| (k, Value::Number(v.into())))
            .collect();
        result.insert("_histogram".to_string(), Value::Object(map));
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

/// Check if a value is a string aggregate `{frequencies, count}` or legacy `{values, count}`.
fn is_string_aggregate(v: &Value) -> bool {
    v.is_object()
        && v.get("count").is_some()
        && (v.get("frequencies").is_some() || v.get("values").is_some())
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
///
/// Output format: `{frequencies: {"val": count, ...}, count: N}`
/// - Raw strings contribute frequency 1 each.
/// - New-format aggregates (`{frequencies, count}`) merge by summing frequencies.
/// - Old-format aggregates (`{values, count}`) pass values through without
///   fabricating frequencies — the values are known to exist but their
///   individual counts are not.
fn merge_strings(
    present: &[&Value],
    has_aggregated: bool,
    presence_count: usize,
    total: usize,
) -> Value {
    let mut frequencies: HashMap<String, u64> = HashMap::new();
    let mut unplaced_values: Vec<String> = Vec::new();
    let mut unplaced_seen = HashSet::new();
    let mut count = 0u64;

    for v in present {
        if has_aggregated && is_string_aggregate(v) {
            let a_count = v.get("count").and_then(|n| n.as_u64()).unwrap_or(0);
            count += a_count;

            if let Some(freqs) = v.get("frequencies").and_then(|f| f.as_object()) {
                // New format: merge frequencies by summing
                for (key, val) in freqs {
                    let freq = val.as_u64().or_else(|| val.as_f64().map(|f| f as u64)).unwrap_or(0);
                    *frequencies.entry(key.clone()).or_insert(0) += freq;
                }
            } else if let Some(vals) = v.get("values").and_then(|a| a.as_array()) {
                // Old format: values without frequencies — track as unplaced
                for val in vals {
                    if let Some(s) = val.as_str() {
                        if unplaced_seen.len() < STRING_VALUES_CAP
                            && unplaced_seen.insert(s.to_string())
                            && !frequencies.contains_key(s)
                        {
                            unplaced_values.push(s.to_string());
                        }
                    }
                }
            }
        } else if let Some(s) = v.as_str() {
            count += 1;
            *frequencies.entry(s.to_string()).or_insert(0) += 1;
        }
    }

    // Cap frequencies at STRING_VALUES_CAP entries
    let mut freq_map = serde_json::Map::new();
    let mut freq_entries: Vec<(String, u64)> = frequencies.into_iter().collect();
    freq_entries.sort_by_key(|b| std::cmp::Reverse(b.1)); // highest frequency first
    for (key, val) in freq_entries.into_iter().take(STRING_VALUES_CAP) {
        freq_map.insert(key, Value::Number(val.into()));
    }

    let mut obj = serde_json::Map::new();
    obj.insert("frequencies".to_string(), Value::Object(freq_map));
    obj.insert("count".to_string(), Value::Number(count.into()));

    // Preserve unplaced values from old-format aggregates
    if !unplaced_values.is_empty() {
        obj.insert(
            "unplaced".to_string(),
            Value::Array(unplaced_values.into_iter().map(Value::String).collect()),
        );
    }

    if presence_count < total {
        obj.insert("present".to_string(), Value::Number(presence_count.into()));
    }
    Value::Object(obj)
}

/// Build a temporal histogram from attestation timestamps.
///
/// Raw attestations get their timestamp bucketed into 10min keys.
/// Existing distill attestations with `_histogram` get their histograms merged.
/// Existing distill attestations without `_histogram` contribute nothing
/// (their `_total` is still counted — `sum(histogram) <= _total` is expected).
fn build_histogram(attestations: &[Attestation]) -> HashMap<String, u64> {
    let mut histogram: HashMap<String, u64> = HashMap::new();

    for att in attestations {
        if let Some(existing) = att.attributes.get("_histogram").and_then(|v| v.as_object()) {
            // Merge existing histogram from prior distill
            for (key, val) in existing {
                if let Some(count) = val.as_u64().or_else(|| val.as_f64().map(|f| f as u64)) {
                    *histogram.entry(key.clone()).or_insert(0) += count;
                }
            }
        } else if !att.attributes.contains_key("_distill") {
            // Raw attestation — bucket its timestamp
            let ts = if att.timestamp > 0 {
                att.timestamp
            } else {
                att.created_at
            };
            let key = timestamp_to_10min_key(ts);
            *histogram.entry(key).or_insert(0) += 1;
        }
        // Distill attestation without _histogram: unplaced, contributes to _total only
    }

    coarsen_histogram(histogram)
}

/// Convert a millisecond timestamp to a 10-minute bucket key.
/// Format: "2026-05-13T14:10" (truncated to 10min boundary).
fn timestamp_to_10min_key(ts_ms: i64) -> String {
    let dt = chrono::DateTime::from_timestamp_millis(ts_ms).unwrap_or_default();
    let minute = (dt.format("%M").to_string().parse::<u32>().unwrap_or(0) / 10) * 10;
    format!("{}:{:02}", dt.format("%Y-%m-%dT%H"), minute)
}

/// Coarsen a histogram if it exceeds the key budget.
///
/// Tiers by key length:
/// - 16 chars: 10min ("2026-05-13T14:10")
/// - 13 chars: hourly ("2026-05-13T14")
/// - 10 chars: daily  ("2026-05-13")
/// -  8 chars: weekly ("2026-W20")
///
/// Collapses the finest tier by prefix-grouping and summing.
/// Recurses if still over budget after one collapse.
fn coarsen_histogram(histogram: HashMap<String, u64>) -> HashMap<String, u64> {
    if histogram.len() <= HISTOGRAM_KEY_BUDGET {
        return histogram;
    }

    // Find the finest tier (longest key)
    let max_len = histogram.keys().map(|k| k.len()).max().unwrap_or(0);

    let mut coarsened: HashMap<String, u64> = HashMap::new();

    for (key, count) in histogram {
        if key.len() == max_len {
            // Collapse to next coarser tier
            let coarse_key = coarsen_key(&key);
            *coarsened.entry(coarse_key).or_insert(0) += count;
        } else {
            *coarsened.entry(key).or_insert(0) += count;
        }
    }

    // Recurse if still over budget
    if coarsened.len() > HISTOGRAM_KEY_BUDGET {
        coarsen_histogram(coarsened)
    } else {
        coarsened
    }
}

/// Collapse a histogram key to the next coarser tier.
///
/// "2026-05-13T14:10" (10min) → "2026-05-13T14" (hourly)
/// "2026-05-13T14"    (hourly) → "2026-05-13"    (daily)
/// "2026-05-13"       (daily)  → ISO week key     (weekly)
fn coarsen_key(key: &str) -> String {
    match key.len() {
        16 => key[..13].to_string(), // 10min → hourly: drop ":MM"
        13 => key[..10].to_string(), // hourly → daily: drop "THH"
        10 => {
            // daily → weekly
            if let Ok(date) = chrono::NaiveDate::parse_from_str(key, "%Y-%m-%d") {
                let iso = date.iso_week();
                format!("{}-W{:02}", iso.year(), iso.week())
            } else {
                key.to_string()
            }
        }
        _ => key.to_string(), // already at coarsest or unknown format
    }
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
        let freqs = stage.get("frequencies").unwrap().as_object().unwrap();
        assert_eq!(freqs.len(), 2); // "connecting" and "discovered"
        assert_eq!(freqs.get("connecting").unwrap(), &Value::Number(2.into()));
        assert_eq!(freqs.get("discovered").unwrap(), &Value::Number(1.into()));
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
        assert_eq!(distill.actors, vec!["system:distill"]);
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
    fn test_string_frequencies_cap() {
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
        let freqs = tag.get("frequencies").unwrap().as_object().unwrap();
        // Capped at 50 entries
        assert_eq!(freqs.len(), STRING_VALUES_CAP);
        // But count reflects all 60
        assert_eq!(tag.get("count").unwrap(), &Value::Number(60.into()));
    }

    #[test]
    fn test_histogram_from_raw_attestations() {
        let atts = vec![
            AttestationBuilder::new()
                .id("AS-1")
                .subject("X")
                .timestamp(1747137000000) // 2025-05-13T11:50:00 UTC
                .build(),
            AttestationBuilder::new()
                .id("AS-2")
                .subject("X")
                .timestamp(1747137060000) // 2025-05-13T11:51:00 UTC (same 10min bucket)
                .build(),
            AttestationBuilder::new()
                .id("AS-3")
                .subject("X")
                .timestamp(1747137600000) // 2025-05-13T12:00:00 UTC (next bucket)
                .build(),
        ];

        let merged = merge_attributes(&atts);
        let hist = merged.get("_histogram").unwrap().as_object().unwrap();

        // Two buckets: 11:50 (2 attestations) and 12:00 (1 attestation)
        assert_eq!(hist.len(), 2);
        assert_eq!(
            hist.get("2025-05-13T11:50").unwrap(),
            &Value::Number(2.into())
        );
        assert_eq!(
            hist.get("2025-05-13T12:00").unwrap(),
            &Value::Number(1.into())
        );
    }

    #[test]
    fn test_histogram_meta_distill_merges() {
        let mut attrs1 = HashMap::new();
        attrs1.insert("_distill".to_string(), Value::Bool(true));
        attrs1.insert("_count".to_string(), Value::Number(3.into()));
        attrs1.insert("_total".to_string(), Value::Number(3.into()));
        let mut hist1 = serde_json::Map::new();
        hist1.insert("2025-05-13T14:10".to_string(), Value::Number(2.into()));
        hist1.insert("2025-05-13T14:20".to_string(), Value::Number(1.into()));
        attrs1.insert("_histogram".to_string(), Value::Object(hist1));

        let mut attrs2 = HashMap::new();
        attrs2.insert("_distill".to_string(), Value::Bool(true));
        attrs2.insert("_count".to_string(), Value::Number(5.into()));
        attrs2.insert("_total".to_string(), Value::Number(5.into()));
        let mut hist2 = serde_json::Map::new();
        hist2.insert("2025-05-13T14:10".to_string(), Value::Number(3.into()));
        hist2.insert("2025-05-13T14:30".to_string(), Value::Number(2.into()));
        attrs2.insert("_histogram".to_string(), Value::Object(hist2));

        let att1 = Attestation {
            id: "AS-distill-1".into(),
            subjects: vec!["X".into()],
            predicates: vec!["test".into()],
            contexts: vec!["ctx".into()],
            actors: vec!["system:distill".into()],
            timestamp: 1747137000000,
            source: "distill".into(),
            attributes: attrs1,
            created_at: 1747137000000,
            signature: None,
            signer_did: None,
        };
        let att2 = Attestation {
            id: "AS-distill-2".into(),
            subjects: vec!["X".into()],
            predicates: vec!["test".into()],
            contexts: vec!["ctx".into()],
            actors: vec!["system:distill".into()],
            timestamp: 1747138200000,
            source: "distill".into(),
            attributes: attrs2,
            created_at: 1747138200000,
            signature: None,
            signer_did: None,
        };

        let merged = merge_attributes(&[att1, att2]);
        let hist = merged.get("_histogram").unwrap().as_object().unwrap();

        // 14:10 merged: 2 + 3 = 5
        assert_eq!(
            hist.get("2025-05-13T14:10").unwrap(),
            &Value::Number(5.into())
        );
        // 14:20 from first only
        assert_eq!(
            hist.get("2025-05-13T14:20").unwrap(),
            &Value::Number(1.into())
        );
        // 14:30 from second only
        assert_eq!(
            hist.get("2025-05-13T14:30").unwrap(),
            &Value::Number(2.into())
        );
        assert_eq!(hist.len(), 3);
    }

    #[test]
    fn test_histogram_legacy_sigma_unplaced() {
        // Old sigma without _histogram — its observations stay unplaced
        let mut attrs1 = HashMap::new();
        attrs1.insert("_distill".to_string(), Value::Bool(true));
        attrs1.insert("_count".to_string(), Value::Number(100.into()));
        attrs1.insert("_total".to_string(), Value::Number(100.into()));
        // No _histogram

        let att1 = Attestation {
            id: "AS-distill-old".into(),
            subjects: vec!["X".into()],
            predicates: vec!["test".into()],
            contexts: vec!["ctx".into()],
            actors: vec!["system:distill".into()],
            timestamp: 1747137000000,
            source: "distill".into(),
            attributes: attrs1,
            created_at: 1747137000000,
            signature: None,
            signer_did: None,
        };

        // New raw attestation with timestamp
        let att2 = AttestationBuilder::new()
            .id("AS-new")
            .subject("X")
            .timestamp(1747137600000) // 2025-05-13T12:00 UTC
            .build();

        let merged = merge_attributes(&[att1, att2]);

        // _total = 100 + 1 = 101
        assert_eq!(merged.get("_total").unwrap(), &Value::Number(101u64.into()));

        // Histogram only has the new attestation's bucket
        let hist = merged.get("_histogram").unwrap().as_object().unwrap();
        assert_eq!(hist.len(), 1);
        assert_eq!(
            hist.get("2025-05-13T12:00").unwrap(),
            &Value::Number(1.into())
        );

        // sum(histogram) = 1, _total = 101 — gap of 100 is unplaced legacy
    }

    #[test]
    fn test_coarsen_10min_to_hourly() {
        let mut hist = HashMap::new();
        // 201 keys at 10min resolution (exceeds budget of 200)
        for hour in 0..4 {
            for ten_min in 0..6 {
                for day in 1..=9 {
                    let key = format!("2025-05-{:02}T{:02}:{:02}", day, hour, ten_min * 10);
                    hist.insert(key, 1u64);
                    if hist.len() > HISTOGRAM_KEY_BUDGET {
                        break;
                    }
                }
                if hist.len() > HISTOGRAM_KEY_BUDGET {
                    break;
                }
            }
            if hist.len() > HISTOGRAM_KEY_BUDGET {
                break;
            }
        }
        assert!(hist.len() > HISTOGRAM_KEY_BUDGET);

        let coarsened = coarsen_histogram(hist);

        // All keys should be hourly (13 chars) now
        assert!(coarsened.len() <= HISTOGRAM_KEY_BUDGET);
        for key in coarsened.keys() {
            assert_eq!(key.len(), 13, "Expected hourly key, got: {}", key);
        }
    }

    #[test]
    fn test_coarsen_key_tiers() {
        assert_eq!(coarsen_key("2025-05-13T14:10"), "2025-05-13T14"); // 10min → hourly
        assert_eq!(coarsen_key("2025-05-13T14"), "2025-05-13"); // hourly → daily
        assert_eq!(coarsen_key("2025-05-13"), "2025-W20"); // daily → weekly
    }

    #[test]
    fn test_old_format_string_agg_becomes_unplaced() {
        // Old-format {values, count} from prior distill — values become unplaced
        let mut attrs1 = HashMap::new();
        attrs1.insert("_distill".to_string(), Value::Bool(true));
        attrs1.insert("_count".to_string(), Value::Number(5.into()));
        let mut old_agg = serde_json::Map::new();
        old_agg.insert(
            "values".to_string(),
            Value::Array(vec![
                Value::String("connecting".into()),
                Value::String("timeout".into()),
            ]),
        );
        old_agg.insert("count".to_string(), Value::Number(5.into()));
        attrs1.insert("stage".to_string(), Value::Object(old_agg));

        let att1 = Attestation {
            id: "AS-distill-old".into(),
            subjects: vec!["X".into()],
            predicates: vec!["test".into()],
            contexts: vec!["ctx".into()],
            actors: vec!["system:distill".into()],
            timestamp: 1747137000000,
            source: "distill".into(),
            attributes: attrs1,
            created_at: 1747137000000,
            signature: None,
            signer_did: None,
        };

        // New raw attestation with a string value
        let att2 = AttestationBuilder::new()
            .id("AS-new")
            .subject("X")
            .attribute("stage".to_string(), Value::String("connecting".into()))
            .build();

        let merged = merge_attributes(&[att1, att2]);
        let stage = merged.get("stage").unwrap();

        // "connecting" from raw attestation has frequency 1
        let freqs = stage.get("frequencies").unwrap().as_object().unwrap();
        assert_eq!(freqs.get("connecting").unwrap(), &Value::Number(1.into()));

        // "timeout" from old format has no frequency — it's unplaced
        assert!(freqs.get("timeout").is_none());
        let unplaced = stage.get("unplaced").unwrap().as_array().unwrap();
        assert!(unplaced.contains(&Value::String("timeout".into())));

        // count = 5 (old) + 1 (new) = 6
        assert_eq!(stage.get("count").unwrap(), &Value::Number(6.into()));
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
            serde_json::Number::from_f64(2101.0)
                .map(Value::Number)
                .unwrap(),
        );
        attrs1.insert(
            "_total".to_string(),
            serde_json::Number::from_f64(2101.0)
                .map(Value::Number)
                .unwrap(),
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
            serde_json::Number::from_f64(500.0)
                .map(Value::Number)
                .unwrap(),
        );
        attrs2.insert(
            "_total".to_string(),
            serde_json::Number::from_f64(500.0)
                .map(Value::Number)
                .unwrap(),
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
        assert_eq!(
            merged.get("_total").unwrap(),
            &Value::Number(2601u64.into())
        );
        // _count is batch size
        assert_eq!(merged.get("_count").unwrap(), &Value::Number(2.into()));
    }
}
