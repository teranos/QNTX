//! Browser WASM bindings using wasm-bindgen and IndexedDB storage
//!
//! Provides browser-compatible functions for:
//! - Parsing AX queries (same as wazero target)
//! - Storing and retrieving attestations using IndexedDB
//!
//! Unlike the wazero target which uses raw memory passing, these functions
//! use wasm-bindgen for seamless JavaScript interop.
//!
//! ## Proto Boundary (ADR-006, ADR-007)
//!
//! This module implements proto conversion at the WASM↔TypeScript boundary:
//! - TypeScript uses proto-generated `Attestation` interface
//! - JSON matches proto schema (timestamps as numbers, attributes as object)
//! - Converted to qntx_core::Attestation for internal storage operations

use qntx_core::fuzzy::{FuzzyEngine, VocabularyType};
use qntx_core::parser::{Lexer, Parser, TokenKind};
use qntx_indexeddb::IndexedDbStore;
use qntx_proto::Attestation as ProtoAttestation;
use std::cell::RefCell;
use std::collections::{HashMap, HashSet};
use std::rc::Rc;
use wasm_bindgen::prelude::*;

/// Global store instance (initialized via init_store)
/// Using Rc<RefCell<>> because WASM is single-threaded and we need to share across async boundaries
thread_local! {
    static STORE: RefCell<Option<Rc<IndexedDbStore>>> = RefCell::new(None);
    static FUZZY: RefCell<FuzzyEngine> = RefCell::new(FuzzyEngine::new());
    static RICH_FUZZY: RefCell<FuzzyEngine> = RefCell::new(FuzzyEngine::new());
}

/// Default database name for browser IndexedDB storage
const DEFAULT_DB_NAME: &str = "qntx";

/// Initialize the IndexedDB store. Must be called before any storage operations.
/// Returns a Promise that resolves when initialization is complete.
#[wasm_bindgen]
pub async fn init_store(db_name: Option<String>) -> Result<(), JsValue> {
    // Route Rust panics to console.error instead of "RuntimeError: unreachable"
    console_error_panic_hook::set_once();

    let name = db_name.unwrap_or_else(|| DEFAULT_DB_NAME.to_string());

    let store = IndexedDbStore::open(&name)
        .await
        .map_err(|e| JsValue::from_str(&format!("Failed to open IndexedDB: {:?}", e)))?;

    STORE.with(|s| {
        let mut s = s.borrow_mut();
        if s.is_some() {
            return Err(JsValue::from_str("Store already initialized"));
        }
        *s = Some(Rc::new(store));
        Ok(())
    })
}

/// Get a clone of the store Rc. Panics if not initialized.
fn get_store() -> Rc<IndexedDbStore> {
    STORE.with(|s| {
        s.borrow()
            .as_ref()
            .expect("Store not initialized. Call init_store() first.")
            .clone()
    })
}

// ============================================================================
// Parser (same as wazero target, but with wasm-bindgen)
// ============================================================================

/// Parse an AX query string. Returns JSON-serialized AxQuery or error.
///
/// Returns: `{"subjects":["ALICE"],"predicates":["author"],...}` on success
///          `{"error":"description"}` on error
#[wasm_bindgen]
pub fn parse_query(input: &str) -> String {
    match Parser::parse(input) {
        Ok(query) => {
            // Same validation hack as wazero target for bug-for-bug compatibility
            if let Some(qntx_core::parser::TemporalClause::Over(ref dur)) = query.temporal {
                if dur.value.is_some() && dur.unit.is_none() {
                    return format!(r#"{{"error":"missing unit in '{}'"}}"#, dur.raw);
                }
            }

            match serde_json::to_string(&query) {
                Ok(json) => json,
                Err(e) => format!(r#"{{"error":"serialization failed: {}"}}"#, e),
            }
        }
        Err(e) => format!(r#"{{"error":"{}"}}"#, e),
    }
}

// ============================================================================
// Storage operations
// ============================================================================

/// Store an attestation in IndexedDB.
/// Returns a Promise that resolves to null on success or error message on failure.
///
/// Expects JSON matching proto schema (timestamps as numbers, attributes as JSON object).
/// Converts to internal core::Attestation format before storage.
#[wasm_bindgen]
pub async fn put_attestation(json: &str) -> Result<(), JsValue> {
    // Deserialize from proto-compliant JSON
    let proto_attestation: ProtoAttestation = serde_json::from_str(json)
        .map_err(|e| JsValue::from_str(&format!("Invalid JSON: {}", e)))?;

    // Convert to core type for storage
    let core_attestation = qntx_proto::proto_convert::from_proto(proto_attestation);

    let store = get_store();
    store
        .put(core_attestation)
        .await
        .map_err(|e| JsValue::from_str(&format!("Store error: {:?}", e)))?;

    Ok(())
}

/// Retrieve an attestation by ID from IndexedDB.
/// Returns a Promise that resolves to JSON-serialized attestation or null if not found.
///
/// Returns JSON matching proto schema (timestamps as numbers, attributes as JSON object).
/// Converts from internal core::Attestation format before serialization.
#[wasm_bindgen]
pub async fn get_attestation(id: &str) -> Result<Option<String>, JsValue> {
    let store = get_store();
    let result = store
        .get(id)
        .await
        .map_err(|e| JsValue::from_str(&format!("Store error: {:?}", e)))?;

    match result {
        Some(core_attestation) => {
            // Convert to proto type for JSON serialization
            let proto_attestation = qntx_proto::proto_convert::to_proto(core_attestation);
            let json = serde_json::to_string(&proto_attestation)
                .map_err(|e| JsValue::from_str(&format!("Serialization error: {}", e)))?;
            Ok(Some(json))
        }
        None => Ok(None),
    }
}

/// Delete an attestation by ID from IndexedDB.
/// Returns a Promise that resolves to true if deleted, false if not found.
#[wasm_bindgen]
pub async fn delete_attestation(id: &str) -> Result<bool, JsValue> {
    let store = get_store();
    store
        .delete(id)
        .await
        .map_err(|e| JsValue::from_str(&format!("Store error: {:?}", e)))
}

/// Check if an attestation exists in IndexedDB.
/// Returns a Promise that resolves to true if exists, false otherwise.
#[wasm_bindgen]
pub async fn exists_attestation(id: &str) -> Result<bool, JsValue> {
    let store = get_store();
    store
        .exists(id)
        .await
        .map_err(|e| JsValue::from_str(&format!("Store error: {:?}", e)))
}

/// Query attestations from IndexedDB using an AxFilter.
/// Expects JSON-serialized AxFilter. Returns JSON array of proto-format attestations.
#[wasm_bindgen]
pub async fn query_attestations(filter_json: &str) -> Result<String, JsValue> {
    use qntx_core::attestation::AxFilter;

    let filter: AxFilter = serde_json::from_str(filter_json)
        .map_err(|e| JsValue::from_str(&format!("Invalid filter JSON: {}", e)))?;

    let store = get_store();
    let result = store
        .query(&filter)
        .await
        .map_err(|e| JsValue::from_str(&format!("Query error: {:?}", e)))?;

    let proto_attestations: Vec<ProtoAttestation> = result
        .attestations
        .into_iter()
        .map(qntx_proto::proto_convert::to_proto)
        .collect();

    serde_json::to_string(&proto_attestations)
        .map_err(|e| JsValue::from_str(&format!("Serialization error: {}", e)))
}

/// Get all attestation IDs from IndexedDB.
/// Returns a Promise that resolves to JSON array of IDs.
#[wasm_bindgen]
pub async fn list_attestation_ids() -> Result<String, JsValue> {
    let store = get_store();
    let ids = store
        .ids()
        .await
        .map_err(|e| JsValue::from_str(&format!("Store error: {:?}", e)))?;

    serde_json::to_string(&ids)
        .map_err(|e| JsValue::from_str(&format!("Serialization error: {}", e)))
}

// ============================================================================
// Fuzzy Search
// ============================================================================

/// Rebuild the fuzzy search index from current IndexedDB vocabulary.
/// Pulls distinct subjects, predicates, contexts, and actors from the attestation store.
/// Returns JSON: {"subjects": N, "predicates": N, "contexts": N, "actors": N, "hash": "..."}
#[wasm_bindgen]
pub async fn fuzzy_rebuild_index() -> Result<String, JsValue> {
    let store = get_store();

    let subjects = store.subjects().await.map_err(|e| {
        JsValue::from_str(&format!("Failed to load subjects from IndexedDB: {:?}", e))
    })?;

    let predicates = store.predicates().await.map_err(|e| {
        JsValue::from_str(&format!(
            "Failed to load predicates from IndexedDB: {:?}",
            e
        ))
    })?;

    let contexts = store.contexts().await.map_err(|e| {
        JsValue::from_str(&format!("Failed to load contexts from IndexedDB: {:?}", e))
    })?;

    let actors = store.actors().await.map_err(|e| {
        JsValue::from_str(&format!("Failed to load actors from IndexedDB: {:?}", e))
    })?;

    let (sub_count, pred_count, ctx_count, act_count, hash) = FUZZY.with(|f| {
        f.borrow_mut()
            .rebuild_index(subjects, predicates, contexts, actors)
    });

    Ok(format!(
        r#"{{"subjects":{},"predicates":{},"contexts":{},"actors":{},"hash":"{}"}}"#,
        sub_count, pred_count, ctx_count, act_count, hash
    ))
}

/// Search the fuzzy index for matching vocabulary.
/// vocab_type: "subjects", "predicates", "contexts", or "actors"
/// Returns JSON array: [{"value":"...", "score":0.95, "strategy":"exact"}, ...]
#[wasm_bindgen]
pub fn fuzzy_search(
    query: &str,
    vocab_type: &str,
    limit: usize,
    min_score: f64,
) -> Result<String, JsValue> {
    let vtype = match vocab_type {
        "subjects" => VocabularyType::Subjects,
        "predicates" => VocabularyType::Predicates,
        "contexts" => VocabularyType::Contexts,
        "actors" => VocabularyType::Actors,
        _ => {
            return Err(JsValue::from_str(&format!(
            "Invalid vocab_type '{}', expected 'subjects', 'predicates', 'contexts', or 'actors'",
            vocab_type
        )))
        }
    };

    let matches = FUZZY.with(|f| f.borrow().find_matches(query, vtype, limit, min_score));

    serde_json::to_string(&matches)
        .map_err(|e| JsValue::from_str(&format!("Failed to serialize fuzzy matches: {}", e)))
}

/// Get fuzzy engine status.
/// Returns JSON: {"ready": bool, "subjects": N, "predicates": N, "contexts": N, "actors": N, "hash": "..."}
#[wasm_bindgen]
pub fn fuzzy_status() -> String {
    FUZZY.with(|f| {
        let engine = f.borrow();
        let (sub_count, pred_count, ctx_count, act_count) = engine.get_counts();
        format!(
            r#"{{"ready":{},"subjects":{},"predicates":{},"contexts":{},"actors":{},"hash":"{}"}}"#,
            engine.is_ready(),
            sub_count,
            pred_count,
            ctx_count,
            act_count,
            engine.get_index_hash()
        )
    })
}

// ============================================================================
// Completions (parser-aware fuzzy matching)
// ============================================================================

/// Get context-aware completions for a partial AX query.
///
/// Parses the partial query to determine which AX slot the cursor is in,
/// then fuzzy-matches the trailing word against the appropriate vocabulary.
///
/// Returns JSON: `{"slot":"predicates","prefix":"auth","items":[{"value":"...","score":0.95,"strategy":"exact"},...]}`
#[wasm_bindgen]
pub fn get_completions(partial_query: &str, limit: usize) -> String {
    let trimmed = partial_query.trim();
    if trimmed.is_empty() {
        return r#"{"slot":"subjects","prefix":"","items":[]}"#.to_string();
    }

    // Determine which slot we're completing and what the prefix is.
    // Strategy: scan tokens to find the last keyword, which tells us the slot.
    // The trailing partial word (if not a keyword) is the prefix to fuzzy-match.
    let (slot, prefix) = infer_completion_slot(trimmed);

    let vocab_type = match slot {
        "subjects" => VocabularyType::Subjects,
        "predicates" => VocabularyType::Predicates,
        "contexts" => VocabularyType::Contexts,
        "actors" => VocabularyType::Actors,
        _ => VocabularyType::Subjects,
    };

    let items = if prefix.is_empty() {
        Vec::new()
    } else {
        FUZZY.with(|f| f.borrow().find_matches(&prefix, vocab_type, limit, 0.3))
    };

    match serde_json::to_string(&items) {
        Ok(items_json) => format!(
            r#"{{"slot":"{}","prefix":"{}","items":{}}}"#,
            slot,
            prefix.replace('"', "\\\""),
            items_json
        ),
        Err(e) => format!(r#"{{"error":"serialization failed: {}"}}"#, e),
    }
}

/// Determine the AX slot and prefix from a partial query string.
///
/// Walks tokens left-to-right. The last keyword (`is`/`are` → predicates,
/// `of`/`from` → contexts, `by`/`via` → actors) determines the slot.
/// No keyword means subjects. The final word (if not a keyword) is the prefix.
fn infer_completion_slot(input: &str) -> (&'static str, String) {
    let mut slot = "subjects";
    let mut last_word = String::new();
    let mut ends_with_keyword = false;

    for token in Lexer::new(input) {
        ends_with_keyword = false;

        match token.kind {
            TokenKind::Is | TokenKind::Are => {
                slot = "predicates";
                last_word.clear();
                ends_with_keyword = true;
            }
            TokenKind::Of | TokenKind::From => {
                slot = "contexts";
                last_word.clear();
                ends_with_keyword = true;
            }
            TokenKind::By | TokenKind::Via => {
                slot = "actors";
                last_word.clear();
                ends_with_keyword = true;
            }
            TokenKind::Identifier | TokenKind::QuotedString => {
                last_word = token.text.to_string();
            }
            TokenKind::Eof => break,
            _ => {}
        }
    }

    // If query ends with trailing whitespace after a keyword, the user hasn't
    // started typing the next word yet — return empty prefix so the UI can
    // show a placeholder like "type a predicate...".
    if ends_with_keyword && input.ends_with(' ') {
        return (slot, String::new());
    }

    (slot, last_word)
}

// ============================================================================
// Rich Search (browser-side, mirrors Go ats/storage/rich_search_qntx.go)
// ============================================================================

/// A single rich search match, serialized to match the proto RichSearchMatch schema.
#[derive(serde::Serialize)]
struct RichSearchMatch {
    node_id: String,
    type_name: String,
    type_label: String,
    field_name: String,
    field_value: String,
    excerpt: String,
    score: f64,
    strategy: String,
    display_label: String,
    attributes: HashMap<String, serde_json::Value>,
    matched_words: Vec<String>,
}

/// Results envelope matching RichSearchResultsMessage proto.
#[derive(serde::Serialize)]
struct RichSearchResponse {
    query: String,
    matches: Vec<RichSearchMatch>,
    total: usize,
}

const MAX_RICH_ATTESTATIONS: usize = 500;
const MAX_VOCABULARY_SIZE: usize = 100_000;
const MAX_WORD_GAP: usize = 50;
const SEQUENTIAL_MATCH_BOOST: f64 = 1.5;

/// Perform rich text search over IndexedDB attestations.
///
/// Algorithm (mirrors Go rich_search_qntx.go):
/// 1. Discover rich_string_fields from type definition attestations
/// 2. Load recent attestations that have those fields
/// 3. Tokenize field values into a word vocabulary
/// 4. Rebuild RICH_FUZZY engine with that vocabulary
/// 5. Fuzzy-match each query word against vocabulary
/// 6. Map matched words back to attestation nodes, score, rank
///
/// Returns JSON: `{"query":"...","matches":[...],"total":N}`
#[wasm_bindgen]
pub async fn rich_search(query: &str, limit: usize) -> Result<String, JsValue> {
    let query = query.trim();
    if query.is_empty() {
        return empty_response("");
    }

    let store = get_store();
    let mut all = store
        .get_all()
        .await
        .map_err(|e| JsValue::from_str(&format!("Failed to load attestations: {:?}", e)))?;

    // Step 1: discover rich_string_fields from type definitions
    let rich_fields = discover_rich_string_fields(&all);
    if rich_fields.is_empty() {
        return empty_response(query);
    }

    // Step 2: build vocabulary from attestations with rich fields
    // Sort by timestamp desc, take most recent MAX_RICH_ATTESTATIONS
    all.sort_by(|a, b| b.timestamp.cmp(&a.timestamp));

    let mut vocabulary: HashSet<String> = HashSet::new();
    let mut node_word_map: HashMap<String, HashMap<String, Vec<String>>> = HashMap::new();
    let mut node_attributes: HashMap<String, HashMap<String, serde_json::Value>> = HashMap::new();
    let mut count = 0;

    for attestation in &all {
        if attestation.attributes.is_empty() {
            continue;
        }
        let has_rich = rich_fields
            .iter()
            .any(|f| matches!(attestation.attributes.get(f), Some(v) if !v.is_null()));
        if !has_rich {
            continue;
        }
        count += 1;
        if count > MAX_RICH_ATTESTATIONS {
            break;
        }

        for node_id in &attestation.subjects {
            node_attributes
                .entry(node_id.clone())
                .or_insert_with(|| attestation.attributes.clone());
            let field_words = node_word_map.entry(node_id.clone()).or_default();

            for field_name in &rich_fields {
                if let Some(serde_json::Value::String(s)) = attestation.attributes.get(field_name) {
                    if s.is_empty() {
                        continue;
                    }
                    let words: Vec<String> = s
                        .split_whitespace()
                        .map(|w| {
                            w.trim_matches(|c: char| {
                                ".,!?;:\"'()[]{}/*&^%$#@".contains(c)
                            })
                            .to_lowercase()
                        })
                        .filter(|w| w.len() > 1)
                        .collect();
                    for word in &words {
                        vocabulary.insert(word.clone());
                    }
                    field_words
                        .entry(field_name.clone())
                        .or_default()
                        .extend(words);
                }
            }
        }
    }

    if vocabulary.is_empty() {
        return empty_response(query);
    }

    // Step 3: rebuild RICH_FUZZY with vocabulary (predicates slot, same as Go)
    let mut vocab_vec: Vec<String> = vocabulary.into_iter().collect();
    if vocab_vec.len() > MAX_VOCABULARY_SIZE {
        vocab_vec.truncate(MAX_VOCABULARY_SIZE);
    }

    RICH_FUZZY.with(|f| {
        f.borrow_mut()
            .rebuild_index(vec![], vocab_vec, vec![], vec![])
    });

    // Step 4: fuzzy-match each query word
    let query_lower = query.to_lowercase();
    let query_words: Vec<&str> = query_lower.split_whitespace().collect();

    let mut query_word_matches: HashMap<String, Vec<(String, f64)>> = HashMap::new();
    for qw in &query_words {
        let fuzzy = RICH_FUZZY
            .with(|f| f.borrow().find_matches(qw, VocabularyType::Predicates, 10, 0.3));
        if !fuzzy.is_empty() {
            for m in fuzzy {
                query_word_matches
                    .entry(qw.to_string())
                    .or_default()
                    .push((m.value, m.score));
            }
        } else {
            query_word_matches
                .entry(qw.to_string())
                .or_default()
                .push((qw.to_string(), 0.7));
        }
    }

    if query_word_matches.is_empty() {
        return empty_response(query);
    }

    // Step 5: score nodes
    let mut matches: Vec<RichSearchMatch> = Vec::new();
    let mut processed: HashSet<String> = HashSet::new();

    for (node_id, field_words) in &node_word_map {
        if processed.contains(node_id) {
            continue;
        }
        let attributes = match node_attributes.get(node_id) {
            Some(a) => a,
            None => continue,
        };

        let mut words_found: HashMap<String, f64> = HashMap::new();
        let mut matched_field_name = String::new();
        let mut matched_field_value = String::new();

        // Fuzzy word matching
        for (field_name, words) in field_words {
            for word in words {
                for (qw, possible) in &query_word_matches {
                    for (matched_word, score) in possible {
                        if word == matched_word {
                            let current = words_found.get(qw).copied().unwrap_or(0.0);
                            if *score > current {
                                words_found.insert(qw.clone(), *score);
                            }
                            if matched_field_name.is_empty() {
                                matched_field_name = field_name.clone();
                                if let Some(serde_json::Value::String(s)) =
                                    attributes.get(field_name)
                                {
                                    matched_field_value = s.clone();
                                }
                            }
                        }
                    }
                }
            }
        }

        // Substring fallback
        for field_name in &rich_fields {
            if let Some(serde_json::Value::String(s)) = attributes.get(field_name) {
                if s.is_empty() {
                    continue;
                }
                let lower = s.to_lowercase();
                let mut found_in_field = false;
                for qw in query_word_matches.keys() {
                    if !words_found.contains_key(qw) && lower.contains(qw.as_str()) {
                        words_found.insert(qw.clone(), 0.6);
                        found_in_field = true;
                    }
                }
                if found_in_field && matched_field_name.is_empty() {
                    matched_field_name = field_name.clone();
                    matched_field_value = s.clone();
                }
            }
        }

        if words_found.is_empty() {
            continue;
        }

        // Score
        let total_score: f64 = words_found.values().sum();
        let match_ratio = words_found.len() as f64 / query_word_matches.len() as f64;
        let mut final_score = (total_score / words_found.len() as f64) * match_ratio;

        // Sequential proximity boost
        if !matched_field_value.is_empty() && query_words.len() > 1 {
            let lower = matched_field_value.to_lowercase();
            let mut positions: Vec<usize> = words_found
                .keys()
                .filter_map(|qw| lower.find(qw.as_str()))
                .collect();
            if positions.len() > 1 {
                positions.sort();
                let sequential = positions.windows(2).all(|w| w[1] - w[0] <= MAX_WORD_GAP);
                if sequential {
                    final_score = (final_score * SEQUENTIAL_MATCH_BOOST).min(1.0);
                }
            }
        }

        let display_label = attributes
            .get("label")
            .and_then(|v| v.as_str())
            .or_else(|| attributes.get("name").and_then(|v| v.as_str()))
            .unwrap_or(node_id)
            .to_string();

        let type_name = attributes
            .get("type")
            .and_then(|v| v.as_str())
            .unwrap_or("Document")
            .to_string();

        let strategy = if words_found.len() == query_word_matches.len() {
            "fuzzy:all-words"
        } else {
            "fuzzy:partial"
        };

        let matched_words: Vec<String> = words_found.keys().cloned().collect();

        matches.push(RichSearchMatch {
            node_id: node_id.clone(),
            type_name: type_name.clone(),
            type_label: type_name,
            field_name: matched_field_name,
            field_value: matched_field_value.clone(),
            excerpt: matched_field_value,
            score: final_score,
            strategy: strategy.to_string(),
            display_label,
            attributes: attributes.clone(),
            matched_words,
        });
        processed.insert(node_id.clone());

        if matches.len() >= limit {
            break;
        }
    }

    matches.sort_by(|a, b| b.score.partial_cmp(&a.score).unwrap_or(std::cmp::Ordering::Equal));
    matches.truncate(limit);

    let total = matches.len();
    let resp = RichSearchResponse {
        query: query.to_string(),
        matches,
        total,
    };
    serde_json::to_string(&resp)
        .map_err(|e| JsValue::from_str(&format!("Serialization error: {}", e)))
}

/// Discover rich_string_fields from type definition attestations.
/// Mirrors Go's buildDynamicRichStringFields.
fn discover_rich_string_fields(
    attestations: &[qntx_core::attestation::Attestation],
) -> Vec<String> {
    let mut fields: HashSet<String> = HashSet::new();
    for a in attestations {
        let is_type_def = a.predicates.iter().any(|p| p == "type")
            && a.contexts.iter().any(|c| c == "graph");
        if !is_type_def {
            continue;
        }
        if let Some(serde_json::Value::Array(arr)) = a.attributes.get("rich_string_fields") {
            for v in arr {
                if let serde_json::Value::String(s) = v {
                    fields.insert(s.clone());
                }
            }
        }
    }
    let mut result: Vec<String> = fields.into_iter().collect();
    result.sort();
    result
}

fn empty_response(query: &str) -> Result<String, JsValue> {
    let resp = RichSearchResponse {
        query: query.to_string(),
        matches: vec![],
        total: 0,
    };
    serde_json::to_string(&resp)
        .map_err(|e| JsValue::from_str(&format!("Serialization error: {}", e)))
}

// ============================================================================
// Classification
// ============================================================================

/// Classify claim conflicts. Takes JSON input with claim groups, temporal config,
/// and current time. Returns JSON with classified conflicts, resolution strategies,
/// and actor rankings.
///
/// Input:
/// ```json
/// {
///   "claim_groups": [{"key": "...", "claims": [...]}],
///   "config": {"verification_window_ms": 60000, ...},
///   "now_ms": 1234567890
/// }
/// ```
///
/// Returns JSON with conflicts, auto_resolved count, review_required count.
#[wasm_bindgen]
pub fn classify_claims(input: &str) -> String {
    qntx_core::classify_claims(input)
}

// ============================================================================
// Sync: content-addressed attestation identity + Merkle tree
// ============================================================================

/// Compute content hash for an attestation.
/// Input: JSON-serialized proto Attestation (attributes as JSON object)
/// Returns: `{"hash":"<64-char hex>"}` or `{"error":"..."}`
#[wasm_bindgen]
pub fn sync_content_hash(attestation_json: &str) -> String {
    match serde_json::from_str::<ProtoAttestation>(attestation_json) {
        Ok(proto) => {
            let core = qntx_proto::proto_convert::from_proto(proto);
            let hash = qntx_core::sync::content_hash_hex(&core);
            format!(r#"{{"hash":"{}"}}"#, hash)
        }
        Err(e) => format!(
            r#"{{"error":"invalid attestation JSON: {}"}}"#,
            e.to_string().replace('"', "\\\"")
        ),
    }
}

/// Insert into the global Merkle tree.
/// Input: `{"actor":"...","context":"...","content_hash":"<hex>"}`
/// Returns: `{"ok":true}` or `{"error":"..."}`
#[wasm_bindgen]
pub fn sync_merkle_insert(input: &str) -> String {
    qntx_core::sync::merkle_insert_json(input)
}

/// Remove from the global Merkle tree.
/// Input: `{"actor":"...","context":"...","content_hash":"<hex>"}`
/// Returns: `{"ok":true}`
#[wasm_bindgen]
pub fn sync_merkle_remove(input: &str) -> String {
    qntx_core::sync::merkle_remove_json(input)
}

/// Check if a content hash exists in the global Merkle tree.
/// Input: `{"content_hash":"<hex>"}`
/// Returns: `{"exists":true|false}`
#[wasm_bindgen]
pub fn sync_merkle_contains(input: &str) -> String {
    qntx_core::sync::merkle_contains_json(input)
}

/// Get the Merkle tree root hash and stats.
/// Returns: `{"root":"<hex>","size":N,"groups":N}`
#[wasm_bindgen]
pub fn sync_merkle_root() -> String {
    qntx_core::sync::merkle_root_json("")
}

/// Get all group hashes from the Merkle tree.
/// Returns: `{"groups":{"<hex>":"<hex>",...}}`
#[wasm_bindgen]
pub fn sync_merkle_group_hashes() -> String {
    qntx_core::sync::merkle_group_hashes_json("")
}

/// Diff Merkle tree against remote group hashes.
/// Input: `{"remote":{"<hex>":"<hex>",...}}`
/// Returns: `{"local_only":[...],"remote_only":[...],"divergent":[...]}`
#[wasm_bindgen]
pub fn sync_merkle_diff(remote_json: &str) -> String {
    qntx_core::sync::merkle_diff_json(remote_json)
}

/// Reverse-lookup a group key hash to its (actor, context) pair.
/// Input: `{"group_key_hash":"<hex>"}`
/// Returns: `{"actor":"...","context":"..."}` or `{"error":"group not found"}`
#[wasm_bindgen]
pub fn sync_merkle_find_group_key(input: &str) -> String {
    qntx_core::sync::merkle_find_group_key_json(input)
}

// ============================================================================
// Cosine Similarity
// ============================================================================

/// Compute cosine similarity between two f32 vectors.
/// Uses typed arrays directly from JavaScript (no JSON overhead).
/// Throws JS exception if vectors have different dimensions.
#[wasm_bindgen]
pub fn cosine_similarity_f32(query: &[f32], candidate: &[f32]) -> Result<f32, JsValue> {
    qntx_core::similarity::cosine_similarity(query, candidate).map_err(|e| JsValue::from_str(&e))
}

// ============================================================================
// Utilities
// ============================================================================

/// Get the qntx-core version.
#[wasm_bindgen]
pub fn version() -> String {
    env!("CARGO_PKG_VERSION").to_string()
}

/// Check if the store is initialized.
#[wasm_bindgen]
pub fn is_store_initialized() -> bool {
    STORE.with(|s| s.borrow().is_some())
}
