//! Temporal resolution shared by the wazero and browser targets.
//!
//! The parser keeps a temporal clause as the words the user typed
//! ("yesterday", "3 days ago"); a store filtering on timestamps needs
//! instants. Resolution lives here, once, so both WASM targets turn the
//! same words into the same bounds.

use ats::parser::{Parser, TemporalClause};
use ats::temporal::{resolve_temporal, ResolvedTemporal};
use serde::Serialize;

/// A parsed query with its temporal clause resolved to epoch milliseconds.
#[derive(Debug, Serialize)]
pub struct ResolvedQuery {
    pub subjects: Vec<String>,
    pub predicates: Vec<String>,
    pub contexts: Vec<String>,
    pub actors: Vec<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub temporal: Option<ResolvedTemporal>,
    pub actions: Vec<String>,
}

/// Parse `input` and resolve its temporal clause against `now_ms`.
pub fn resolve_query(input: &str, now_ms: i64) -> Result<ResolvedQuery, String> {
    let query = Parser::parse(input).map_err(|e| format!("{}", e))?;

    // Same validation hack as parse_ax_query: the parser accepts "over 5q"
    // but Go rejects it, and bug-for-bug compatibility wins.
    if let Some(TemporalClause::Over(ref dur)) = query.temporal {
        if dur.value.is_some() && dur.unit.is_none() {
            return Err(format!("missing unit in '{}'", dur.raw));
        }
    }

    let resolve = |expr: &str| {
        resolve_temporal(expr, now_ms)
            .ok_or_else(|| format!("unable to parse temporal expression: {}", expr))
    };

    let temporal = match &query.temporal {
        Some(TemporalClause::Since(expr)) => Some(ResolvedTemporal::Since(resolve(expr)?)),
        Some(TemporalClause::Until(expr)) => Some(ResolvedTemporal::Until(resolve(expr)?)),
        Some(TemporalClause::On(expr)) => {
            let start_ms = resolve(expr)?;
            Some(ResolvedTemporal::On {
                start_ms,
                end_ms: start_ms + 86_400_000,
            })
        }
        Some(TemporalClause::Between(start, end)) => Some(ResolvedTemporal::Between {
            start_ms: resolve(start)?,
            end_ms: resolve(end)?,
        }),
        Some(TemporalClause::Over(dur)) => Some(ResolvedTemporal::Over {
            raw: dur.raw.to_string(),
            value: dur.value,
            unit: dur.unit.map(|u| u.to_string()),
        }),
        None => None,
    };

    Ok(ResolvedQuery {
        subjects: query.subjects.iter().map(|s| s.to_string()).collect(),
        predicates: query.predicates.iter().map(|s| s.to_string()).collect(),
        contexts: query.contexts.iter().map(|s| s.to_string()).collect(),
        actors: query.actors.iter().map(|s| s.to_string()).collect(),
        temporal,
        actions: query.actions.iter().map(|s| s.to_string()).collect(),
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    const NOW_MS: i64 = 1_700_000_000_000;
    const DAY_MS: i64 = 86_400_000;

    #[test]
    fn since_yesterday_resolves_to_an_instant() {
        let q = resolve_query("ALICE since yesterday", NOW_MS).unwrap();
        assert_eq!(q.subjects, vec!["ALICE"]);
        assert_eq!(q.temporal, Some(ResolvedTemporal::Since(NOW_MS - DAY_MS)));
    }

    #[test]
    fn on_spans_the_full_day() {
        let q = resolve_query("ALICE on 2025-01-15", NOW_MS).unwrap();
        match q.temporal {
            Some(ResolvedTemporal::On { start_ms, end_ms }) => {
                assert_eq!(end_ms - start_ms, DAY_MS);
            }
            other => panic!("expected On, got {:?}", other),
        }
    }

    #[test]
    fn between_resolves_both_bounds() {
        let q = resolve_query("ALICE between 2025-01-01 and 2025-02-01", NOW_MS).unwrap();
        match q.temporal {
            Some(ResolvedTemporal::Between { start_ms, end_ms }) => {
                assert!(start_ms < end_ms);
            }
            other => panic!("expected Between, got {:?}", other),
        }
    }

    #[test]
    fn an_unresolvable_expression_is_an_error_naming_it() {
        let err = resolve_query("ALICE since yesterdy", NOW_MS).unwrap_err();
        assert!(err.contains("yesterdy"), "error was: {}", err);
    }

    #[test]
    fn no_temporal_clause_stays_absent() {
        let q = resolve_query("ALICE is author", NOW_MS).unwrap();
        assert_eq!(q.temporal, None);
        assert_eq!(q.predicates, vec!["author"]);
    }

    #[test]
    fn over_carries_the_duration_through_unresolved() {
        let q = resolve_query("ALICE over 5y", NOW_MS).unwrap();
        match q.temporal {
            Some(ResolvedTemporal::Over { value, .. }) => assert_eq!(value, Some(5.0)),
            other => panic!("expected Over, got {:?}", other),
        }
    }
}
