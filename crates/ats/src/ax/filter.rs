//! An AX query as the filter it asks for.
//!
//! The one place a query's words become rows: the server (through wazero) and
//! the browser (through wasm-bindgen) both call [`ax_filter`], so `ALICE since
//! last month` bounds the same rows wherever it runs. The browser's offline
//! query used to pass the parsed query straight to its store, whose filter
//! reads time from `time_start`/`time_end` and dropped the `temporal` clause:
//! offline, `since last month` was all time.

use serde::{Deserialize, Serialize};
use thiserror::Error;

use crate::attestation::AxFilter;
use crate::parser::{DurationExpr, ParseError, Parser, TemporalClause};
use crate::temporal::{resolve_day, resolve_temporal};

/// What an AX query asks for: the rows, and what to do with them (`so`).
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct AxRequest {
    pub filter: AxFilter,
    pub actions: Vec<String>,
}

/// Why a query names no rows.
#[derive(Debug, Clone, PartialEq, Error)]
pub enum AxRequestError {
    #[error("{0}")]
    Parse(#[from] ParseError),
    #[error("missing unit in '{0}'")]
    MissingUnit(String),
    #[error("unable to parse temporal expression: {0}")]
    Temporal(String),
}

/// The filter an AX query names, its time resolved against `now_ms`, reading
/// at most `limit` rows. Subjects, predicates, contexts and actors are as
/// written: each store matches case as it does.
pub fn ax_filter(query: &str, now_ms: i64, limit: usize) -> Result<AxRequest, AxRequestError> {
    let parsed = Parser::parse(query)?;

    let at = |expr: &str| {
        resolve_temporal(expr, now_ms).ok_or_else(|| AxRequestError::Temporal(expr.to_string()))
    };

    let (time_start, time_end) = match &parsed.temporal {
        None => (None, None),
        Some(TemporalClause::Since(expr)) => (Some(at(expr)?), None),
        Some(TemporalClause::Until(expr)) => (None, Some(at(expr)?)),
        Some(TemporalClause::On(expr)) => {
            let day = resolve_day(expr, now_ms)
                .ok_or_else(|| AxRequestError::Temporal(expr.to_string()))?;
            (Some(day.start_ms), Some(day.end_ms))
        }
        Some(TemporalClause::Between(start, end)) => (Some(at(start)?), Some(at(end)?)),
        // `over 5y` is a duration, not a range: it bounds no rows. A number
        // with no unit it reads is refused.
        Some(TemporalClause::Over(DurationExpr {
            raw,
            value: Some(_),
            unit: None,
        })) => return Err(AxRequestError::MissingUnit(raw.to_string())),
        Some(TemporalClause::Over(DurationExpr { .. })) => (None, None),
    };

    let owned = |words: &[&str]| words.iter().map(|w| w.to_string()).collect::<Vec<_>>();

    Ok(AxRequest {
        filter: AxFilter {
            subjects: owned(&parsed.subjects),
            predicates: owned(&parsed.predicates),
            contexts: owned(&parsed.contexts),
            actors: owned(&parsed.actors),
            time_start,
            time_end,
            source: None,
            limit,
        },
        actions: owned(&parsed.actions),
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::attestation::EVERY_ROW;

    // 2026-03-31T10:00:00Z
    const MARCH_31: i64 = 1_774_951_200_000;

    #[test]
    fn since_last_month_bounds_the_start() {
        let req = ax_filter("ALICE since last month", MARCH_31, EVERY_ROW).unwrap();
        assert_eq!(req.filter.subjects, vec!["ALICE"]);
        // 2026-02-28T10:00:00Z
        assert_eq!(req.filter.time_start, Some(1_772_272_800_000));
        assert_eq!(req.filter.time_end, None);
        assert_eq!(req.filter.limit, EVERY_ROW);
    }

    #[test]
    fn on_bounds_the_utc_day_and_between_both_ends() {
        let on = ax_filter("ALICE on yesterday", MARCH_31, 10).unwrap();
        assert_eq!(on.filter.time_start, Some(1_774_828_800_000));
        assert_eq!(on.filter.time_end, Some(1_774_915_200_000));

        let between = ax_filter("between 2025-01-01 and 2025-02-01", MARCH_31, 10).unwrap();
        assert_eq!(between.filter.time_start, Some(1_735_689_600_000));
        assert_eq!(between.filter.time_end, Some(1_738_368_000_000));

        let until = ax_filter("is author until 2025-01-01", MARCH_31, 10).unwrap();
        assert_eq!(until.filter.predicates, vec!["author"]);
        assert_eq!(until.filter.time_start, None);
        assert_eq!(until.filter.time_end, Some(1_735_689_600_000));
    }

    #[test]
    fn words_are_kept_as_written_and_actions_carried() {
        let req = ax_filter("alice is Author of qntx by Bob so notify", MARCH_31, 100).unwrap();
        assert_eq!(req.filter.subjects, vec!["alice"]);
        assert_eq!(req.filter.predicates, vec!["Author"]);
        assert_eq!(req.filter.contexts, vec!["qntx"]);
        assert_eq!(req.filter.actors, vec!["Bob"]);
        assert_eq!(req.actions, vec!["notify"]);
    }

    #[test]
    fn refusals() {
        assert_eq!(
            ax_filter("ALICE since 01/15/2025", MARCH_31, 10),
            Err(AxRequestError::Temporal("01/15/2025".to_string()))
        );
        let slashes = ax_filter("ALICE since 2025/01/15", MARCH_31, 10).unwrap();
        assert_eq!(slashes.filter.time_start, Some(1_736_899_200_000));
        assert_eq!(
            ax_filter("ALICE over 5q", MARCH_31, 10),
            Err(AxRequestError::MissingUnit("5q".to_string()))
        );
        assert!(matches!(
            ax_filter("", MARCH_31, 10),
            Err(AxRequestError::Parse(_))
        ));
    }

    #[test]
    fn over_bounds_no_rows() {
        let req = ax_filter("ALICE over 5y", MARCH_31, 10).unwrap();
        assert_eq!((req.filter.time_start, req.filter.time_end), (None, None));
    }

    #[test]
    fn serializes_as_the_stores_read_it() {
        let req = ax_filter("ALICE since last month", MARCH_31, 10).unwrap();
        let json = serde_json::to_string(&req).unwrap();
        assert!(json.contains(r#""time_start":1772272800000"#), "{json}");
        assert!(json.contains(r#""limit":10"#), "{json}");
        let back: AxFilter =
            serde_json::from_str(&serde_json::to_string(&req.filter).unwrap()).unwrap();
        assert_eq!(back, req.filter);
    }
}
