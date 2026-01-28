//! QNTX Fuzzy Matching Library (CGO)
//!
//! This crate provides CGO bindings to the qntx-core fuzzy matching engine.
//! It wraps qntx_core::FuzzyEngine with thread-safety (RwLock) for concurrent
//! access from Go goroutines, and adds CGO-specific features like JSON attribute
//! searching.
//!
//! ## Usage from Go via CGO
//!
//! ```go
//! engine := cgo.NewFuzzyEngine()
//! defer engine.Free()
//!
//! engine.RebuildIndex(predicates, contexts)
//! result := engine.FindMatches("author", 0, 10, 0.6)
//! ```

#[macro_use]
extern crate log;

// FFI module for C/CGO integration
pub mod ffi;

// Re-export core types from qntx-core
pub use qntx_core::fuzzy::{EngineConfig, FuzzyEngine, FuzzyMatch, VocabularyType};

/// Initialize the logger for the fuzzy matching library.
pub fn init_logger() {
    use std::sync::Once;
    static INIT: Once = Once::new();

    INIT.call_once(|| {
        env_logger::init();
        info!("QNTX Fuzzy matching library initialized (powered by qntx-core)");
    });
}

/// A match result for RichStringFields search (CGO-specific)
#[derive(Debug, Clone)]
pub struct AttributeMatch {
    pub node_id: String,
    pub field_name: String,
    pub field_value: String,
    pub excerpt: String,
    pub score: f64,
    pub strategy: &'static str,
}

impl AttributeMatch {
    pub fn new(
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

/// Thread-safe wrapper around qntx_core::FuzzyEngine for CGO use.
/// Multiple Go goroutines can safely call methods on this engine concurrently.
pub struct ThreadSafeFuzzyEngine {
    inner: parking_lot::RwLock<FuzzyEngine>,
}

impl ThreadSafeFuzzyEngine {
    pub fn new() -> Self {
        Self {
            inner: parking_lot::RwLock::new(FuzzyEngine::new()),
        }
    }

    /// Rebuild index - returns (pred_count, ctx_count, build_time_ms, hash)
    pub fn rebuild_index(
        &self,
        predicates: Vec<String>,
        contexts: Vec<String>,
    ) -> (usize, usize, u64, String) {
        let start = std::time::Instant::now();
        let mut engine = self.inner.write();
        let (pred_count, ctx_count, hash) = engine.rebuild_index(predicates, contexts);
        let build_time = start.elapsed().as_millis() as u64;
        (pred_count, ctx_count, build_time, hash)
    }

    /// Find matches - returns (matches, search_time_us)
    pub fn find_matches(
        &self,
        query: &str,
        vocab_type: VocabularyType,
        limit: Option<usize>,
        min_score: Option<f64>,
    ) -> (Vec<FuzzyMatch>, u64) {
        let start = std::time::Instant::now();
        let engine = self.inner.read();
        let limit = limit.unwrap_or(20);
        let min_score = min_score.unwrap_or(0.6);
        let matches = engine.find_matches(query, vocab_type, limit, min_score);
        let search_time = start.elapsed().as_micros() as u64;
        (matches, search_time)
    }

    pub fn get_index_hash(&self) -> String {
        self.inner.read().get_index_hash().to_string()
    }

    pub fn is_ready(&self) -> bool {
        self.inner.read().is_ready()
    }

    /// Search for matches in JSON attributes (CGO-specific feature)
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

        let attributes: serde_json::Value = match serde_json::from_str(attributes_json) {
            Ok(val) => val,
            Err(_) => return Vec::new(),
        };

        let mut matches = Vec::new();
        let query_lower = query.to_lowercase();

        for field_name in rich_string_fields {
            if let Some(value) = attributes.get(field_name) {
                let str_value = match value {
                    serde_json::Value::String(s) => s.clone(),
                    serde_json::Value::Array(arr) => arr
                        .iter()
                        .filter_map(|v| v.as_str())
                        .collect::<Vec<_>>()
                        .join(" "),
                    _ => continue,
                };

                if str_value.is_empty() {
                    continue;
                }

                // Simple scoring for attribute values
                let item_lower = str_value.to_lowercase();
                let (score, strategy) = if query_lower == item_lower {
                    (1.0, "exact")
                } else if item_lower.starts_with(&query_lower) {
                    (0.9, "prefix")
                } else if item_lower.contains(&query_lower) {
                    (0.75, "substring")
                } else {
                    continue;
                };

                let excerpt = extract_excerpt(&str_value, query, 150);
                matches.push(AttributeMatch::new(
                    node_id.to_string(),
                    field_name.clone(),
                    str_value,
                    excerpt,
                    score,
                    strategy,
                ));
            }
        }

        matches.sort_by(|a, b| b.score.partial_cmp(&a.score).unwrap());
        matches.truncate(20);
        matches
    }
}

impl Default for ThreadSafeFuzzyEngine {
    fn default() -> Self {
        Self::new()
    }
}

/// Extract an excerpt from text around the match
fn extract_excerpt(text: &str, query: &str, max_length: usize) -> String {
    let text_lower = text.to_lowercase();
    let query_lower = query.to_lowercase();

    let idx = text_lower.find(&query_lower).unwrap_or(0);

    let start = if idx > max_length / 2 {
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

// Re-export FFI types
pub use ffi::{RustMatchC, RustMatchResultC, RustRebuildResultC};
