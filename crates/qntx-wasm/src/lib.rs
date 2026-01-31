//! WASM bindings for QNTX attestation verification
//!
//! This module provides JavaScript-accessible functions for verifying
//! attestations in the browser without server roundtrips.

use serde::{Deserialize, Serialize};
use wasm_bindgen::prelude::*;

// Use wee_alloc as the global allocator to reduce WASM size
#[global_allocator]
static ALLOC: wee_alloc::WeeAlloc = wee_alloc::WeeAlloc::INIT;

// Helper macro for logging to browser console
macro_rules! log {
    ($($t:tt)*) => (web_sys::console::log_1(&format!($($t)*).into()))
}

/// JavaScript-friendly attestation structure
#[wasm_bindgen]
#[derive(Serialize, Deserialize)]
pub struct JsAttestation {
    id: String,
    subjects: Vec<String>,
    predicates: Vec<String>,
    contexts: Vec<String>,
    actors: Vec<String>,
    timestamp: f64, // JavaScript Date.now() format (milliseconds)
    source: String,
    #[serde(skip)]
    attributes: JsValue, // Will be a JavaScript object
}

#[wasm_bindgen]
impl JsAttestation {
    /// Create a new attestation from JavaScript
    #[wasm_bindgen(constructor)]
    pub fn new(
        id: String,
        subjects: Vec<String>,
        predicates: Vec<String>,
        contexts: Vec<String>,
        actors: Vec<String>,
        timestamp: f64,
        source: String,
        attributes: JsValue,
    ) -> Result<JsAttestation, JsValue> {
        Ok(JsAttestation {
            id,
            subjects,
            predicates,
            contexts,
            actors,
            timestamp,
            source,
            attributes,
        })
    }

    /// Get the attestation ID
    #[wasm_bindgen(getter)]
    pub fn id(&self) -> String {
        self.id.clone()
    }

    /// Get subjects as JavaScript array
    #[wasm_bindgen(getter)]
    pub fn subjects(&self) -> Vec<String> {
        self.subjects.clone()
    }

    /// Get predicates as JavaScript array
    #[wasm_bindgen(getter)]
    pub fn predicates(&self) -> Vec<String> {
        self.predicates.clone()
    }

    /// Get contexts as JavaScript array
    #[wasm_bindgen(getter)]
    pub fn contexts(&self) -> Vec<String> {
        self.contexts.clone()
    }

    /// Get actors as JavaScript array
    #[wasm_bindgen(getter)]
    pub fn actors(&self) -> Vec<String> {
        self.actors.clone()
    }

    /// Get timestamp (milliseconds since epoch)
    #[wasm_bindgen(getter)]
    pub fn timestamp(&self) -> f64 {
        self.timestamp
    }

    /// Get source
    #[wasm_bindgen(getter)]
    pub fn source(&self) -> String {
        self.source.clone()
    }

    /// Convert to JSON string
    #[wasm_bindgen(js_name = toJSON)]
    pub fn to_json(&self) -> Result<String, JsValue> {
        serde_json::to_string(&self)
            .map_err(|e| JsValue::from_str(&format!("Serialization error: {}", e)))
    }

    /// Create from JSON string
    #[wasm_bindgen(js_name = fromJSON)]
    pub fn from_json(json: &str) -> Result<JsAttestation, JsValue> {
        serde_json::from_str(json)
            .map_err(|e| JsValue::from_str(&format!("Deserialization error: {}", e)))
    }
}

/// Verify that an attestation matches given criteria
#[wasm_bindgen]
pub fn verify_attestation(
    attestation: &JsAttestation,
    subject_pattern: Option<String>,
    predicate_pattern: Option<String>,
    context_pattern: Option<String>,
    actor_pattern: Option<String>,
) -> bool {
    log!("Verifying attestation: {}", attestation.id);

    // Check subject pattern
    if let Some(pattern) = subject_pattern {
        if !attestation.subjects.iter().any(|s| s.contains(&pattern)) {
            log!("Subject pattern '{}' not matched", pattern);
            return false;
        }
    }

    // Check predicate pattern
    if let Some(pattern) = predicate_pattern {
        if !attestation.predicates.iter().any(|p| p.contains(&pattern)) {
            log!("Predicate pattern '{}' not matched", pattern);
            return false;
        }
    }

    // Check context pattern
    if let Some(pattern) = context_pattern {
        if !attestation.contexts.iter().any(|c| c.contains(&pattern)) {
            log!("Context pattern '{}' not matched", pattern);
            return false;
        }
    }

    // Check actor pattern
    if let Some(pattern) = actor_pattern {
        if !attestation.actors.iter().any(|a| a.contains(&pattern)) {
            log!("Actor pattern '{}' not matched", pattern);
            return false;
        }
    }

    log!("Attestation {} verified successfully", attestation.id);
    true
}

/// Check if an attestation has a specific subject
#[wasm_bindgen]
pub fn has_subject(attestation: &JsAttestation, subject: &str) -> bool {
    attestation.subjects.iter().any(|s| s == subject)
}

/// Check if an attestation has a specific predicate
#[wasm_bindgen]
pub fn has_predicate(attestation: &JsAttestation, predicate: &str) -> bool {
    attestation.predicates.iter().any(|p| p == predicate)
}

/// Check if an attestation has a specific context
#[wasm_bindgen]
pub fn has_context(attestation: &JsAttestation, context: &str) -> bool {
    attestation.contexts.iter().any(|c| c == context)
}

/// Check if an attestation has a specific actor
#[wasm_bindgen]
pub fn has_actor(attestation: &JsAttestation, actor: &str) -> bool {
    attestation.actors.iter().any(|a| a == actor)
}

/// Filter attestations based on criteria
#[wasm_bindgen]
pub fn filter_attestations(
    attestations_json: &str,
    subject_pattern: Option<String>,
    predicate_pattern: Option<String>,
    context_pattern: Option<String>,
    actor_pattern: Option<String>,
) -> Result<String, JsValue> {
    // Parse the JSON array
    let attestations: Vec<JsAttestation> = serde_json::from_str(attestations_json)
        .map_err(|e| JsValue::from_str(&format!("Failed to parse attestations: {}", e)))?;

    // Filter attestations
    let filtered: Vec<&JsAttestation> = attestations
        .iter()
        .filter(|att| {
            verify_attestation(
                att,
                subject_pattern.clone(),
                predicate_pattern.clone(),
                context_pattern.clone(),
                actor_pattern.clone(),
            )
        })
        .collect();

    // Serialize back to JSON
    serde_json::to_string(&filtered)
        .map_err(|e| JsValue::from_str(&format!("Failed to serialize results: {}", e)))
}

/// Initialize the WASM module (called once when loaded)
#[wasm_bindgen(start)]
pub fn init() {
    log!("QNTX WASM module initialized");
}

/// Get version information
#[wasm_bindgen]
pub fn version() -> String {
    env!("CARGO_PKG_VERSION").to_string()
}
