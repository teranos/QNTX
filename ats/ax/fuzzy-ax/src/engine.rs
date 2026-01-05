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

        let query_lower = query.trim().to_lowercase();
        if query_lower.is_empty() {
            return (Vec::new(), start.elapsed().as_micros() as u64);
        }

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
        if let Some(pos) = item_lower.find(query_lower) {
            // Score based on position (earlier = better)
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

    // ========================================================================
    // Edge case tests
    // ========================================================================

    #[test]
    fn test_empty_query() {
        let engine = test_engine();
        let (matches, _) = engine.find_matches("", VocabularyType::Predicates, None, None);
        assert!(matches.is_empty());
    }

    #[test]
    fn test_whitespace_only_query() {
        let engine = test_engine();
        let (matches, _) = engine.find_matches("   ", VocabularyType::Predicates, None, None);
        assert!(matches.is_empty());
    }

    #[test]
    fn test_empty_vocabulary() {
        let engine = FuzzyEngine::new();
        // Don't rebuild index - vocabulary is empty
        let (matches, _) = engine.find_matches("test", VocabularyType::Predicates, None, None);
        assert!(matches.is_empty());
    }

    #[test]
    fn test_case_insensitive_matching() {
        let engine = test_engine();
        let (matches, _) = engine.find_matches("WORKS_AT", VocabularyType::Predicates, None, None);
        assert!(!matches.is_empty());
        assert_eq!(matches[0].value, "works_at");
        assert_eq!(matches[0].strategy, "exact");
    }

    // ========================================================================
    // Unicode tests
    // ========================================================================

    #[test]
    fn test_unicode_cjk_substring() {
        let engine = FuzzyEngine::new();
        engine.rebuild_index(
            vec![
                "日本語プログラミング".to_string(),
                "中文编程".to_string(),
                "한국어코딩".to_string(),
            ],
            vec![],
        );

        // Substring match for Japanese
        let (matches, _) = engine.find_matches("日本", VocabularyType::Predicates, None, Some(0.5));
        assert!(!matches.is_empty());
        assert_eq!(matches[0].value, "日本語プログラミング");
        assert_eq!(matches[0].strategy, "prefix"); // "日本" is prefix of "日本語..."

        // Substring match for Chinese
        let (matches, _) = engine.find_matches("编程", VocabularyType::Predicates, None, Some(0.5));
        assert!(!matches.is_empty());
        assert_eq!(matches[0].value, "中文编程");
    }

    #[test]
    fn test_unicode_accented_chars() {
        let engine = FuzzyEngine::new();
        engine.rebuild_index(
            vec![
                "café_owner".to_string(),
                "naïve_implementation".to_string(),
                "résumé_parser".to_string(),
            ],
            vec![],
        );

        // Exact substring with accents
        let (matches, _) = engine.find_matches("café", VocabularyType::Predicates, None, Some(0.5));
        assert!(!matches.is_empty());
        assert_eq!(matches[0].value, "café_owner");

        // Word boundary with accents
        let (matches, _) =
            engine.find_matches("naïve", VocabularyType::Predicates, None, Some(0.5));
        assert!(!matches.is_empty());
        assert_eq!(matches[0].value, "naïve_implementation");
    }

    #[test]
    fn test_unicode_emoji() {
        let engine = FuzzyEngine::new();
        engine.rebuild_index(
            vec![
                "rocket_🚀_launch".to_string(),
                "heart_❤️_react".to_string(),
            ],
            vec![],
        );

        let (matches, _) =
            engine.find_matches("rocket", VocabularyType::Predicates, None, Some(0.5));
        assert!(!matches.is_empty());
        assert_eq!(matches[0].value, "rocket_🚀_launch");
    }

    #[test]
    fn test_unicode_mixed_script() {
        let engine = FuzzyEngine::new();
        engine.rebuild_index(
            vec![],
            vec![
                "Tokyo 東京".to_string(),
                "Москва Moscow".to_string(),
                "Αθήνα Athens".to_string(),
            ],
        );

        // Search English part of mixed context
        let (matches, _) = engine.find_matches("Tokyo", VocabularyType::Contexts, None, Some(0.5));
        assert!(!matches.is_empty());
        assert_eq!(matches[0].value, "Tokyo 東京");

        // Search non-Latin part
        let (matches, _) = engine.find_matches("東京", VocabularyType::Contexts, None, Some(0.5));
        assert!(!matches.is_empty());
        assert_eq!(matches[0].value, "Tokyo 東京");

        // Search Cyrillic
        let (matches, _) =
            engine.find_matches("Москва", VocabularyType::Contexts, None, Some(0.5));
        assert!(!matches.is_empty());
        assert_eq!(matches[0].value, "Москва Moscow");
    }

    #[test]
    fn test_unicode_case_folding() {
        let engine = FuzzyEngine::new();
        engine.rebuild_index(
            vec![
                "GROSSE_strasse".to_string(), // German ß
                "İstanbul_city".to_string(),  // Turkish İ (dotted I)
            ],
            vec![],
        );

        // Standard case folding
        let (matches, _) =
            engine.find_matches("grosse", VocabularyType::Predicates, None, Some(0.5));
        assert!(!matches.is_empty());
    }

    // ========================================================================
    // Boundary and stress tests
    // ========================================================================

    #[test]
    fn test_single_char_query() {
        let engine = test_engine();
        // Single char should match via substring but skip fuzzy (min_fuzzy_length=3)
        let (matches, _) = engine.find_matches("a", VocabularyType::Predicates, Some(10), Some(0.5));
        // Should find items containing 'a'
        assert!(matches.iter().any(|m| m.value.contains('a')));
    }

    #[test]
    fn test_very_long_query() {
        let engine = test_engine();
        let long_query = "a".repeat(500);
        let (matches, _) =
            engine.find_matches(&long_query, VocabularyType::Predicates, None, None);
        // Should return empty, not panic
        assert!(matches.is_empty());
    }

    #[test]
    fn test_special_regex_chars() {
        let engine = FuzzyEngine::new();
        engine.rebuild_index(
            vec![
                "match.*pattern".to_string(),
                "test[0-9]+".to_string(),
                "foo|bar".to_string(),
            ],
            vec![],
        );

        // These should be treated as literal strings, not regex
        let (matches, _) =
            engine.find_matches(".*", VocabularyType::Predicates, None, Some(0.5));
        assert!(!matches.is_empty());
        assert!(matches.iter().any(|m| m.value == "match.*pattern"));
    }

    #[test]
    fn test_hyphen_word_boundary() {
        let engine = FuzzyEngine::new();
        engine.rebuild_index(
            vec![
                "user-defined-type".to_string(),
                "pre-processing".to_string(),
                "re-implementation".to_string(),
            ],
            vec![],
        );

        // Word boundary should split on hyphens
        let (matches, _) =
            engine.find_matches("defined", VocabularyType::Predicates, None, Some(0.5));
        assert!(!matches.is_empty());
        assert_eq!(matches[0].strategy, "word_boundary");
    }

    #[test]
    fn test_duplicate_vocabulary_items() {
        let engine = FuzzyEngine::new();
        let (pred_count, _, _, _) = engine.rebuild_index(
            vec![
                "duplicate".to_string(),
                "duplicate".to_string(),
                "duplicate".to_string(),
                "unique".to_string(),
            ],
            vec![],
        );
        // Duplicates should be removed during rebuild
        assert_eq!(pred_count, 2);
    }
}
