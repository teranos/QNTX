//! Fuzzy matching strategies
//!
//! Platform-specific implementations:
//! - Native: SIMD substring via memchr, phonetic via rphonetic
//! - WASM: Pure Rust fallbacks

use strsim::{jaro_winkler, levenshtein};

/// Match result from a strategy
#[derive(Debug, Clone)]
pub struct StrategyMatch {
    pub score: f64,
    pub strategy: &'static str,
}

/// Try exact match (score: 1.0)
#[inline]
pub fn exact_match(query: &str, item: &str) -> Option<StrategyMatch> {
    if query == item {
        Some(StrategyMatch {
            score: 1.0,
            strategy: "exact",
        })
    } else {
        None
    }
}

/// Try prefix match (score: 0.9)
#[inline]
pub fn prefix_match(query: &str, item: &str) -> Option<StrategyMatch> {
    if item.starts_with(query) {
        Some(StrategyMatch {
            score: 0.9,
            strategy: "prefix",
        })
    } else {
        None
    }
}

/// Try word boundary match (score: 0.85)
/// Matches when query equals a complete word in item (split on whitespace, underscore, hyphen)
#[inline]
pub fn word_boundary_match(query: &str, item: &str) -> Option<StrategyMatch> {
    for word in item.split(|c: char| c.is_whitespace() || c == '_' || c == '-') {
        if word == query {
            return Some(StrategyMatch {
                score: 0.85,
                strategy: "word_boundary",
            });
        }
    }
    None
}

/// Try substring match (score: 0.65-0.75)
/// Uses SIMD via memchr on native, pure Rust on WASM
#[inline]
pub fn substring_match(query: &str, item: &str) -> Option<StrategyMatch> {
    #[cfg(all(not(target_arch = "wasm32"), feature = "simd"))]
    {
        // SIMD-accelerated substring search
        use memchr::memmem;
        let finder = memmem::Finder::new(query.as_bytes());
        if let Some(pos) = finder.find(item.as_bytes()) {
            let pos_penalty = (pos as f64 / item.len() as f64) * 0.1;
            let score = (0.75 - pos_penalty).max(0.65);
            return Some(StrategyMatch {
                score,
                strategy: "substring",
            });
        }
    }

    #[cfg(any(target_arch = "wasm32", not(feature = "simd")))]
    {
        // Pure Rust fallback
        if let Some(pos) = item.find(query) {
            let pos_penalty = (pos as f64 / item.len() as f64) * 0.1;
            let score = (0.75 - pos_penalty).max(0.65);
            return Some(StrategyMatch {
                score,
                strategy: "substring",
            });
        }
    }

    None
}

/// Try phonetic match using Double Metaphone (score: 0.70-0.75)
/// Only available on native with phonetic feature
#[cfg(all(not(target_arch = "wasm32"), feature = "phonetic"))]
pub fn phonetic_match(query: &str, item: &str) -> Option<StrategyMatch> {
    use rphonetic::DoubleMetaphone;

    // Double Metaphone only works reliably with ASCII
    if !query.is_ascii() || !item.is_ascii() {
        return None;
    }

    let encoder = DoubleMetaphone::default();

    // For compound terms, check word-by-word
    let query_words: Vec<&str> = query
        .split(|c: char| c.is_whitespace() || c == '_' || c == '-')
        .filter(|w| !w.is_empty())
        .collect();
    let item_words: Vec<&str> = item
        .split(|c: char| c.is_whitespace() || c == '_' || c == '-')
        .filter(|w| !w.is_empty())
        .collect();

    // Single word query - check if it matches any word in item
    if query_words.len() == 1 {
        let q_result = encoder.double_metaphone(query_words[0]);
        let q_primary = q_result.primary();
        let q_alternate = q_result.alternate();

        for item_word in &item_words {
            let i_result = encoder.double_metaphone(item_word);
            let i_primary = i_result.primary();
            let i_alternate = i_result.alternate();

            if !q_primary.is_empty() && q_primary == i_primary {
                return Some(StrategyMatch {
                    score: 0.75,
                    strategy: "phonetic",
                });
            }
            if !q_alternate.is_empty() && q_alternate == i_primary {
                return Some(StrategyMatch {
                    score: 0.70,
                    strategy: "phonetic",
                });
            }
            if !i_alternate.is_empty() && q_primary == i_alternate {
                return Some(StrategyMatch {
                    score: 0.70,
                    strategy: "phonetic",
                });
            }
        }
    }

    // Multi-word - check if all words match
    if query_words.len() > 1 && query_words.len() == item_words.len() {
        let mut all_match = true;
        for (qw, iw) in query_words.iter().zip(item_words.iter()) {
            let q_result = encoder.double_metaphone(qw);
            let i_result = encoder.double_metaphone(iw);
            if q_result.primary() != i_result.primary() {
                all_match = false;
                break;
            }
        }
        if all_match {
            return Some(StrategyMatch {
                score: 0.72,
                strategy: "phonetic",
            });
        }
    }

    None
}

/// Stub for phonetic match when feature is disabled
#[cfg(any(target_arch = "wasm32", not(feature = "phonetic")))]
pub fn phonetic_match(_query: &str, _item: &str) -> Option<StrategyMatch> {
    None
}

/// Try Jaro-Winkler similarity (score: 0.6-0.825)
#[inline]
pub fn jaro_winkler_match(query: &str, item: &str) -> Option<StrategyMatch> {
    let jw_score = jaro_winkler(query, item);
    if jw_score > 0.85 {
        // Scale to our scoring range: maps 0.85-1.0 to 0.6-0.825
        let score = (0.6 + (jw_score - 0.85) * 1.5).min(0.82);
        Some(StrategyMatch {
            score,
            strategy: "jaro_winkler",
        })
    } else {
        None
    }
}

/// Try Levenshtein edit distance (score: 0.6-0.8)
#[inline]
pub fn levenshtein_match(query: &str, item: &str, max_edit_distance: usize) -> Option<StrategyMatch> {
    let edit_dist = levenshtein(query, item);
    if edit_dist <= max_edit_distance {
        let max_len = query.len().max(item.len());
        if max_len > 0 {
            let score = 0.8 - (edit_dist as f64 / max_len as f64) * 0.4;
            if score >= 0.6 {
                return Some(StrategyMatch {
                    score,
                    strategy: "levenshtein",
                });
            }
        }
    }
    None
}

/// Apply all strategies in order, return first match
pub fn score_match(
    query_lower: &str,
    item_lower: &str,
    min_fuzzy_length: usize,
    max_edit_distance: usize,
    enable_phonetic: bool,
) -> Option<StrategyMatch> {
    // 1. Exact match
    if let Some(m) = exact_match(query_lower, item_lower) {
        return Some(m);
    }

    // 2. Prefix match
    if let Some(m) = prefix_match(query_lower, item_lower) {
        return Some(m);
    }

    // 3. Word boundary match
    if let Some(m) = word_boundary_match(query_lower, item_lower) {
        return Some(m);
    }

    // 4. Substring match
    if let Some(m) = substring_match(query_lower, item_lower) {
        return Some(m);
    }

    // Skip expensive fuzzy matching for short queries
    if query_lower.len() < min_fuzzy_length {
        return None;
    }

    // 5. Phonetic match (native only)
    if enable_phonetic {
        if let Some(m) = phonetic_match(query_lower, item_lower) {
            return Some(m);
        }
    }

    // 6. Jaro-Winkler similarity
    if let Some(m) = jaro_winkler_match(query_lower, item_lower) {
        return Some(m);
    }

    // 7. Levenshtein edit distance
    if let Some(m) = levenshtein_match(query_lower, item_lower, max_edit_distance) {
        return Some(m);
    }

    None
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_exact_match() {
        assert!(exact_match("hello", "hello").is_some());
        assert!(exact_match("hello", "Hello").is_none());
    }

    #[test]
    fn test_prefix_match() {
        assert!(prefix_match("hel", "hello").is_some());
        assert!(prefix_match("hello", "hel").is_none());
    }

    #[test]
    fn test_word_boundary_match() {
        assert!(word_boundary_match("author", "is_author_of").is_some());
        assert!(word_boundary_match("auth", "is_author_of").is_none());
    }

    #[test]
    fn test_substring_match() {
        assert!(substring_match("thor", "is_author_of").is_some());
    }

    #[test]
    fn test_jaro_winkler() {
        // Very similar strings should match
        let m = jaro_winkler_match("algorithm", "algoritm");
        assert!(m.is_some());
    }

    #[test]
    fn test_levenshtein() {
        // 1 edit distance
        let m = levenshtein_match("works_at", "wroks_at", 2);
        assert!(m.is_some());
    }
}
