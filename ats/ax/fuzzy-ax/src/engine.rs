//! FuzzyEngine - Core fuzzy matching implementation
//!
//! Provides multi-strategy fuzzy matching with configurable scoring thresholds.
//! Strategies are applied in order of specificity:
//! 1. Exact match (score: 1.0)
//! 2. Prefix match (score: 0.9)
//! 3. Substring match (score: 0.7)
//! 4. Jaro-Winkler similarity (score: 0.6-0.85)
//! 5. Levenshtein edit distance (score: 0.6-0.8)

use std::time::Instant;

use ahash::AHasher;
use parking_lot::RwLock;
use serde_json::Value;
use std::hash::{Hash, Hasher};
use strsim::{jaro_winkler, levenshtein};

/// A ranked match result with score and strategy information
#[derive(Debug, Clone)]
pub struct RankedMatch {
    pub value: String,
    pub score: f64,
    pub strategy: &'static str,
}

impl RankedMatch {
    fn new(value: String, score: f64, strategy: &'static str) -> Self {
        Self {
            value,
            score,
            strategy,
        }
    }
}

/// A match result for RichStringFields search
#[derive(Debug, Clone)]
pub struct AttributeMatch {
    pub node_id: String,        // The ID of the attestation/node
    pub field_name: String,     // The name of the matched field
    pub field_value: String,    // The full value of the matched field
    pub excerpt: String,        // An excerpt showing the match in context
    pub score: f64,             // Match score (higher is better)
    pub strategy: &'static str, // The matching strategy used
}

impl AttributeMatch {
    fn new(
        node_id: String,
        field_name: String,
        field_value: String,
        excerpt: String,
        score: f64,
        strategy: &'static str,
    ) -> Self {
        Self {
            node_id,
            field_name,
            field_value,
            excerpt,
            score,
            strategy,
        }
    }
}

/// Configuration for the fuzzy matching engine
#[derive(Debug, Clone)]
pub struct EngineConfig {
    /// Minimum score threshold (0.0-1.0)
    pub min_score: f64,
    /// Maximum results to return
    pub max_results: usize,
    /// Maximum edit distance for Levenshtein matching
    pub max_edit_distance: usize,
    /// Minimum query length for fuzzy matching (shorter queries use exact/prefix only)
    pub min_fuzzy_length: usize,
}

impl Default for EngineConfig {
    fn default() -> Self {
        Self {
            min_score: 0.6,
            max_results: 20,
            max_edit_distance: 2,
            min_fuzzy_length: 3,
        }
    }
}

/// Thread-safe fuzzy matching engine with in-memory vocabulary index
pub struct FuzzyEngine {
    predicates: RwLock<Vec<String>>,
    contexts: RwLock<Vec<String>>,

    // Lowercase versions for faster matching (computed once)
    predicates_lower: RwLock<Vec<String>>,
    contexts_lower: RwLock<Vec<String>>,

    // Hash of current vocabulary for change detection
    index_hash: RwLock<String>,

    config: EngineConfig,
}

impl FuzzyEngine {
    /// Create a new FuzzyEngine with default configuration
    pub fn new() -> Self {
        Self::with_config(EngineConfig::default())
    }

    /// Create a new FuzzyEngine with custom configuration
    pub fn with_config(config: EngineConfig) -> Self {
        Self {
            predicates: RwLock::new(Vec::new()),
            contexts: RwLock::new(Vec::new()),
            predicates_lower: RwLock::new(Vec::new()),
            contexts_lower: RwLock::new(Vec::new()),
            index_hash: RwLock::new(String::new()),
            config,
        }
    }

    /// Rebuild the index with new vocabulary
    /// Returns (predicate_count, context_count, build_time_ms, hash)
    pub fn rebuild_index(
        &self,
        predicates: Vec<String>,
        contexts: Vec<String>,
    ) -> (usize, usize, u64, String) {
        let start = Instant::now();

        // Deduplicate and sort for consistent hashing (more efficient than HashSet)
        let predicates: Vec<String> = {
            let mut v = predicates;
            v.sort();
            v.dedup();
            v
        };

        let contexts: Vec<String> = {
            let mut v = contexts;
            v.sort();
            v.dedup();
            v
        };

        // Pre-compute lowercase versions
        let predicates_lower: Vec<String> = predicates.iter().map(|s| s.to_lowercase()).collect();
        let contexts_lower: Vec<String> = contexts.iter().map(|s| s.to_lowercase()).collect();

        // Compute hash for change detection
        let hash = self.compute_hash(&predicates, &contexts);

        let pred_count = predicates.len();
        let ctx_count = contexts.len();

        // Update indices (write locks)
        *self.predicates.write() = predicates;
        *self.contexts.write() = contexts;
        *self.predicates_lower.write() = predicates_lower;
        *self.contexts_lower.write() = contexts_lower;
        *self.index_hash.write() = hash.clone();

        let build_time = start.elapsed().as_millis() as u64;

        (pred_count, ctx_count, build_time, hash)
    }

    /// Find matches for a query in the specified vocabulary
    pub fn find_matches(
        &self,
        query: &str,
        vocabulary_type: VocabularyType,
        limit: Option<usize>,
        min_score: Option<f64>,
    ) -> (Vec<RankedMatch>, u64) {
        let start = Instant::now();

        let limit = limit.unwrap_or(self.config.max_results);
        let min_score = min_score.unwrap_or(self.config.min_score);

        let query_lower = query.to_lowercase().trim().to_string();
        if query_lower.is_empty() {
            return (Vec::new(), start.elapsed().as_micros() as u64);
        }

        debug!(
            "Finding matches for query: '{}' (type: {:?}, limit: {}, min_score: {})",
            query, vocabulary_type, limit, min_score
        );

        // Get the appropriate vocabulary
        let (vocabulary, vocabulary_lower) = match vocabulary_type {
            VocabularyType::Predicates => (self.predicates.read(), self.predicates_lower.read()),
            VocabularyType::Contexts => (self.contexts.read(), self.contexts_lower.read()),
        };

        let mut matches = Vec::new();

        for (idx, item_lower) in vocabulary_lower.iter().enumerate() {
            if let Some(m) = self.score_match(&query_lower, item_lower, &vocabulary[idx]) {
                if m.score >= min_score {
                    matches.push(m);
                }
            }
        }

        // Sort by score descending, then by value for stability
        matches.sort_by(|a, b| {
            b.score
                .partial_cmp(&a.score)
                .unwrap_or(std::cmp::Ordering::Equal)
                .then_with(|| a.value.cmp(&b.value))
        });

        // Limit results
        matches.truncate(limit);

        let search_time = start.elapsed().as_micros() as u64;
        (matches, search_time)
    }

    /// Score a single match using multiple strategies
    fn score_match(
        &self,
        query_lower: &str,
        item_lower: &str,
        original_value: &str,
    ) -> Option<RankedMatch> {
        // 1. Exact match
        if query_lower == item_lower {
            return Some(RankedMatch::new(original_value.to_string(), 1.0, "exact"));
        }

        // 2. Prefix match
        if item_lower.starts_with(query_lower) {
            return Some(RankedMatch::new(original_value.to_string(), 0.9, "prefix"));
        }

        // 3. Word boundary match (query matches a complete word)
        // Split on common separators: whitespace, underscore, hyphen
        // Check this BEFORE substring to get better scoring
        for word in item_lower.split(|c: char| c.is_whitespace() || c == '_' || c == '-') {
            if word == query_lower {
                return Some(RankedMatch::new(
                    original_value.to_string(),
                    0.85,
                    "word_boundary",
                ));
            }
        }

        // 4. Substring match
        if item_lower.contains(query_lower) {
            // Score based on position (earlier = better)
            // Safe to unwrap: contains() returned true, so find() must succeed
            let pos = item_lower.find(query_lower).unwrap();
            let pos_penalty = (pos as f64 / item_lower.len() as f64) * 0.1;
            let score = 0.75 - pos_penalty;
            return Some(RankedMatch::new(
                original_value.to_string(),
                score.max(0.65),
                "substring",
            ));
        }

        // For short queries, skip expensive fuzzy matching
        if query_lower.len() < self.config.min_fuzzy_length {
            return None;
        }

        // 5. Jaro-Winkler similarity
        let jw_score = jaro_winkler(query_lower, item_lower);
        if jw_score > 0.85 {
            // Scale to our scoring range
            let score = 0.6 + (jw_score - 0.85) * 1.5; // Maps 0.85-1.0 to 0.6-0.825
            return Some(RankedMatch::new(
                original_value.to_string(),
                score.min(0.82),
                "jaro_winkler",
            ));
        }

        // 6. Levenshtein edit distance (for typo tolerance)
        let edit_dist = levenshtein(query_lower, item_lower);
        if edit_dist <= self.config.max_edit_distance {
            let max_len = query_lower.len().max(item_lower.len());
            if max_len > 0 {
                // Score decreases with edit distance
                let score = 0.8 - (edit_dist as f64 / max_len as f64) * 0.4;
                if score >= 0.6 {
                    return Some(RankedMatch::new(
                        original_value.to_string(),
                        score,
                        "levenshtein",
                    ));
                }
            }
        }

        None
    }

    /// Get current index hash for change detection
    pub fn get_index_hash(&self) -> String {
        self.index_hash.read().clone()
    }

    /// Get vocabulary counts
    pub fn get_counts(&self) -> (usize, usize) {
        (self.predicates.read().len(), self.contexts.read().len())
    }

    /// Check if index is ready (has vocabulary)
    pub fn is_ready(&self) -> bool {
        !self.predicates.read().is_empty() || !self.contexts.read().is_empty()
    }

    /// Compute hash of vocabulary for change detection
    fn compute_hash(&self, predicates: &[String], contexts: &[String]) -> String {
        let mut hasher = AHasher::default();

        for p in predicates {
            p.hash(&mut hasher);
        }
        for c in contexts {
            c.hash(&mut hasher);
        }

        format!("{:016x}", hasher.finish())
    }

    /// Search for matches in RichStringFields of attestations
    ///
    /// Parameters:
    /// - query: The search query
    /// - attributes_json: JSON string containing the attributes object
    /// - rich_string_fields: List of field names to search in
    /// - node_id: The ID of the attestation/node for tracking
    ///
    /// Returns matches with scores and excerpts
    pub fn find_attribute_matches(
        &self,
        query: &str,
        attributes_json: &str,
        rich_string_fields: &[String],
        node_id: &str,
    ) -> Vec<AttributeMatch> {
        if query.trim().is_empty() || rich_string_fields.is_empty() {
            return Vec::new();
        }

        // Parse JSON attributes
        let attributes: Value = match serde_json::from_str(attributes_json) {
            Ok(val) => val,
            Err(_) => return Vec::new(),
        };

        let mut matches = Vec::new();
        let query_lower = query.to_lowercase();

        // Search through each RichStringField
        for field_name in rich_string_fields {
            if let Some(value) = attributes.get(field_name) {
                // Convert value to string
                let str_value = match value {
                    Value::String(s) => s.clone(),
                    Value::Array(arr) => {
                        // Join array elements with spaces
                        arr.iter()
                            .filter_map(|v| v.as_str())
                            .collect::<Vec<_>>()
                            .join(" ")
                    }
                    _ => continue,
                };

                if str_value.is_empty() {
                    continue;
                }

                // Apply fuzzy matching
                if let Some(ranked_match) =
                    self.score_match(&query_lower, &str_value.to_lowercase(), &str_value)
                {
                    let excerpt = self.extract_excerpt(&str_value, query, 150);

                    matches.push(AttributeMatch::new(
                        node_id.to_string(),
                        field_name.clone(),
                        str_value,
                        excerpt,
                        ranked_match.score,
                        ranked_match.strategy,
                    ));
                }
            }
        }

        // Sort by score descending
        matches.sort_by(|a, b| b.score.partial_cmp(&a.score).unwrap());

        // Apply limit from config
        matches.truncate(self.config.max_results);

        matches
    }

    /// Extract an excerpt from text around the match
    fn extract_excerpt(&self, text: &str, query: &str, max_length: usize) -> String {
        let text_lower = text.to_lowercase();
        let query_lower = query.to_lowercase();

        // Find the match position
        let idx = text_lower.find(&query_lower).unwrap_or(0);

        // Calculate excerpt bounds
        let start = if idx > max_length / 2 {
            // Find word boundary
            let target_start = idx.saturating_sub(max_length / 2);
            text[..target_start]
                .rfind(char::is_whitespace)
                .map(|i| i + 1)
                .unwrap_or(target_start)
        } else {
            0
        };

        let end = if idx + query.len() + max_length / 2 < text.len() {
            let target_end = (idx + query.len() + max_length / 2).min(text.len());
            text[idx + query.len()..target_end]
                .find(char::is_whitespace)
                .map(|i| idx + query.len() + i)
                .unwrap_or(target_end)
        } else {
            text.len()
        };

        // Build excerpt
        let mut excerpt = String::new();
        if start > 0 {
            excerpt.push_str("...");
        }
        excerpt.push_str(&text[start..end]);
        if end < text.len() {
            excerpt.push_str("...");
        }

        excerpt
    }
}

impl Default for FuzzyEngine {
    fn default() -> Self {
        Self::new()
    }
}

/// Vocabulary type enum (mirrors protobuf)
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum VocabularyType {
    Predicates,
    Contexts,
}

#[cfg(test)]
mod tests {
    use super::*;

    fn test_engine() -> FuzzyEngine {
        let engine = FuzzyEngine::new();
        engine.rebuild_index(
            vec![
                "is_author_of".to_string(),
                "is_maintainer_of".to_string(),
                "works_at".to_string(),
                "employed_by".to_string(),
                "contributes_to".to_string(),
            ],
            vec![
                "GitHub".to_string(),
                "Microsoft".to_string(),
                "Google".to_string(),
                "Anthropic".to_string(),
            ],
        );
        engine
    }

    #[test]
    fn test_exact_match() {
        let engine = test_engine();
        let (matches, _) = engine.find_matches("works_at", VocabularyType::Predicates, None, None);

        assert!(!matches.is_empty());
        assert_eq!(matches[0].value, "works_at");
        assert_eq!(matches[0].score, 1.0);
        assert_eq!(matches[0].strategy, "exact");
    }

    #[test]
    fn test_prefix_match() {
        let engine = test_engine();
        let (matches, _) = engine.find_matches("is_", VocabularyType::Predicates, None, Some(0.5));

        assert!(matches.len() >= 2);
        assert!(matches.iter().all(|m| m.value.starts_with("is_")));
        assert!(matches[0].strategy == "prefix");
    }

    #[test]
    fn test_substring_match() {
        let engine = test_engine();
        let (matches, _) = engine.find_matches("author", VocabularyType::Predicates, None, None);

        assert!(!matches.is_empty());
        assert!(matches.iter().any(|m| m.value == "is_author_of"));
    }

    #[test]
    fn test_levenshtein_typo() {
        let engine = test_engine();
        // "wroks_at" is 1 edit from "works_at"
        let (matches, _) =
            engine.find_matches("wroks_at", VocabularyType::Predicates, None, Some(0.5));

        assert!(!matches.is_empty());
        assert!(matches.iter().any(|m| m.value == "works_at"));
    }

    #[test]
    fn test_context_matching() {
        let engine = test_engine();
        let (matches, _) = engine.find_matches("git", VocabularyType::Contexts, None, Some(0.5));

        assert!(!matches.is_empty());
        assert!(matches.iter().any(|m| m.value == "GitHub"));
    }

    #[test]
    fn test_min_score_filtering() {
        let engine = test_engine();
        let (matches, _) = engine.find_matches("xyz", VocabularyType::Predicates, None, Some(0.9));

        // No matches should pass 90% threshold for unrelated query
        assert!(matches.is_empty());
    }

    #[test]
    fn test_limit() {
        let engine = test_engine();
        let (matches, _) = engine.find_matches("o", VocabularyType::Predicates, Some(2), Some(0.3));

        assert!(matches.len() <= 2);
    }

    #[test]
    fn test_index_hash_changes() {
        let engine = FuzzyEngine::new();

        engine.rebuild_index(vec!["a".to_string()], vec![]);
        let hash1 = engine.get_index_hash();

        engine.rebuild_index(vec!["b".to_string()], vec![]);
        let hash2 = engine.get_index_hash();

        assert_ne!(hash1, hash2);
    }

    #[test]
    fn test_word_boundary_with_underscore() {
        let engine = test_engine();
        // "author" is a word in "is_author_of" when splitting on underscores
        let (matches, _) = engine.find_matches("author", VocabularyType::Predicates, None, None);

        assert!(!matches.is_empty());
        // Should match via word_boundary strategy for exact word match
        let author_match = matches.iter().find(|m| m.value == "is_author_of");
        assert!(author_match.is_some());
        // Now that word_boundary is checked first, it should always be word_boundary
        assert_eq!(author_match.unwrap().strategy, "word_boundary");
        assert_eq!(author_match.unwrap().score, 0.85);
    }
}
