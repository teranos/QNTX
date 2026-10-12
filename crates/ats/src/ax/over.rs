//! `over`: longer than.
//!
//! "over means longer than"
//!
//! "An attested type should designate what we see as start and end for the
//! purpose of over, like rich_string"
//!
//! An attestation's type is its predicate: `prompt-result` attestations are of
//! the `prompt-result` type. A type that names a `start_field` and an
//! `end_field` among its attributes says where each of its attestations starts
//! and ends, and `over 5y` keeps the ones whose end is more than five years
//! after their start, counted on the calendar ([`Span::step`]). An attestation
//! whose type names no start and end, or that carries no time in either field,
//! has no length to be longer than, so it is not over.

use std::collections::{BTreeSet, HashMap};

use crate::attestation::{Attestation, AxFilter, EVERY_ROW};
use crate::span::Span;
use crate::temporal::parse_iso_date;

/// The predicate a type definition is attested with: `[name] is type`.
pub const TYPE_PREDICATE: &str = "type";

/// The attribute of a type definition naming where its attestations start.
pub const START_FIELD: &str = "start_field";

/// The attribute of a type definition naming where its attestations end.
pub const END_FIELD: &str = "end_field";

/// Where a type's attestations start and end: the attributes that hold each.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Ends {
    pub start: String,
    pub end: String,
}

/// The filter that reads the definitions of every type `attestations` are of.
pub fn types_of(attestations: &[Attestation]) -> AxFilter {
    let names: BTreeSet<&String> = attestations.iter().flat_map(|a| &a.predicates).collect();
    AxFilter {
        subjects: names.into_iter().cloned().collect(),
        predicates: vec![TYPE_PREDICATE.to_string()],
        limit: EVERY_ROW,
        ..Default::default()
    }
}

/// The start and end each type names, from its type definitions. The latest
/// definition of a type is the one that holds, as it is everywhere a type is
/// read; one naming no start and end gives its type no length.
pub fn ends_of(definitions: &[Attestation]) -> HashMap<String, Ends> {
    let mut latest: HashMap<&str, &Attestation> = HashMap::new();
    for def in definitions {
        if !def.predicates.iter().any(|p| p == TYPE_PREDICATE) {
            continue;
        }
        for name in &def.subjects {
            let newer = match latest.get(name.as_str()) {
                Some(held) => def.timestamp > held.timestamp,
                None => true,
            };
            if newer {
                latest.insert(name, def);
            }
        }
    }

    let field = |def: &Attestation, key: &str| match def.attributes.get(key) {
        Some(serde_json::Value::String(name)) => Some(name.clone()),
        Some(_) | None => None,
    };

    latest
        .into_iter()
        .filter_map(|(name, def)| {
            let ends = Ends {
                start: field(def, START_FIELD)?,
                end: field(def, END_FIELD)?,
            };
            Some((name.to_string(), ends))
        })
        .collect()
}

/// The rows `filter` names, read through `read`, with `over` applied: when it
/// asks for one, every row the rest of the filter names is read, the
/// definitions of their types are read the same way, the longer than the span
/// kept, then the limit. A filter asking none is read as it is.
pub fn read_over<E>(
    filter: &AxFilter,
    mut read: impl FnMut(&AxFilter) -> Result<Vec<Attestation>, E>,
) -> Result<Vec<Attestation>, E> {
    let Some(span) = &filter.over else {
        return read(filter);
    };
    let rows = read(&AxFilter {
        limit: EVERY_ROW,
        over: None,
        ..filter.clone()
    })?;
    let definitions = read(&types_of(&rows))?;
    let mut kept = keep_over(rows, span, &ends_of(&definitions));
    kept.truncate(filter.limit);
    Ok(kept)
}

/// Whether `attestation` is longer than `span` by any type it is of.
pub fn is_over(attestation: &Attestation, span: &Span, ends: &HashMap<String, Ends>) -> bool {
    attestation
        .predicates
        .iter()
        .filter_map(|p| ends.get(p))
        .any(|e| longer(attestation, span, e))
}

/// The attestations longer than `span`, in the order given.
pub fn keep_over(
    attestations: Vec<Attestation>,
    span: &Span,
    ends: &HashMap<String, Ends>,
) -> Vec<Attestation> {
    attestations
        .into_iter()
        .filter(|a| is_over(a, span, ends))
        .collect()
}

/// End is more than `span` after start. A field holding no time it can read
/// gives the attestation no length.
fn longer(attestation: &Attestation, span: &Span, ends: &Ends) -> bool {
    let moment = |field: &str| match attestation.attributes.get(field) {
        Some(serde_json::Value::String(at)) => parse_iso_date(at),
        Some(_) | None => None,
    };
    let (Some(start), Some(end)) = (moment(&ends.start), moment(&ends.end)) else {
        return false;
    };
    match span.step(start, 1) {
        Some(reached) => end > reached,
        None => false,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn attestation(
        id: &str,
        predicate: &str,
        timestamp: i64,
        attrs: serde_json::Value,
    ) -> Attestation {
        Attestation {
            id: id.into(),
            subjects: vec!["ALICE".into()],
            predicates: vec![predicate.into()],
            contexts: vec![],
            actors: vec!["bob".into()],
            timestamp,
            source: "test".into(),
            attributes: serde_json::from_value(attrs).unwrap(),
            created_at: timestamp,
            signature: None,
            signer_did: None,
        }
    }

    fn definition(name: &str, timestamp: i64, attrs: serde_json::Value) -> Attestation {
        Attestation {
            id: format!("type-{name}-{timestamp}"),
            subjects: vec![name.into()],
            predicates: vec![TYPE_PREDICATE.into()],
            contexts: vec![],
            actors: vec![name.into()],
            timestamp,
            source: "test".into(),
            attributes: serde_json::from_value(attrs).unwrap(),
            created_at: timestamp,
            signature: None,
            signer_did: None,
        }
    }

    fn employment() -> HashMap<String, Ends> {
        ends_of(&[definition(
            "employed",
            1,
            json!({"start_field": "from", "end_field": "until"}),
        )])
    }

    #[test]
    fn longer_than_is_counted_on_the_calendar_between_the_typed_ends() {
        let five_years = Span::parse("5y").unwrap();
        let ends = employment();
        let six = attestation(
            "six",
            "employed",
            0,
            json!({"from": "2018-03-01", "until": "2024-03-01"}),
        );
        let exactly = attestation(
            "exactly",
            "employed",
            0,
            json!({"from": "2019-02-28", "until": "2024-02-28"}),
        );
        let leap = attestation(
            "leap",
            "employed",
            0,
            json!({"from": "2020-02-29T09:00:00Z", "until": "2025-02-28T09:00:01Z"}),
        );
        assert!(is_over(&six, &five_years, &ends));
        // Longer than, not as long as.
        assert!(!is_over(&exactly, &five_years, &ends));
        // Five years from February 29 is February 28: a second past it is over.
        assert!(is_over(&leap, &five_years, &ends));
    }

    #[test]
    fn no_typed_ends_is_no_length() {
        let span = Span::parse("1d").unwrap();
        let ends = employment();
        let untyped = attestation(
            "untyped",
            "author",
            0,
            json!({"from": "2018-03-01", "until": "2024-03-01"}),
        );
        let missing = attestation("missing", "employed", 0, json!({"from": "2018-03-01"}));
        let unreadable = attestation(
            "unreadable",
            "employed",
            0,
            json!({"from": "a while ago", "until": "2024-03-01"}),
        );
        let number = attestation(
            "number",
            "employed",
            0,
            json!({"from": 1, "until": "2024-03-01"}),
        );
        for a in [untyped, missing, unreadable, number] {
            assert!(!is_over(&a, &span, &ends), "{}", a.id);
        }
    }

    #[test]
    fn the_latest_definition_holds() {
        let ends = ends_of(&[
            definition("employed", 1, json!({"start_field": "a", "end_field": "b"})),
            definition(
                "employed",
                3,
                json!({"start_field": "from", "end_field": "until"}),
            ),
            definition("employed", 2, json!({"start_field": "x", "end_field": "y"})),
            definition("named", 1, json!({"start_field": "from"})),
            definition("plain", 1, json!({"display_color": "#fff"})),
        ]);
        assert_eq!(
            ends.get("employed"),
            Some(&Ends {
                start: "from".into(),
                end: "until".into()
            })
        );
        // A type naming only one end has no length.
        assert_eq!(ends.get("named"), None);
        assert_eq!(ends.get("plain"), None);
    }

    #[test]
    fn types_of_asks_for_every_predicate_once() {
        let rows = vec![
            attestation("1", "employed", 0, json!({})),
            attestation("2", "author", 0, json!({})),
            attestation("3", "employed", 0, json!({})),
        ];
        let filter = types_of(&rows);
        assert_eq!(filter.subjects, vec!["author", "employed"]);
        assert_eq!(filter.predicates, vec![TYPE_PREDICATE]);
        assert_eq!(filter.limit, EVERY_ROW);
    }

    #[test]
    fn keep_over_keeps_order() {
        let span = Span::parse("1y").unwrap();
        let ends = employment();
        let rows = vec![
            attestation(
                "b",
                "employed",
                0,
                json!({"from": "2020-01-01", "until": "2022-01-01"}),
            ),
            attestation(
                "short",
                "employed",
                0,
                json!({"from": "2020-01-01", "until": "2020-06-01"}),
            ),
            attestation(
                "a",
                "employed",
                0,
                json!({"from": "2010-01-01", "until": "2022-01-01"}),
            ),
        ];
        let kept: Vec<String> = keep_over(rows, &span, &ends)
            .into_iter()
            .map(|a| a.id)
            .collect();
        assert_eq!(kept, vec!["b", "a"]);
    }
}
