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
//! - JSON matches proto schema (timestamps as numbers, attributes as string)
//! - Converted to qntx_core::Attestation for internal storage operations

use qntx_core::fuzzy::{FuzzyEngine, VocabularyType};
use qntx_core::parser::Parser;
use qntx_indexeddb::IndexedDbStore;
use qntx_proto::Attestation as ProtoAttestation;
use std::cell::RefCell;
use std::rc::Rc;
use wasm_bindgen::prelude::*;

/// Global store instance (initialized via init_store)
/// Using Rc<RefCell<>> because WASM is single-threaded and we need to share across async boundaries
thread_local! {
    static STORE: RefCell<Option<Rc<IndexedDbStore>>> = RefCell::new(None);
    static FUZZY: RefCell<FuzzyEngine> = RefCell::new(FuzzyEngine::new());
}

/// Default database name for browser IndexedDB storage
const DEFAULT_DB_NAME: &str = "qntx";

/// Initialize the IndexedDB store. Must be called before any storage operations.
/// Returns a Promise that resolves when initialization is complete.
#[wasm_bindgen]
pub async fn init_store(db_name: Option<String>) -> Result<(), JsValue> {
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
/// Expects JSON matching proto schema (timestamps as numbers, attributes as JSON string).
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
/// Returns JSON matching proto schema (timestamps as numbers, attributes as JSON string).
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
/// Pulls distinct predicates and contexts from the attestation store.
/// Returns JSON: {"predicates": N, "contexts": N, "hash": "..."}
#[wasm_bindgen]
pub async fn fuzzy_rebuild_index() -> Result<String, JsValue> {
    let store = get_store();

    let predicates = store
        .predicates()
        .await
        .map_err(|e| JsValue::from_str(&format!("Failed to load predicates from IndexedDB: {:?}", e)))?;

    let contexts = store
        .contexts()
        .await
        .map_err(|e| JsValue::from_str(&format!("Failed to load contexts from IndexedDB: {:?}", e)))?;

    let (pred_count, ctx_count, hash) = FUZZY.with(|f| {
        f.borrow_mut().rebuild_index(predicates, contexts)
    });

    Ok(format!(
        r#"{{"predicates":{},"contexts":{},"hash":"{}"}}"#,
        pred_count, ctx_count, hash
    ))
}

/// Search the fuzzy index for matching vocabulary.
/// vocab_type: "predicates" or "contexts"
/// Returns JSON array: [{"value":"...", "score":0.95, "strategy":"exact"}, ...]
#[wasm_bindgen]
pub fn fuzzy_search(query: &str, vocab_type: &str, limit: usize, min_score: f64) -> Result<String, JsValue> {
    let vtype = match vocab_type {
        "predicates" => VocabularyType::Predicates,
        "contexts" => VocabularyType::Contexts,
        _ => return Err(JsValue::from_str(&format!(
            "Invalid vocab_type '{}', expected 'predicates' or 'contexts'", vocab_type
        ))),
    };

    let matches = FUZZY.with(|f| {
        f.borrow().find_matches(query, vtype, limit, min_score)
    });

    serde_json::to_string(&matches)
        .map_err(|e| JsValue::from_str(&format!("Failed to serialize fuzzy matches: {}", e)))
}

/// Get fuzzy engine status.
/// Returns JSON: {"ready": bool, "predicates": N, "contexts": N, "hash": "..."}
#[wasm_bindgen]
pub fn fuzzy_status() -> String {
    FUZZY.with(|f| {
        let engine = f.borrow();
        let (pred_count, ctx_count) = engine.get_counts();
        format!(
            r#"{{"ready":{},"predicates":{},"contexts":{},"hash":"{}"}}"#,
            engine.is_ready(),
            pred_count,
            ctx_count,
            engine.get_index_hash()
        )
    })
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
