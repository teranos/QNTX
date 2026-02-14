//! Attestation type definitions

use serde::{Deserialize, Serialize};
use std::collections::HashMap;

/// An attestation - a verifiable claim about subjects, predicates, and contexts
/// with actor attribution and timestamps.
///
/// This is the fundamental unit of data in QNTX. Every piece of information
/// is represented as an attestation with full provenance.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct Attestation {
    /// ASID: AS + UUID (e.g., "AS-550e8400-e29b-41d4-a716-446655440000")
    pub id: String,

    /// Entities being attested about
    pub subjects: Vec<String>,

    /// What is being claimed (the relationship/property)
    pub predicates: Vec<String>,

    /// Context for the claim (e.g., "GitHub", "2024")
    pub contexts: Vec<String>,

    /// Who made the attestation
    pub actors: Vec<String>,

    /// When attestation was made (Unix timestamp milliseconds)
    pub timestamp: i64,

    /// How attestation was created (e.g., "cli", "api", "pulse")
    pub source: String,

    /// Arbitrary JSON attributes
    #[serde(default, skip_serializing_if = "HashMap::is_empty")]
    pub attributes: HashMap<String, serde_json::Value>,

    /// Database creation time (Unix timestamp milliseconds).
    /// Defaults to 0 when absent (e.g. content hashing omits it deliberately).
    #[serde(default)]
    pub created_at: i64,
}

impl Attestation {
    /// Returns true if this is a simple existence attestation (predicates and contexts are "_")
    pub fn is_existence_attestation(&self) -> bool {
        self.predicates.len() == 1
            && self.predicates[0] == "_"
            && self.contexts.len() == 1
            && self.contexts[0] == "_"
    }

    /// Returns true if this attestation has multiple subjects, predicates, or contexts
    pub fn has_multiple_dimensions(&self) -> bool {
        self.subjects.len() > 1 || self.predicates.len() > 1 || self.contexts.len() > 1
    }

    /// Returns the total number of individual claims this attestation represents
    /// (Cartesian product of subjects × predicates × contexts)
    pub fn cartesian_count(&self) -> usize {
        self.subjects.len() * self.predicates.len() * self.contexts.len()
    }
}

impl Default for Attestation {
    fn default() -> Self {
        Self {
            id: String::new(),
            subjects: Vec::new(),
            predicates: vec!["_".to_string()],
            contexts: vec!["_".to_string()],
            actors: Vec::new(),
            timestamp: 0,
            source: String::new(),
            attributes: HashMap::new(),
            created_at: 0,
        }
    }
}

/// Builder for creating attestations
#[derive(Debug, Default)]
pub struct AttestationBuilder {
    attestation: Attestation,
}

impl AttestationBuilder {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn id(mut self, id: impl Into<String>) -> Self {
        self.attestation.id = id.into();
        self
    }

    pub fn subject(mut self, subject: impl Into<String>) -> Self {
        self.attestation.subjects.push(subject.into());
        self
    }

    pub fn subjects(mut self, subjects: impl IntoIterator<Item = impl Into<String>>) -> Self {
        self.attestation
            .subjects
            .extend(subjects.into_iter().map(|s| s.into()));
        self
    }

    pub fn predicate(mut self, predicate: impl Into<String>) -> Self {
        // Clear default "_" if we're adding a real predicate
        if self.attestation.predicates == vec!["_"] {
            self.attestation.predicates.clear();
        }
        self.attestation.predicates.push(predicate.into());
        self
    }

    pub fn predicates(mut self, predicates: impl IntoIterator<Item = impl Into<String>>) -> Self {
        if self.attestation.predicates == vec!["_"] {
            self.attestation.predicates.clear();
        }
        self.attestation
            .predicates
            .extend(predicates.into_iter().map(|s| s.into()));
        self
    }

    pub fn context(mut self, context: impl Into<String>) -> Self {
        if self.attestation.contexts == vec!["_"] {
            self.attestation.contexts.clear();
        }
        self.attestation.contexts.push(context.into());
        self
    }

    pub fn contexts(mut self, contexts: impl IntoIterator<Item = impl Into<String>>) -> Self {
        if self.attestation.contexts == vec!["_"] {
            self.attestation.contexts.clear();
        }
        self.attestation
            .contexts
            .extend(contexts.into_iter().map(|s| s.into()));
        self
    }

    pub fn actor(mut self, actor: impl Into<String>) -> Self {
        self.attestation.actors.push(actor.into());
        self
    }

    pub fn actors(mut self, actors: impl IntoIterator<Item = impl Into<String>>) -> Self {
        self.attestation
            .actors
            .extend(actors.into_iter().map(|s| s.into()));
        self
    }

    pub fn timestamp(mut self, timestamp: i64) -> Self {
        self.attestation.timestamp = timestamp;
        self
    }

    pub fn source(mut self, source: impl Into<String>) -> Self {
        self.attestation.source = source.into();
        self
    }

    pub fn attribute(mut self, key: impl Into<String>, value: serde_json::Value) -> Self {
        self.attestation.attributes.insert(key.into(), value);
        self
    }

    pub fn build(self) -> Attestation {
        self.attestation
    }
}

/// Temporal comparison for "over X years/months" queries
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct OverFilter {
    /// The numeric value (e.g., 5 for "5y")
    pub value: f64,
    /// The unit: "y" for years, "m" for months, "d" for days
    pub unit: String,
    /// Comparison operator: "over" means >=
    pub operator: String,
}

/// Query filter for attestations
#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
pub struct AxFilter {
    /// Specific entities to query about
    #[serde(default)]
    pub subjects: Vec<String>,

    /// Predicates to match (supports fuzzy)
    #[serde(default)]
    pub predicates: Vec<String>,

    /// Contexts to match
    #[serde(default)]
    pub contexts: Vec<String>,

    /// Filter by specific actors
    #[serde(default)]
    pub actors: Vec<String>,

    /// Temporal range start (Unix timestamp ms)
    pub time_start: Option<i64>,

    /// Temporal range end (Unix timestamp ms)
    pub time_end: Option<i64>,

    /// Temporal comparison (e.g., "over 5y")
    pub over_comparison: Option<OverFilter>,

    /// Maximum results
    pub limit: Option<usize>,
}

/// Result of an ax query
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct AxResult {
    /// All matching attestations
    pub attestations: Vec<Attestation>,

    /// Identified conflicts
    pub conflicts: Vec<Conflict>,

    /// Aggregated information
    pub summary: AxSummary,
}

/// Aggregated information about query results
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct AxSummary {
    pub total_attestations: usize,
    pub unique_subjects: HashMap<String, usize>,
    pub unique_predicates: HashMap<String, usize>,
    pub unique_contexts: HashMap<String, usize>,
    pub unique_actors: HashMap<String, usize>,
}

/// Conflicting attestations
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct Conflict {
    pub subject: String,
    pub predicate: String,
    pub context: String,
    pub attestations: Vec<Attestation>,
    /// Resolution type: "conflict", "evolution", "verification", "coexistence", "supersession"
    pub resolution: String,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_builder() {
        let attestation = AttestationBuilder::new()
            .id("AS-test-123")
            .subject("ALICE")
            .predicate("is_author_of")
            .context("GitHub")
            .actor("human:bob")
            .source("cli")
            .timestamp(1704067200000)
            .build();

        assert_eq!(attestation.id, "AS-test-123");
        assert_eq!(attestation.subjects, vec!["ALICE"]);
        assert_eq!(attestation.predicates, vec!["is_author_of"]);
        assert_eq!(attestation.contexts, vec!["GitHub"]);
        assert_eq!(attestation.actors, vec!["human:bob"]);
    }

    #[test]
    fn test_existence_attestation() {
        let existence = AttestationBuilder::new()
            .subject("ALICE")
            .actor("human:bob")
            .build();

        assert!(existence.is_existence_attestation());

        let regular = AttestationBuilder::new()
            .subject("ALICE")
            .predicate("works_at")
            .context("ACME")
            .build();

        assert!(!regular.is_existence_attestation());
    }

    #[test]
    fn test_cartesian_count() {
        let multi = AttestationBuilder::new()
            .subjects(["ALICE", "BOB"])
            .predicates(["knows", "works_with"])
            .contexts(["ACME", "GitHub"])
            .build();

        assert_eq!(multi.cartesian_count(), 8); // 2 × 2 × 2
    }

    #[test]
    fn test_multiple_dimensions() {
        let single = AttestationBuilder::new()
            .subject("ALICE")
            .predicate("is_author")
            .context("GitHub")
            .build();

        assert!(!single.has_multiple_dimensions());

        let multi = AttestationBuilder::new()
            .subjects(["ALICE", "BOB"])
            .predicate("is_author")
            .build();

        assert!(multi.has_multiple_dimensions());
    }
}
