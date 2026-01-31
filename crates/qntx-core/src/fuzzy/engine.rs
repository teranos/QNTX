//! FuzzyEngine - Core fuzzy matching implementation

use super::strategies;
use ahash::AHasher;
use serde::{Deserialize, Serialize};
use std::hash::{Hash, Hasher};

#[cfg(all(not(target_arch = "wasm32"), feature = "parallel"))]
use rayon::prelude::*;

/// A ranked match result
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct FuzzyMatch {
    pub value: String,
    pub score: f64,
    pub strategy: &'static str,
}

impl FuzzyMatch {
    fn new(value: String, score: f64, strategy: &'static str) -> Self {
        Self {
            value,
            score,
            strategy,
        }
    }
}

/// Vocabulary type for searching
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum VocabularyType {
    Predicates,
    Contexts,
}

/// Engine configuration
#[derive(Debug, Clone)]
pub struct EngineConfig {
    pub min_score: f64,
    pub max_results: usize,
    pub max_edit_distance: usize,
    pub min_fuzzy_length: usize,
    pub parallel_threshold: usize,
    pub enable_phonetic: bool,
}

impl Default for EngineConfig {
    fn default() -> Self {
        Self {
            min_score: 0.6,
            max_results: 20,
            max_edit_distance: 2,
            min_fuzzy_length: 3,
            parallel_threshold: 1000,
            #[cfg(all(not(target_arch = "wasm32"), feature = "phonetic"))]
            enable_phonetic: true,
            #[cfg(any(target_arch = "wasm32", not(feature = "phonetic")))]
            enable_phonetic: false,
        }
    }
}

/// Thread-safe fuzzy matching engine
///
/// On native with `parallel` feature, uses rayon for large vocabularies.
/// On WASM, uses sequential matching.
pub struct FuzzyEngine {
    predicates: Vec<String>,
    contexts: Vec<String>,
    predicates_lower: Vec<String>,
    contexts_lower: Vec<String>,
    index_hash: String,
    config: EngineConfig,
}

impl Default for FuzzyEngine {
    fn default() -> Self {
        Self::new()
    }
}

impl FuzzyEngine {
    /// Create a new FuzzyEngine with default configuration
    pub fn new() -> Self {
        Self::with_config(EngineConfig::default())
    }

    /// Create with custom configuration
    pub fn with_config(config: EngineConfig) -> Self {
        Self {
            predicates: Vec::new(),
            contexts: Vec::new(),
            predicates_lower: Vec::new(),
            contexts_lower: Vec::new(),
            index_hash: String::new(),
            config,
        }
    }

    /// Rebuild the index with new vocabulary
    /// Returns (predicate_count, context_count, hash)
    pub fn rebuild_index(
        &mut self,
        predicates: Vec<String>,
        contexts: Vec<String>,
    ) -> (usize, usize, String) {
        // Deduplicate and sort
        let mut predicates = predicates;
        predicates.sort();
        predicates.dedup();

        let mut contexts = contexts;
        contexts.sort();
        contexts.dedup();

        // Pre-compute lowercase versions
        let predicates_lower: Vec<String> = predicates.iter().map(|s| s.to_lowercase()).collect();
        let contexts_lower: Vec<String> = contexts.iter().map(|s| s.to_lowercase()).collect();

        // Compute hash
        let hash = self.compute_hash(&predicates, &contexts);

        let pred_count = predicates.len();
        let ctx_count = contexts.len();

        self.predicates = predicates;
        self.contexts = contexts;
        self.predicates_lower = predicates_lower;
        self.contexts_lower = contexts_lower;
        self.index_hash = hash.clone();

        (pred_count, ctx_count, hash)
    }

    /// Search predicates vocabulary
    pub fn search_predicates(&self, query: &str, limit: usize, min_score: f64) -> Vec<FuzzyMatch> {
        self.find_matches(query, VocabularyType::Predicates, limit, min_score)
    }

    /// Search contexts vocabulary
    pub fn search_contexts(&self, query: &str, limit: usize, min_score: f64) -> Vec<FuzzyMatch> {
        self.find_matches(query, VocabularyType::Contexts, limit, min_score)
    }

    /// Find matches in the specified vocabulary
    pub fn find_matches(
        &self,
        query: &str,
        vocab_type: VocabularyType,
        limit: usize,
        min_score: f64,
    ) -> Vec<FuzzyMatch> {
        let query_lower = query.trim().to_lowercase();
        if query_lower.is_empty() {
            return Vec::new();
        }

        let (vocabulary, vocabulary_lower) = match vocab_type {
            VocabularyType::Predicates => (&self.predicates, &self.predicates_lower),
            VocabularyType::Contexts => (&self.contexts, &self.contexts_lower),
        };

        // Platform-specific matching
        let mut matches =
            self.match_vocabulary(&query_lower, vocabulary, vocabulary_lower, min_score);

        // Sort by score descending, then by value for stability
        matches.sort_by(|a, b| {
            b.score
                .partial_cmp(&a.score)
                .unwrap_or(std::cmp::Ordering::Equal)
                .then_with(|| a.value.cmp(&b.value))
        });

        matches.truncate(limit);
        matches
    }

    /// Match against vocabulary - parallel on native, sequential on WASM
    #[cfg(all(not(target_arch = "wasm32"), feature = "parallel"))]
    fn match_vocabulary(
        &self,
        query_lower: &str,
        vocabulary: &[String],
        vocabulary_lower: &[String],
        min_score: f64,
    ) -> Vec<FuzzyMatch> {
        if vocabulary_lower.len() >= self.config.parallel_threshold {
            // Parallel matching with rayon
            vocabulary_lower
                .par_iter()
                .enumerate()
                .filter_map(|(idx, item_lower)| {
                    self.score_single(query_lower, item_lower, &vocabulary[idx])
                })
                .filter(|m| m.score >= min_score)
                .collect()
        } else {
            // Sequential for small vocabularies
            self.match_sequential(query_lower, vocabulary, vocabulary_lower, min_score)
        }
    }

    /// Sequential matching (WASM or when parallel feature disabled)
    #[cfg(any(target_arch = "wasm32", not(feature = "parallel")))]
    fn match_vocabulary(
        &self,
        query_lower: &str,
        vocabulary: &[String],
        vocabulary_lower: &[String],
        min_score: f64,
    ) -> Vec<FuzzyMatch> {
        self.match_sequential(query_lower, vocabulary, vocabulary_lower, min_score)
    }

    /// Sequential matching implementation
    fn match_sequential(
        &self,
        query_lower: &str,
        vocabulary: &[String],
        vocabulary_lower: &[String],
        min_score: f64,
    ) -> Vec<FuzzyMatch> {
        vocabulary_lower
            .iter()
            .enumerate()
            .filter_map(|(idx, item_lower)| {
                self.score_single(query_lower, item_lower, &vocabulary[idx])
            })
            .filter(|m| m.score >= min_score)
            .collect()
    }

    /// Score a single item against the query
    fn score_single(
        &self,
        query_lower: &str,
        item_lower: &str,
        original_value: &str,
    ) -> Option<FuzzyMatch> {
        strategies::score_match(
            query_lower,
            item_lower,
            self.config.min_fuzzy_length,
            self.config.max_edit_distance,
            self.config.enable_phonetic,
        )
        .map(|m| FuzzyMatch::new(original_value.to_string(), m.score, m.strategy))
    }

    /// Get current index hash
    pub fn get_index_hash(&self) -> &str {
        &self.index_hash
    }

    /// Get vocabulary counts
    pub fn get_counts(&self) -> (usize, usize) {
        (self.predicates.len(), self.contexts.len())
    }

    /// Check if index has vocabulary
    pub fn is_ready(&self) -> bool {
        !self.predicates.is_empty() || !self.contexts.is_empty()
    }

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

#[cfg(test)]
mod tests {
    use super::*;

    fn test_engine() -> FuzzyEngine {
        let mut engine = FuzzyEngine::new();
        engine.rebuild_index(
            vec![
                "is_author_of".to_string(),
                "is_maintainer_of".to_string(),
                "works_at".to_string(),
            ],
            vec!["GitHub".to_string(), "GitLab".to_string()],
        );
        engine
    }

    #[test]
    fn test_exact_match() {
        let engine = test_engine();
        let matches = engine.search_predicates("works_at", 10, 0.6);
        assert!(!matches.is_empty());
        assert_eq!(matches[0].value, "works_at");
        assert_eq!(matches[0].score, 1.0);
    }

    #[test]
    fn test_prefix_match() {
        let engine = test_engine();
        let matches = engine.search_predicates("is_", 10, 0.5);
        assert!(matches.len() >= 2);
        assert!(matches.iter().all(|m| m.value.starts_with("is_")));
    }

    #[test]
    fn test_word_boundary() {
        let engine = test_engine();
        let matches = engine.search_predicates("author", 10, 0.6);
        assert!(!matches.is_empty());
        assert!(matches.iter().any(|m| m.value == "is_author_of"));
    }

    #[test]
    fn test_context_search() {
        let engine = test_engine();
        let matches = engine.search_contexts("git", 10, 0.5);
        assert!(!matches.is_empty());
    }

    #[test]
    fn test_empty_query() {
        let engine = test_engine();
        let matches = engine.search_predicates("", 10, 0.6);
        assert!(matches.is_empty());
    }

    #[test]
    fn test_no_matches() {
        let engine = test_engine();
        let matches = engine.search_predicates("xyz123", 10, 0.9);
        assert!(matches.is_empty());
    }
}
