//! AST types for parsed AX queries

use serde::{Deserialize, Serialize};
use std::fmt;

/// A fully parsed AX query
#[derive(Debug, Clone, PartialEq, Default, Serialize, Deserialize)]
pub struct AxQuery<'a> {
    #[serde(borrow)]
    pub subjects: Vec<&'a str>,
    #[serde(borrow)]
    pub predicates: Vec<&'a str>,
    #[serde(borrow)]
    pub contexts: Vec<&'a str>,
    #[serde(borrow)]
    pub actors: Vec<&'a str>,
    pub temporal: Option<TemporalClause<'a>>,
    #[serde(borrow)]
    pub actions: Vec<&'a str>,
}

impl<'a> AxQuery<'a> {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn has_subjects(&self) -> bool {
        !self.subjects.is_empty()
    }

    pub fn has_predicates(&self) -> bool {
        !self.predicates.is_empty()
    }

    pub fn has_contexts(&self) -> bool {
        !self.contexts.is_empty()
    }

    pub fn has_actors(&self) -> bool {
        !self.actors.is_empty()
    }

    pub fn has_temporal(&self) -> bool {
        self.temporal.is_some()
    }

    pub fn has_actions(&self) -> bool {
        !self.actions.is_empty()
    }

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
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub enum TemporalClause<'a> {
    Since(&'a str),
    Until(&'a str),
    On(&'a str),
    Between(&'a str, &'a str),
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
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct DurationExpr<'a> {
    pub raw: &'a str,
    pub value: Option<f64>,
    pub unit: Option<DurationUnit>,
}

impl<'a> DurationExpr<'a> {
    pub fn parse(raw: &'a str) -> Self {
        let trimmed = raw.trim();
        if trimmed.is_empty() {
            return Self {
                raw,
                value: None,
                unit: None,
            };
        }

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
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum DurationUnit {
    Years,
    Months,
    Weeks,
    Days,
}

impl DurationUnit {
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
