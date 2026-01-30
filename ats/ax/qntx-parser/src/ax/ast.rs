//! AST types for parsed AX queries
//!
//! These types represent the structured output of the AX parser,
//! closely mirroring the Go `AxFilter` type for FFI compatibility.

use std::fmt;

/// A fully parsed AX query
///
/// Represents the complete semantic structure of an AX query string.
/// All fields are optional since AX queries support progressive refinement.
///
/// # Example
///
/// ```text
/// "ALICE BOB is author_of of GitHub by CHARLIE since 2024-01-01 so notify"
/// ```
///
/// Parses to:
/// - subjects: ["ALICE", "BOB"]
/// - predicates: ["author_of"]
/// - contexts: ["GitHub"]
/// - actors: ["CHARLIE"]
/// - temporal: Since("2024-01-01")
/// - actions: ["notify"]
#[derive(Debug, Clone, PartialEq, Default)]
pub struct AxQuery<'a> {
    /// Subject entities being queried
    pub subjects: Vec<&'a str>,
    /// Predicates (what is being asked)
    pub predicates: Vec<&'a str>,
    /// Context values (of/from clause)
    pub contexts: Vec<&'a str>,
    /// Actor values (by/via clause)
    pub actors: Vec<&'a str>,
    /// Temporal constraint
    pub temporal: Option<TemporalClause<'a>>,
    /// Actions to perform (so/therefore clause)
    pub actions: Vec<&'a str>,
}

impl<'a> AxQuery<'a> {
    /// Create a new empty query
    pub fn new() -> Self {
        Self::default()
    }

    /// Check if query has any subjects
    pub fn has_subjects(&self) -> bool {
        !self.subjects.is_empty()
    }

    /// Check if query has any predicates
    pub fn has_predicates(&self) -> bool {
        !self.predicates.is_empty()
    }

    /// Check if query has any contexts
    pub fn has_contexts(&self) -> bool {
        !self.contexts.is_empty()
    }

    /// Check if query has any actors
    pub fn has_actors(&self) -> bool {
        !self.actors.is_empty()
    }

    /// Check if query has temporal constraints
    pub fn has_temporal(&self) -> bool {
        self.temporal.is_some()
    }

    /// Check if query has actions
    pub fn has_actions(&self) -> bool {
        !self.actions.is_empty()
    }

    /// Check if query is empty (no clauses)
    pub fn is_empty(&self) -> bool {
        self.subjects.is_empty()
            && self.predicates.is_empty()
            && self.contexts.is_empty()
            && self.actors.is_empty()
            && self.temporal.is_none()
            && self.actions.is_empty()
    }
}

/// Temporal constraint types
///
/// Represents different ways to constrain query results by time.
#[derive(Debug, Clone, PartialEq)]
pub enum TemporalClause<'a> {
    /// "since DATE" - results from this date onwards
    Since(&'a str),
    /// "until DATE" - results up to this date
    Until(&'a str),
    /// "on DATE" - results on this specific date
    On(&'a str),
    /// "between DATE and DATE" - results within date range
    Between(&'a str, &'a str),
    /// "over DURATION" - duration comparison (e.g., "over 5y")
    Over(DurationExpr<'a>),
}

impl fmt::Display for TemporalClause<'_> {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            TemporalClause::Since(date) => write!(f, "since {}", date),
            TemporalClause::Until(date) => write!(f, "until {}", date),
            TemporalClause::On(date) => write!(f, "on {}", date),
            TemporalClause::Between(start, end) => write!(f, "between {} and {}", start, end),
            TemporalClause::Over(dur) => write!(f, "over {}", dur),
        }
    }
}

/// Duration expression for "over" comparisons
///
/// Represents expressions like "5y" (5 years), "3m" (3 months), "10d" (10 days).
#[derive(Debug, Clone, PartialEq)]
pub struct DurationExpr<'a> {
    /// The raw duration string (e.g., "5y", "3m")
    pub raw: &'a str,
    /// Parsed numeric value (if parseable)
    pub value: Option<f64>,
    /// Duration unit (y=years, m=months, d=days, w=weeks)
    pub unit: Option<DurationUnit>,
}

impl<'a> DurationExpr<'a> {
    /// Parse a duration expression from a string like "5y" or "3m"
    pub fn parse(raw: &'a str) -> Self {
        let trimmed = raw.trim();
        if trimmed.is_empty() {
            return Self {
                raw,
                value: None,
                unit: None,
            };
        }

        // Try to extract numeric part and unit
        let mut num_end = 0;
        for (i, c) in trimmed.char_indices() {
            if c.is_ascii_digit() || c == '.' {
                num_end = i + c.len_utf8();
            } else {
                break;
            }
        }

        let value = trimmed[..num_end].parse::<f64>().ok();
        let unit_str = &trimmed[num_end..];
        let unit = DurationUnit::parse(unit_str);

        Self { raw, value, unit }
    }
}

impl fmt::Display for DurationExpr<'_> {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}", self.raw)
    }
}

/// Duration unit types
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum DurationUnit {
    /// Years (y)
    Years,
    /// Months (m)
    Months,
    /// Weeks (w)
    Weeks,
    /// Days (d)
    Days,
}

impl DurationUnit {
    /// Parse duration unit from string suffix
    pub fn parse(s: &str) -> Option<Self> {
        match s.to_ascii_lowercase().as_str() {
            "y" | "yr" | "yrs" | "year" | "years" => Some(DurationUnit::Years),
            "m" | "mo" | "mos" | "month" | "months" => Some(DurationUnit::Months),
            "w" | "wk" | "wks" | "week" | "weeks" => Some(DurationUnit::Weeks),
            "d" | "day" | "days" => Some(DurationUnit::Days),
            _ => None,
        }
    }
}

impl fmt::Display for DurationUnit {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            DurationUnit::Years => write!(f, "y"),
            DurationUnit::Months => write!(f, "m"),
            DurationUnit::Weeks => write!(f, "w"),
            DurationUnit::Days => write!(f, "d"),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_empty_query() {
        let query = AxQuery::new();
        assert!(query.is_empty());
        assert!(!query.has_subjects());
        assert!(!query.has_predicates());
        assert!(!query.has_contexts());
        assert!(!query.has_actors());
        assert!(!query.has_temporal());
        assert!(!query.has_actions());
    }

    #[test]
    fn test_query_with_subjects() {
        let query = AxQuery {
            subjects: vec!["ALICE", "BOB"],
            ..Default::default()
        };
        assert!(!query.is_empty());
        assert!(query.has_subjects());
        assert_eq!(query.subjects.len(), 2);
    }

    #[test]
    fn test_duration_parse_years() {
        let dur = DurationExpr::parse("5y");
        assert_eq!(dur.value, Some(5.0));
        assert_eq!(dur.unit, Some(DurationUnit::Years));
    }

    #[test]
    fn test_duration_parse_months() {
        let dur = DurationExpr::parse("3m");
        assert_eq!(dur.value, Some(3.0));
        assert_eq!(dur.unit, Some(DurationUnit::Months));
    }

    #[test]
    fn test_duration_parse_decimal() {
        let dur = DurationExpr::parse("2.5y");
        assert_eq!(dur.value, Some(2.5));
        assert_eq!(dur.unit, Some(DurationUnit::Years));
    }

    #[test]
    fn test_duration_parse_days() {
        let dur = DurationExpr::parse("30d");
        assert_eq!(dur.value, Some(30.0));
        assert_eq!(dur.unit, Some(DurationUnit::Days));
    }

    #[test]
    fn test_duration_parse_weeks() {
        let dur = DurationExpr::parse("2w");
        assert_eq!(dur.value, Some(2.0));
        assert_eq!(dur.unit, Some(DurationUnit::Weeks));
    }

    #[test]
    fn test_duration_unit_alternatives() {
        assert_eq!(DurationUnit::parse("year"), Some(DurationUnit::Years));
        assert_eq!(DurationUnit::parse("years"), Some(DurationUnit::Years));
        assert_eq!(DurationUnit::parse("month"), Some(DurationUnit::Months));
        assert_eq!(DurationUnit::parse("months"), Some(DurationUnit::Months));
    }

    #[test]
    fn test_temporal_display() {
        assert_eq!(
            TemporalClause::Since("2024-01-01").to_string(),
            "since 2024-01-01"
        );
        assert_eq!(
            TemporalClause::Between("2024-01-01", "2024-12-31").to_string(),
            "between 2024-01-01 and 2024-12-31"
        );
        assert_eq!(
            TemporalClause::Over(DurationExpr::parse("5y")).to_string(),
            "over 5y"
        );
    }
}
