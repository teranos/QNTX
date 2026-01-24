//! Classification types

use super::ActorCredibility;
use serde::{Deserialize, Serialize};

/// Types of claim relationships
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum ConflictType {
    /// Same actor updated their claim over time
    Evolution,
    /// Multiple sources agreeing (strengthens the claim)
    Verification,
    /// Different contexts, both claims are valid
    Coexistence,
    /// Higher authority overrides lower
    Supersession,
    /// Genuine conflict requiring manual review
    Review,
}

impl ConflictType {
    /// Whether this type can be auto-resolved
    pub fn is_auto_resolvable(&self) -> bool {
        !matches!(self, Self::Review)
    }

    /// Suggested resolution strategy
    pub fn resolution_strategy(&self) -> &'static str {
        match self {
            Self::Evolution => "show_latest",
            Self::Verification => "show_all_sources",
            Self::Coexistence => "show_all_contexts",
            Self::Supersession => "show_highest_authority",
            Self::Review => "flag_for_review",
        }
    }
}

impl std::fmt::Display for ConflictType {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Evolution => write!(f, "evolution"),
            Self::Verification => write!(f, "verification"),
            Self::Coexistence => write!(f, "coexistence"),
            Self::Supersession => write!(f, "supersession"),
            Self::Review => write!(f, "review"),
        }
    }
}

/// Result of classifying a set of claims
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ClassificationResult {
    /// The determined conflict type
    pub conflict_type: ConflictType,
    /// Confidence in the classification (0.0 - 1.0)
    pub confidence: f64,
    /// Actors involved, ranked by credibility
    pub actor_rankings: Vec<ActorRanking>,
    /// Whether this was auto-resolved or needs review
    pub auto_resolved: bool,
    /// Suggested resolution strategy
    pub strategy: &'static str,
}

/// Actor with credibility ranking
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ActorRanking {
    pub actor: String,
    pub credibility: ActorCredibility,
    pub timestamp: Option<i64>, // Unix timestamp
}

impl ClassificationResult {
    /// Create a new classification result
    pub fn new(
        conflict_type: ConflictType,
        confidence: f64,
        actor_rankings: Vec<ActorRanking>,
    ) -> Self {
        Self {
            auto_resolved: conflict_type.is_auto_resolvable(),
            strategy: conflict_type.resolution_strategy(),
            conflict_type,
            confidence,
            actor_rankings,
        }
    }
}
