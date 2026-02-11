//! Confidence scoring for conflict classification
//!
//! Multi-factor confidence calculation:
//! - Source diversity bonus (multiple independent actors)
//! - Actor credibility bonus (highest credibility actor)
//! - Temporal pattern bonus (simultaneous vs distributed)
//! - Recency bonus (how recent the most recent claim is)
//! - Consistency bonus (all claims agree on predicate)

use super::credibility::ActorCredibility;
use super::temporal::{ClaimTiming, TemporalAnalyzer};

/// Claim with full context for confidence calculation
#[derive(Debug, Clone)]
pub struct ClaimWithTiming {
    pub actor: String,
    pub timestamp_ms: i64,
    pub predicate: String,
    pub subject: String,
    pub context: String,
}

impl ClaimWithTiming {
    fn to_timing(&self) -> ClaimTiming {
        ClaimTiming {
            actor: self.actor.clone(),
            timestamp_ms: self.timestamp_ms,
            predicate: self.predicate.clone(),
        }
    }
}

/// Confidence calculator with configurable review threshold
pub struct ConfidenceCalculator<'a> {
    temporal: &'a TemporalAnalyzer,
    review_threshold: f64,
}

impl<'a> ConfidenceCalculator<'a> {
    pub fn new(temporal: &'a TemporalAnalyzer) -> Self {
        Self {
            temporal,
            review_threshold: 0.3,
        }
    }

    pub fn with_review_threshold(mut self, threshold: f64) -> Self {
        self.review_threshold = threshold;
        self
    }

    /// Calculate overall confidence score for a set of claims.
    /// `now_ms` is the current time in milliseconds for recency calculation.
    pub fn calculate(&self, claims: &[ClaimWithTiming], now_ms: i64) -> f64 {
        if claims.is_empty() {
            return 0.0;
        }

        if claims.len() == 1 {
            return self.single_claim_confidence(&claims[0], now_ms);
        }

        let base_score = 0.5;
        let source_bonus = self.source_diversity_bonus(claims);
        let credibility_bonus = self.credibility_bonus(claims);
        let temporal_bonus = self.temporal_bonus(claims);
        let recency_bonus = self.recency_bonus(claims, now_ms);
        let consistency_bonus = self.consistency_bonus(claims);

        let total = base_score
            + source_bonus
            + credibility_bonus
            + temporal_bonus
            + recency_bonus
            + consistency_bonus;

        total.min(1.0)
    }

    /// Whether a confidence score requires human review
    pub fn requires_review(&self, confidence: f64) -> bool {
        confidence < self.review_threshold
    }

    /// Single claim confidence based on actor credibility and recency
    fn single_claim_confidence(&self, claim: &ClaimWithTiming, now_ms: i64) -> f64 {
        let credibility = ActorCredibility::from_actor(&claim.actor);
        let recency = self.temporal.recency_score(claim.timestamp_ms, now_ms);
        (credibility.score() * 0.7) + (recency * 0.3)
    }

    /// Bonus for having multiple independent sources (+0.3 max)
    fn source_diversity_bonus(&self, claims: &[ClaimWithTiming]) -> f64 {
        let mut unique_actors = std::collections::HashSet::new();
        for claim in claims {
            unique_actors.insert(&claim.actor);
        }

        let independent_count = unique_actors.len();
        if independent_count <= 1 {
            return 0.0;
        }

        let bonus = (independent_count - 1) as f64 * 0.1;
        bonus.min(0.3)
    }

    /// Bonus based on highest credibility actor (+0.2 max)
    fn credibility_bonus(&self, claims: &[ClaimWithTiming]) -> f64 {
        let highest = claims
            .iter()
            .map(|c| ActorCredibility::from_actor(&c.actor))
            .max()
            .unwrap_or(ActorCredibility::External);

        highest.score() * 0.2
    }

    /// Bonus based on temporal patterns (+0.2 max)
    fn temporal_bonus(&self, claims: &[ClaimWithTiming]) -> f64 {
        let timings: Vec<ClaimTiming> = claims.iter().map(|c| c.to_timing()).collect();
        let temporal_confidence = self.temporal.temporal_confidence(&timings);
        (temporal_confidence - 0.5) * 0.4 // Convert 0.5-1.0 range to 0.0-0.2
    }

    /// Bonus based on most recent claim (+0.1 max)
    fn recency_bonus(&self, claims: &[ClaimWithTiming], now_ms: i64) -> f64 {
        let timings: Vec<ClaimTiming> = claims.iter().map(|c| c.to_timing()).collect();
        if let Some(most_recent) = self.temporal.most_recent(&timings) {
            let recency = self.temporal.recency_score(most_recent, now_ms);
            recency * 0.1
        } else {
            0.0
        }
    }

    /// Bonus for consistent predicates (+0.1 max)
    fn consistency_bonus(&self, claims: &[ClaimWithTiming]) -> f64 {
        let mut unique_predicates = std::collections::HashSet::new();
        for claim in claims {
            unique_predicates.insert(&claim.predicate);
        }

        if unique_predicates.len() == 1 {
            return 0.1;
        }

        // Check if predicates are related (one contains the other as prefix/suffix)
        if self.are_related_predicates(&unique_predicates) {
            return 0.05;
        }

        0.0
    }

    /// Heuristic: if one predicate contains another, they might be related
    fn are_related_predicates(&self, predicates: &std::collections::HashSet<&String>) -> bool {
        let list: Vec<&&String> = predicates.iter().collect();
        for (i, p1) in list.iter().enumerate() {
            for (j, p2) in list.iter().enumerate() {
                if i != j && (p1.starts_with(p2.as_str()) || p1.ends_with(p2.as_str())) {
                    return true;
                }
            }
        }
        false
    }
}

#[cfg(test)]
mod tests {
    use super::super::temporal::TemporalConfig;
    use super::*;

    fn setup() -> (TemporalAnalyzer, i64) {
        let ta = TemporalAnalyzer::new(TemporalConfig::default());
        let now = 1_000_000_000; // some reference time
        (ta, now)
    }

    fn claim(actor: &str, ts: i64, predicate: &str) -> ClaimWithTiming {
        ClaimWithTiming {
            actor: actor.to_string(),
            timestamp_ms: ts,
            predicate: predicate.to_string(),
            subject: "ALICE".to_string(),
            context: "GitHub".to_string(),
        }
    }

    #[test]
    fn empty_claims_zero_confidence() {
        let (ta, now) = setup();
        let cc = ConfidenceCalculator::new(&ta);
        assert_eq!(cc.calculate(&[], now), 0.0);
    }

    #[test]
    fn single_human_claim_high_confidence() {
        let (ta, now) = setup();
        let cc = ConfidenceCalculator::new(&ta);
        let claims = vec![claim("human:alice", now - 1000, "is_author_of")];
        let score = cc.calculate(&claims, now);
        // Human credibility (1.0 * 0.7) + recency (1.0 * 0.3) = 1.0
        assert!(
            score > 0.9,
            "human claim should be high confidence, got {}",
            score
        );
    }

    #[test]
    fn multiple_sources_boost_confidence() {
        let (ta, now) = setup();
        let cc = ConfidenceCalculator::new(&ta);
        let claims = vec![
            claim("human:alice", now - 1000, "is_author_of"),
            claim("human:bob", now - 2000, "is_author_of"),
            claim("system:ci", now - 3000, "is_author_of"),
        ];
        let score = cc.calculate(&claims, now);
        assert!(
            score > 0.7,
            "multiple sources should boost confidence, got {}",
            score
        );
    }

    #[test]
    fn review_threshold() {
        let (ta, _now) = setup();
        let cc = ConfidenceCalculator::new(&ta).with_review_threshold(0.5);
        assert!(cc.requires_review(0.3));
        assert!(!cc.requires_review(0.6));
    }
}
