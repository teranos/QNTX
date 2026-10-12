//! AST types for parsed AX queries

use serde::{Deserialize, Serialize};

use crate::span::Span;
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
    /// `over 5y`: a span, read in the one grammar (crate::span).
    Over(Span),
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
