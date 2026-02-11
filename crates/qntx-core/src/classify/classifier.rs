//! Smart conflict classifier
//!
//! Orchestrates temporal analysis, credibility ranking, and confidence scoring
//! to classify claim conflicts and determine resolution strategies.

use serde::{Deserialize, Serialize};

use super::confidence::{ClaimWithTiming, ConfidenceCalculator};
use super::credibility::ActorCredibility;
use super::temporal::{TemporalAnalyzer, TemporalConfig};
use super::types::{ActorRanking, ConflictType};

/// Input claim for classification (JSON-friendly for WASM boundary)
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ClaimInput {
    pub subject: String,
    pub predicate: String,
    pub context: String,
    pub actor: String,
    pub timestamp_ms: i64,
    /// Source attestation ID
    pub source_id: String,
}

/// Input for classify_claims WASM function
#[derive(Debug, Serialize, Deserialize)]
pub struct ClassifyInput {
    /// Claims grouped by key (subject|predicate|context|actor)
    pub claim_groups: Vec<ClaimGroup>,
    /// Temporal configuration (uses defaults if omitted)
    #[serde(default)]
    pub config: TemporalConfig,
    /// Current time in milliseconds (for recency calculation)
    pub now_ms: i64,
}

/// A group of claims sharing the same key
#[derive(Debug, Serialize, Deserialize)]
pub struct ClaimGroup {
    pub key: String,
    pub claims: Vec<ClaimInput>,
}

/// Output of classification
#[derive(Debug, Serialize, Deserialize)]
pub struct ClassifyOutput {
    pub conflicts: Vec<ConflictOutput>,
    pub auto_resolved: usize,
    pub review_required: usize,
    pub total_analyzed: usize,
}

/// A single classified conflict
#[derive(Debug, Serialize, Deserialize)]
pub struct ConflictOutput {
    pub subject: String,
    pub predicate: String,
    pub context: String,
    pub conflict_type: ConflictType,
    pub confidence: f64,
    pub strategy: String,
    pub actor_hierarchy: Vec<ActorRanking>,
    pub temporal_pattern: String,
    pub auto_resolved: bool,
    pub source_ids: Vec<String>,
}

/// Smart classifier that performs conflict classification on claim groups
pub struct SmartClassifier {
    temporal: TemporalAnalyzer,
    config: TemporalConfig,
}

impl SmartClassifier {
    pub fn new(config: TemporalConfig) -> Self {
        let temporal = TemporalAnalyzer::new(config.clone());
        Self { temporal, config }
    }

    /// Classify all claim groups and return structured results
    pub fn classify(&self, input: &ClassifyInput) -> ClassifyOutput {
        let mut conflicts = Vec::new();
        let mut auto_resolved = 0;
        let mut review_required = 0;
        let mut total_analyzed = 0;

        for group in &input.claim_groups {
            if group.claims.len() <= 1 {
                continue;
            }

            total_analyzed += 1;
            let conflict = self.classify_group(group, input.now_ms);

            if conflict.auto_resolved {
                auto_resolved += 1;
            } else if conflict.conflict_type == ConflictType::Review {
                review_required += 1;
            }

            conflicts.push(conflict);
        }

        ClassifyOutput {
            conflicts,
            auto_resolved,
            review_required,
            total_analyzed,
        }
    }

    /// Classify a single group of claims
    fn classify_group(&self, group: &ClaimGroup, now_ms: i64) -> ConflictOutput {
        let claims = &group.claims;

        // Convert to ClaimWithTiming for confidence calculation
        let claims_with_timing: Vec<ClaimWithTiming> = claims
            .iter()
            .map(|c| ClaimWithTiming {
                actor: c.actor.clone(),
                timestamp_ms: c.timestamp_ms,
                predicate: c.predicate.clone(),
                subject: c.subject.clone(),
                context: c.context.clone(),
            })
            .collect();

        // Calculate confidence
        let calculator = ConfidenceCalculator::new(&self.temporal);
        let confidence = calculator.calculate(&claims_with_timing, now_ms);

        // Determine resolution type
        let conflict_type = self.determine_resolution_type(claims);

        // Determine strategy (may override based on confidence)
        let strategy = if calculator.requires_review(confidence) {
            "human_review".to_string()
        } else {
            conflict_type.resolution_strategy().to_string()
        };

        // Analyze temporal pattern
        let timings: Vec<_> = claims_with_timing
            .iter()
            .map(|c| super::temporal::ClaimTiming {
                actor: c.actor.clone(),
                timestamp_ms: c.timestamp_ms,
                predicate: c.predicate.clone(),
            })
            .collect();
        let temporal_pattern = self.temporal.analyze_pattern(&timings).to_string();

        // Build actor hierarchy
        let actor_hierarchy = self.rank_actors(claims);

        // Collect unique source IDs
        let mut source_ids: Vec<String> = claims
            .iter()
            .map(|c| c.source_id.clone())
            .collect::<std::collections::HashSet<_>>()
            .into_iter()
            .collect();
        source_ids.sort();

        let auto_resolved = conflict_type.is_auto_resolvable();

        ConflictOutput {
            subject: claims[0].subject.clone(),
            predicate: claims[0].predicate.clone(),
            context: claims[0].context.clone(),
            conflict_type,
            confidence,
            strategy,
            actor_hierarchy,
            temporal_pattern,
            auto_resolved,
            source_ids,
        }
    }

    /// Determine the type of conflict resolution needed
    fn determine_resolution_type(&self, claims: &[ClaimInput]) -> ConflictType {
        if self.is_same_actor_evolution(claims) {
            return ConflictType::Evolution;
        }

        if self.is_simultaneous_verification(claims) {
            return ConflictType::Verification;
        }

        if self.is_different_contexts(claims) {
            return ConflictType::Coexistence;
        }

        if self.has_human_supersession(claims) {
            return ConflictType::Supersession;
        }

        ConflictType::Review
    }

    /// Same actor updated their claim over time (with meaningful time gaps)
    fn is_same_actor_evolution(&self, claims: &[ClaimInput]) -> bool {
        if claims.len() < 2 {
            return false;
        }

        let first_actor = &claims[0].actor;
        if !claims.iter().all(|c| &c.actor == first_actor) {
            return false;
        }

        // Check for meaningful gaps between timestamps
        let mut timestamps: Vec<i64> = claims.iter().map(|c| c.timestamp_ms).collect();
        timestamps.sort();

        for i in 1..timestamps.len() {
            let gap = timestamps[i] - timestamps[i - 1];
            if gap > self.config.verification_window_ms {
                return true;
            }
        }

        false
    }

    /// Multiple actors agree simultaneously
    fn is_simultaneous_verification(&self, claims: &[ClaimInput]) -> bool {
        if claims.len() < 2 {
            return false;
        }

        // All claims must have same predicate
        let first_predicate = &claims[0].predicate;
        if !claims.iter().all(|c| &c.predicate == first_predicate) {
            return false;
        }

        // All timestamps within verification window
        let first_ts = claims[0].timestamp_ms;
        claims
            .iter()
            .all(|c| self.temporal.is_simultaneous(first_ts, c.timestamp_ms))
    }

    /// Claims span different contexts
    fn is_different_contexts(&self, claims: &[ClaimInput]) -> bool {
        let mut contexts = std::collections::HashSet::new();
        for claim in claims {
            contexts.insert(&claim.context);
        }
        contexts.len() > 1
    }

    /// A human actor overrides non-human actors
    fn has_human_supersession(&self, claims: &[ClaimInput]) -> bool {
        let mut has_human = false;
        let mut has_non_human = false;

        for claim in claims {
            let cred = ActorCredibility::from_actor(&claim.actor);
            if cred.is_human() {
                has_human = true;
            } else {
                has_non_human = true;
            }
        }

        has_human && has_non_human
    }

    /// Rank actors by credibility (highest first)
    fn rank_actors(&self, claims: &[ClaimInput]) -> Vec<ActorRanking> {
        let mut rankings: Vec<ActorRanking> = claims
            .iter()
            .map(|c| {
                let credibility = ActorCredibility::from_actor(&c.actor);
                ActorRanking {
                    actor: c.actor.clone(),
                    credibility,
                    timestamp: Some(c.timestamp_ms),
                }
            })
            .collect();

        // Sort by credibility descending
        rankings.sort_by(|a, b| b.credibility.cmp(&a.credibility));
        rankings
    }
}

/// Top-level function: classify claim groups from JSON input, return JSON output.
/// This is the function exposed through WASM.
pub fn classify_claims(input: &str) -> String {
    let parsed: ClassifyInput = match serde_json::from_str(input) {
        Ok(v) => v,
        Err(e) => {
            return format!(
                r#"{{"error":"invalid classify input: {}"}}"#,
                e.to_string().replace('"', "\\\"")
            );
        }
    };

    let classifier = SmartClassifier::new(parsed.config.clone());
    let output = classifier.classify(&parsed);

    match serde_json::to_string(&output) {
        Ok(json) => json,
        Err(e) => format!(r#"{{"error":"serialization failed: {}"}}"#, e),
    }
}

// Missing test coverage:
// - Resolution priority ordering: claims that match multiple types (e.g. same actor +
//   different contexts) should resolve to the highest-priority match. The priority chain
//   is Evolution > Verification > Coexistence > Supersession > Review.
// - Boundary conditions: claims exactly at verification_window_ms apart (evolution vs
//   verification edge), custom TemporalConfig values.
// - Multiple claim groups in a single input (auto_resolved/review_required counters).
// - Actor hierarchy ordering within a conflict output.
// - Low-confidence override: confidence below review_threshold should produce
//   "human_review" strategy regardless of conflict type.
// - Empty claim_groups input.

#[cfg(test)]
mod tests {
    use super::*;

    fn make_claim(
        subject: &str,
        predicate: &str,
        context: &str,
        actor: &str,
        ts: i64,
    ) -> ClaimInput {
        ClaimInput {
            subject: subject.to_string(),
            predicate: predicate.to_string(),
            context: context.to_string(),
            actor: actor.to_string(),
            timestamp_ms: ts,
            source_id: format!("as-{}", ts),
        }
    }

    #[test]
    fn same_actor_evolution() {
        let now = 1_000_000_000;
        let input = ClassifyInput {
            claim_groups: vec![ClaimGroup {
                key: "ALICE|is_dev|GitHub|alice".to_string(),
                claims: vec![
                    make_claim(
                        "ALICE",
                        "is_junior_dev",
                        "GitHub",
                        "human:alice",
                        now - 200_000,
                    ),
                    make_claim(
                        "ALICE",
                        "is_senior_dev",
                        "GitHub",
                        "human:alice",
                        now - 1000,
                    ),
                ],
            }],
            config: TemporalConfig::default(),
            now_ms: now,
        };

        let classifier = SmartClassifier::new(input.config.clone());
        let output = classifier.classify(&input);

        assert_eq!(output.total_analyzed, 1);
        assert_eq!(output.auto_resolved, 1);
        assert_eq!(output.review_required, 0);

        let c = &output.conflicts[0];
        assert_eq!(c.conflict_type, ConflictType::Evolution);
        assert_eq!(c.strategy, "show_latest");
        assert!(c.auto_resolved);
        assert!(c.confidence > 0.8, "evolution confidence={}", c.confidence);
    }

    #[test]
    fn simultaneous_verification() {
        let now = 1_000_000_000;
        let input = ClassifyInput {
            claim_groups: vec![ClaimGroup {
                key: "ALICE|is_author|GitHub".to_string(),
                claims: vec![
                    make_claim("ALICE", "is_author", "GitHub", "human:alice", now - 10_000),
                    make_claim("ALICE", "is_author", "GitHub", "system:ci", now - 5_000),
                ],
            }],
            config: TemporalConfig::default(),
            now_ms: now,
        };

        let classifier = SmartClassifier::new(input.config.clone());
        let output = classifier.classify(&input);

        let c = &output.conflicts[0];
        assert_eq!(c.conflict_type, ConflictType::Verification);
        assert_eq!(c.strategy, "show_all_sources");
        assert!(c.auto_resolved);
        assert!(
            c.confidence > 0.7,
            "verification confidence={}",
            c.confidence
        );
    }

    #[test]
    fn different_contexts_coexistence() {
        let now = 1_000_000_000;
        let input = ClassifyInput {
            claim_groups: vec![ClaimGroup {
                key: "ALICE|role".to_string(),
                claims: vec![
                    make_claim("ALICE", "is_dev", "GitHub", "human:alice", now - 10_000),
                    make_claim("ALICE", "is_maintainer", "GitLab", "human:bob", now - 5_000),
                ],
            }],
            config: TemporalConfig::default(),
            now_ms: now,
        };

        let classifier = SmartClassifier::new(input.config.clone());
        let output = classifier.classify(&input);

        let c = &output.conflicts[0];
        assert_eq!(c.conflict_type, ConflictType::Coexistence);
        assert_eq!(c.strategy, "show_all_contexts");
        assert!(c.auto_resolved);
        assert!(
            c.confidence > 0.9,
            "coexistence confidence={}",
            c.confidence
        );
    }

    #[test]
    fn human_supersedes_llm() {
        let now = 1_000_000_000;
        let input = ClassifyInput {
            claim_groups: vec![ClaimGroup {
                key: "ALICE|role|GitHub".to_string(),
                claims: vec![
                    make_claim(
                        "ALICE",
                        "is_junior_dev",
                        "GitHub",
                        "llm:gpt-4",
                        now - 10_000,
                    ),
                    make_claim(
                        "ALICE",
                        "is_senior_dev",
                        "GitHub",
                        "human:alice",
                        now - 5_000,
                    ),
                ],
            }],
            config: TemporalConfig::default(),
            now_ms: now,
        };

        let classifier = SmartClassifier::new(input.config.clone());
        let output = classifier.classify(&input);

        let c = &output.conflicts[0];
        assert_eq!(c.conflict_type, ConflictType::Supersession);
        assert_eq!(c.strategy, "show_highest_authority");
        assert!(c.auto_resolved);
        assert!(
            c.confidence > 0.9,
            "supersession confidence={}",
            c.confidence
        );
    }

    #[test]
    fn single_claim_no_conflict() {
        let now = 1_000_000_000;
        let input = ClassifyInput {
            claim_groups: vec![ClaimGroup {
                key: "ALICE|is_dev|GitHub".to_string(),
                claims: vec![make_claim("ALICE", "is_dev", "GitHub", "human:alice", now)],
            }],
            config: TemporalConfig::default(),
            now_ms: now,
        };

        let classifier = SmartClassifier::new(input.config.clone());
        let output = classifier.classify(&input);

        assert_eq!(output.total_analyzed, 0);
        assert_eq!(output.auto_resolved, 0);
        assert_eq!(output.review_required, 0);
        assert!(output.conflicts.is_empty());
    }

    #[test]
    fn classify_claims_json_roundtrip() {
        let now = 1_000_000_000_i64;
        let input_json = serde_json::json!({
            "claim_groups": [{
                "key": "ALICE|is_dev|GitHub",
                "claims": [
                    {"subject": "ALICE", "predicate": "is_dev", "context": "GitHub", "actor": "human:alice", "timestamp_ms": now - 200_000, "source_id": "as-1"},
                    {"subject": "ALICE", "predicate": "is_dev", "context": "GitHub", "actor": "human:alice", "timestamp_ms": now - 1000, "source_id": "as-2"}
                ]
            }],
            "config": {
                "verification_window_ms": 60000,
                "evolution_window_ms": 86400000,
                "obsolescence_window_ms": 31536000000_i64
            },
            "now_ms": now
        });

        let result = classify_claims(&input_json.to_string());
        let parsed: serde_json::Value = serde_json::from_str(&result).unwrap();

        assert!(parsed["error"].is_null(), "unexpected error: {}", result);
        assert_eq!(parsed["total_analyzed"], 1);
        assert_eq!(parsed["conflicts"][0]["conflict_type"], "Evolution");
        assert_eq!(parsed["conflicts"][0]["strategy"], "show_latest");
        assert!(parsed["conflicts"][0]["auto_resolved"].as_bool().unwrap());
    }

    #[test]
    fn classify_claims_invalid_json() {
        let result = classify_claims("not json");
        let parsed: serde_json::Value = serde_json::from_str(&result).unwrap();
        assert!(parsed["error"]
            .as_str()
            .unwrap()
            .contains("invalid classify input"));
    }
}
