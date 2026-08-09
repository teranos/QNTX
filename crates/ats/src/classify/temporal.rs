//! Temporal pattern analysis for claim classification
//!
//! Analyzes time relationships between claims to determine patterns
//! like simultaneous verification, sequential evolution, or distributed claims.

use serde::{Deserialize, Serialize};

/// Configurable time windows for temporal classification.
/// All values are in milliseconds.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TemporalConfig {
    /// Window within which claims are considered simultaneous (default: 60_000ms = 1 minute)
    pub verification_window_ms: i64,
    /// Window within which claims suggest natural evolution (default: 86_400_000ms = 24 hours)
    pub evolution_window_ms: i64,
    /// Window beyond which claims are considered obsolete (default: 31_536_000_000ms = 1 year)
    pub obsolescence_window_ms: i64,
}

impl Default for TemporalConfig {
    fn default() -> Self {
        Self {
            verification_window_ms: 60_000,         // 1 minute
            evolution_window_ms: 86_400_000,        // 24 hours
            obsolescence_window_ms: 31_536_000_000, // ~365 days
        }
    }
}

/// Temporal pattern between a set of claims
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum TemporalPattern {
    /// All claims within verification window
    Simultaneous,
    /// Clear time ordering with gaps
    Sequential,
    /// Some temporal overlap
    Overlapping,
    /// Spread over a long period
    Distributed,
}

impl std::fmt::Display for TemporalPattern {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Simultaneous => write!(f, "simultaneous"),
            Self::Sequential => write!(f, "sequential"),
            Self::Overlapping => write!(f, "overlapping"),
            Self::Distributed => write!(f, "distributed"),
        }
    }
}

/// Timing information for a single claim
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ClaimTiming {
    pub actor: String,
    pub timestamp_ms: i64,
    pub predicate: String,
}

/// Temporal analyzer for claim classification
pub struct TemporalAnalyzer {
    config: TemporalConfig,
}

impl TemporalAnalyzer {
    pub fn new(config: TemporalConfig) -> Self {
        Self { config }
    }

    /// Determine the temporal pattern of a set of claims
    pub fn analyze_pattern(&self, timings: &[ClaimTiming]) -> TemporalPattern {
        if timings.len() <= 1 {
            return TemporalPattern::Simultaneous;
        }

        let mut timestamps: Vec<i64> = timings.iter().map(|t| t.timestamp_ms).collect();
        timestamps.sort();

        let earliest = timestamps[0];
        let latest = timestamps[timestamps.len() - 1];
        let total_span = latest - earliest;

        if total_span <= self.config.verification_window_ms {
            return TemporalPattern::Simultaneous;
        }

        if self.has_sequential_gaps(&timestamps) {
            return TemporalPattern::Sequential;
        }

        if total_span > self.config.evolution_window_ms {
            return TemporalPattern::Distributed;
        }

        TemporalPattern::Overlapping
    }

    /// Check if timestamps have clear gaps (> verification window) between consecutive values
    fn has_sequential_gaps(&self, sorted_timestamps: &[i64]) -> bool {
        for i in 1..sorted_timestamps.len() {
            let gap = sorted_timestamps[i] - sorted_timestamps[i - 1];
            if gap > self.config.verification_window_ms {
                return true;
            }
        }
        false
    }

    /// Check if two timestamps are within the verification window
    pub fn is_simultaneous(&self, t1: i64, t2: i64) -> bool {
        (t1 - t2).unsigned_abs() <= self.config.verification_window_ms as u64
    }

    /// Check if a timespan suggests natural evolution
    pub fn is_evolution_timespan(&self, t1: i64, t2: i64) -> bool {
        let diff = (t1 - t2).unsigned_abs() as i64;
        diff >= self.config.verification_window_ms && diff <= self.config.evolution_window_ms
    }

    /// Calculate recency score (0.0-1.0) based on age relative to a reference time.
    /// `now_ms` is the current time in milliseconds.
    pub fn recency_score(&self, timestamp_ms: i64, now_ms: i64) -> f64 {
        let age = now_ms - timestamp_ms;
        if age < 0 {
            return 1.0; // future timestamps get full score
        }

        if age <= self.config.verification_window_ms {
            return 1.0;
        }

        if age <= self.config.evolution_window_ms {
            let ratio = age as f64 / self.config.evolution_window_ms as f64;
            return 1.0 - (ratio * 0.5); // 1.0 -> 0.5
        }

        if age <= self.config.obsolescence_window_ms {
            let ratio = age as f64 / self.config.obsolescence_window_ms as f64;
            return 0.5 - (ratio * 0.4); // 0.5 -> 0.1
        }

        0.1 // obsolete
    }

    /// Calculate temporal confidence (0.0-1.0) based on the temporal pattern
    pub fn temporal_confidence(&self, timings: &[ClaimTiming]) -> f64 {
        if timings.len() <= 1 {
            return 1.0;
        }

        match self.analyze_pattern(timings) {
            TemporalPattern::Simultaneous => 0.9,
            TemporalPattern::Sequential => 0.8,
            TemporalPattern::Overlapping => 0.6,
            TemporalPattern::Distributed => 0.4,
        }
    }

    /// Get the most recent timestamp from a set of timings
    pub fn most_recent(&self, timings: &[ClaimTiming]) -> Option<i64> {
        timings.iter().map(|t| t.timestamp_ms).max()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn default_analyzer() -> TemporalAnalyzer {
        TemporalAnalyzer::new(TemporalConfig::default())
    }

    fn timing(actor: &str, ts: i64) -> ClaimTiming {
        ClaimTiming {
            actor: actor.to_string(),
            timestamp_ms: ts,
            predicate: "test".to_string(),
        }
    }

    #[test]
    fn single_claim_is_simultaneous() {
        let ta = default_analyzer();
        let timings = vec![timing("alice", 1000)];
        assert_eq!(ta.analyze_pattern(&timings), TemporalPattern::Simultaneous);
    }

    #[test]
    fn claims_within_verification_window_are_simultaneous() {
        let ta = default_analyzer();
        let timings = vec![timing("alice", 1000), timing("bob", 50_000)]; // 49s apart
        assert_eq!(ta.analyze_pattern(&timings), TemporalPattern::Simultaneous);
    }

    #[test]
    fn claims_with_gaps_are_sequential() {
        let ta = default_analyzer();
        let timings = vec![
            timing("alice", 0),
            timing("alice", 120_000), // 2 min gap
        ];
        assert_eq!(ta.analyze_pattern(&timings), TemporalPattern::Sequential);
    }

    #[test]
    fn claims_with_large_gap_are_sequential() {
        // With 2 claims and a gap > verification window, sequential wins
        // (sequential is checked before distributed)
        let ta = default_analyzer();
        let timings = vec![
            timing("alice", 0),
            timing("bob", 100_000_000), // ~1.15 days gap
        ];
        assert_eq!(ta.analyze_pattern(&timings), TemporalPattern::Sequential);
    }

    #[test]
    fn overlapping_chain_spanning_days_is_distributed() {
        // Many claims within verification window of neighbors but spanning > evolution window total.
        // Each consecutive pair is 50s apart (within 60s verification window), but total span = 250s * 346 > 24h.
        // Simpler: use a custom config with a small evolution window for testability.
        let ta = TemporalAnalyzer::new(TemporalConfig {
            verification_window_ms: 60_000,
            evolution_window_ms: 200_000, // 200 seconds
            obsolescence_window_ms: 31_536_000_000,
        });
        // 6 claims, each 50s apart. Total span = 250s > evolution_window (200s).
        // No gap exceeds verification_window (60s), so sequential doesn't match.
        let timings = vec![
            timing("a", 0),
            timing("b", 50_000),
            timing("c", 100_000),
            timing("d", 150_000),
            timing("e", 200_000),
            timing("f", 250_000),
        ];
        assert_eq!(ta.analyze_pattern(&timings), TemporalPattern::Distributed);
    }

    #[test]
    fn recency_score_recent() {
        let ta = default_analyzer();
        let now = 1_000_000;
        // 30 seconds ago
        assert_eq!(ta.recency_score(now - 30_000, now), 1.0);
    }

    #[test]
    fn recency_score_evolution_range() {
        let ta = default_analyzer();
        let now = 1_000_000_000;
        // 12 hours ago (halfway through evolution window)
        let score = ta.recency_score(now - 43_200_000, now);
        assert!(score > 0.5 && score < 1.0);
    }

    #[test]
    fn recency_score_obsolete() {
        let ta = default_analyzer();
        let now = 100_000_000_000;
        // 2 years ago
        let score = ta.recency_score(now - 63_072_000_000, now);
        assert_eq!(score, 0.1);
    }

    #[test]
    fn temporal_confidence_simultaneous_high() {
        let ta = default_analyzer();
        let timings = vec![timing("alice", 1000), timing("bob", 2000)];
        assert_eq!(ta.temporal_confidence(&timings), 0.9);
    }
}
