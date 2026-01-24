//! QNTX WebAssembly Bindings
//!
//! Exposes qntx-core functionality to JavaScript/TypeScript via wasm-bindgen.
//!
//! # Usage (TypeScript)
//!
//! ```typescript
//! import init, { QntxEngine } from 'qntx-wasm';
//!
//! await init();
//! const engine = new QntxEngine();
//!
//! // Parse a query
//! const query = engine.parseQuery("ALICE is author_of of GitHub");
//! console.log(query.subjects); // ["ALICE"]
//!
//! // Fuzzy search
//! engine.rebuildIndex(["is_author_of", "is_maintainer_of"], ["GitHub", "GitLab"]);
//! const matches = engine.searchPredicates("author", 10, 0.6);
//! ```

use qntx_core::{
    classify::ActorCredibility,
    fuzzy::FuzzyEngine,
    parser::{AxQuery, Parser, TemporalClause},
};
use serde::{Deserialize, Serialize};
use wasm_bindgen::prelude::*;

// Initialize panic hook for better error messages
#[wasm_bindgen(start)]
pub fn init() {
    #[cfg(feature = "console_error_panic_hook")]
    console_error_panic_hook::set_once();
}

// ============================================================================
// Parser Bindings
// ============================================================================

/// Parse an AX query string and return the result as JSON
#[wasm_bindgen(js_name = parseQuery)]
pub fn parse_query(input: &str) -> Result<JsValue, JsError> {
    let query = Parser::parse(input).map_err(|e| JsError::new(&e.to_string()))?;

    // Convert to owned version for serialization
    let owned = OwnedAxQuery::from_borrowed(&query);
    serde_wasm_bindgen::to_value(&owned).map_err(|e| JsError::new(&e.to_string()))
}

/// Validate query syntax without returning full AST
#[wasm_bindgen(js_name = validateQuery)]
pub fn validate_query(input: &str) -> bool {
    Parser::parse(input).is_ok()
}

/// Get parse error message if query is invalid
#[wasm_bindgen(js_name = getParseError)]
pub fn get_parse_error(input: &str) -> Option<String> {
    match Parser::parse(input) {
        Ok(_) => None,
        Err(e) => Some(e.to_string()),
    }
}

// Owned version of AxQuery for JSON serialization (no lifetimes)
#[derive(Debug, Clone, Serialize, Deserialize)]
struct OwnedAxQuery {
    subjects: Vec<String>,
    predicates: Vec<String>,
    contexts: Vec<String>,
    actors: Vec<String>,
    temporal: Option<OwnedTemporalClause>,
    actions: Vec<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", content = "value")]
enum OwnedTemporalClause {
    Since(String),
    Until(String),
    On(String),
    Between { start: String, end: String },
    Over { raw: String, value: Option<f64>, unit: Option<String> },
}

impl OwnedAxQuery {
    fn from_borrowed(query: &AxQuery<'_>) -> Self {
        Self {
            subjects: query.subjects.iter().map(|s| s.to_string()).collect(),
            predicates: query.predicates.iter().map(|s| s.to_string()).collect(),
            contexts: query.contexts.iter().map(|s| s.to_string()).collect(),
            actors: query.actors.iter().map(|s| s.to_string()).collect(),
            temporal: query.temporal.as_ref().map(OwnedTemporalClause::from_borrowed),
            actions: query.actions.iter().map(|s| s.to_string()).collect(),
        }
    }
}

impl OwnedTemporalClause {
    fn from_borrowed(clause: &TemporalClause<'_>) -> Self {
        match clause {
            TemporalClause::Since(s) => Self::Since(s.to_string()),
            TemporalClause::Until(s) => Self::Until(s.to_string()),
            TemporalClause::On(s) => Self::On(s.to_string()),
            TemporalClause::Between(start, end) => Self::Between {
                start: start.to_string(),
                end: end.to_string(),
            },
            TemporalClause::Over(dur) => Self::Over {
                raw: dur.raw.to_string(),
                value: dur.value,
                unit: dur.unit.map(|u| u.to_string()),
            },
        }
    }
}

// ============================================================================
// Fuzzy Engine Bindings
// ============================================================================

/// QNTX Engine - main entry point for WASM
#[wasm_bindgen]
pub struct QntxEngine {
    fuzzy: FuzzyEngine,
}

#[wasm_bindgen]
impl QntxEngine {
    /// Create a new QNTX engine
    #[wasm_bindgen(constructor)]
    pub fn new() -> Self {
        Self {
            fuzzy: FuzzyEngine::new(),
        }
    }

    /// Rebuild the fuzzy matching index
    #[wasm_bindgen(js_name = rebuildIndex)]
    pub fn rebuild_index(&mut self, predicates: Vec<String>, contexts: Vec<String>) -> JsValue {
        let (pred_count, ctx_count, hash) = self.fuzzy.rebuild_index(predicates, contexts);

        let result = serde_json::json!({
            "predicateCount": pred_count,
            "contextCount": ctx_count,
            "hash": hash
        });

        serde_wasm_bindgen::to_value(&result).unwrap_or(JsValue::NULL)
    }

    /// Search predicates vocabulary
    #[wasm_bindgen(js_name = searchPredicates)]
    pub fn search_predicates(&self, query: &str, limit: usize, min_score: f64) -> JsValue {
        let matches = self.fuzzy.search_predicates(query, limit, min_score);
        serde_wasm_bindgen::to_value(&matches).unwrap_or(JsValue::NULL)
    }

    /// Search contexts vocabulary
    #[wasm_bindgen(js_name = searchContexts)]
    pub fn search_contexts(&self, query: &str, limit: usize, min_score: f64) -> JsValue {
        let matches = self.fuzzy.search_contexts(query, limit, min_score);
        serde_wasm_bindgen::to_value(&matches).unwrap_or(JsValue::NULL)
    }

    /// Check if index is ready
    #[wasm_bindgen(js_name = isReady)]
    pub fn is_ready(&self) -> bool {
        self.fuzzy.is_ready()
    }

    /// Get vocabulary counts
    #[wasm_bindgen(js_name = getCounts)]
    pub fn get_counts(&self) -> JsValue {
        let (predicates, contexts) = self.fuzzy.get_counts();
        let result = serde_json::json!({
            "predicates": predicates,
            "contexts": contexts
        });
        serde_wasm_bindgen::to_value(&result).unwrap_or(JsValue::NULL)
    }

    /// Get index hash for change detection
    #[wasm_bindgen(js_name = getIndexHash)]
    pub fn get_index_hash(&self) -> String {
        self.fuzzy.get_index_hash().to_string()
    }

    /// Parse a query (convenience method)
    #[wasm_bindgen(js_name = parseQuery)]
    pub fn parse_query(&self, input: &str) -> Result<JsValue, JsError> {
        parse_query(input)
    }

    /// Validate query syntax
    #[wasm_bindgen(js_name = validateQuery)]
    pub fn validate_query(&self, input: &str) -> bool {
        validate_query(input)
    }
}

impl Default for QntxEngine {
    fn default() -> Self {
        Self::new()
    }
}

// ============================================================================
// Classification Bindings
// ============================================================================

/// Get actor credibility from actor identifier
#[wasm_bindgen(js_name = getActorCredibility)]
pub fn get_actor_credibility(actor: &str) -> JsValue {
    let cred = ActorCredibility::from_actor(actor);
    let result = serde_json::json!({
        "level": cred.to_string(),
        "score": cred.score(),
        "isHuman": cred.is_human()
    });
    serde_wasm_bindgen::to_value(&result).unwrap_or(JsValue::NULL)
}

/// Compare two actors' credibility
#[wasm_bindgen(js_name = compareCredibility)]
pub fn compare_credibility(actor1: &str, actor2: &str) -> JsValue {
    let cred1 = ActorCredibility::from_actor(actor1);
    let cred2 = ActorCredibility::from_actor(actor2);

    let result = serde_json::json!({
        "actor1": {
            "level": cred1.to_string(),
            "score": cred1.score()
        },
        "actor2": {
            "level": cred2.to_string(),
            "score": cred2.score()
        },
        "actor1Overrides": cred1.overrides(&cred2),
        "actor2Overrides": cred2.overrides(&cred1)
    });
    serde_wasm_bindgen::to_value(&result).unwrap_or(JsValue::NULL)
}

// ============================================================================
// Utilities
// ============================================================================

/// Get library version
#[wasm_bindgen(js_name = getVersion)]
pub fn get_version() -> String {
    env!("CARGO_PKG_VERSION").to_string()
}

/// Log to browser console (for debugging)
#[wasm_bindgen(js_name = consoleLog)]
pub fn console_log(msg: &str) {
    web_sys::console::log_1(&JsValue::from_str(msg));
}

// Native tests (test underlying logic without JsValue)
#[cfg(test)]
mod tests {
    use qntx_core::parser::Parser;
    use qntx_core::fuzzy::FuzzyEngine;
    use qntx_core::classify::ActorCredibility;

    #[test]
    fn test_parser_integration() {
        let query = Parser::parse("ALICE is author_of of GitHub").unwrap();
        assert_eq!(query.subjects, vec!["ALICE"]);
        assert_eq!(query.predicates, vec!["author_of"]);
        assert_eq!(query.contexts, vec!["GitHub"]);
    }

    #[test]
    fn test_validate_query() {
        assert!(Parser::parse("ALICE is author").is_ok());
        assert!(Parser::parse("").is_ok()); // Empty is valid
        assert!(Parser::parse("ALICE is").is_err()); // Missing predicate
    }

    #[test]
    fn test_fuzzy_engine() {
        let mut engine = FuzzyEngine::new();
        engine.rebuild_index(
            vec!["is_author_of".to_string()],
            vec!["GitHub".to_string()],
        );
        assert!(engine.is_ready());

        let matches = engine.search_predicates("author", 10, 0.6);
        assert!(!matches.is_empty());
    }

    #[test]
    fn test_credibility() {
        let human = ActorCredibility::from_actor("human:alice");
        let system = ActorCredibility::from_actor("system:hr");
        assert!(human.overrides(&system));
    }
}

// WASM-specific tests (run with wasm-pack test)
#[cfg(target_arch = "wasm32")]
mod wasm_tests {
    use super::*;
    use wasm_bindgen_test::*;

    wasm_bindgen_test_configure!(run_in_browser);

    #[wasm_bindgen_test]
    fn test_parse_query_wasm() {
        let result = parse_query("ALICE is author_of of GitHub");
        assert!(result.is_ok());
    }

    #[wasm_bindgen_test]
    fn test_engine_wasm() {
        let mut engine = QntxEngine::new();
        engine.rebuild_index(
            vec!["is_author_of".to_string()],
            vec!["GitHub".to_string()],
        );
        assert!(engine.is_ready());
    }
}
