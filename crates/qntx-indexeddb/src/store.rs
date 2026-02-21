//! IndexedDB storage backend implementing the same contract as AttestationStore and QueryStore.
//!
//! Because IndexedDB is inherently async, the methods here are async equivalents of the
//! synchronous `AttestationStore` and `QueryStore` trait methods from `qntx-core`.
//! The method signatures and semantics match exactly — same inputs, same outputs, same errors.

use std::collections::{HashMap, HashSet};

use qntx_core::{
    attestation::{Attestation, AxFilter, AxResult, AxSummary},
    storage::{StorageStats, StoreError},
};
use wasm_bindgen::prelude::*;
use web_sys::{IdbDatabase, IdbTransactionMode};

use crate::idb;

type StoreResult<T> = std::result::Result<T, StoreError>;

/// IndexedDB-backed attestation store for browser WASM.
///
/// Stores attestations in an IndexedDB object store with the same schema
/// and semantics as the SQLite backend. Array fields (subjects, predicates,
/// contexts, actors) are stored as native JS arrays to leverage IndexedDB's
/// multiEntry indexes for efficient lookups.
///
/// All methods are async because IndexedDB is callback-based.
pub struct IndexedDbStore {
    db: IdbDatabase,
}

impl IndexedDbStore {
    /// Open or create an IndexedDB store with the given database name.
    pub async fn open(db_name: &str) -> crate::Result<Self> {
        let db = idb::open_database(db_name).await?;
        Ok(Self { db })
    }

    /// Open with the default database name "qntx".
    pub async fn open_default() -> crate::Result<Self> {
        Self::open("qntx").await
    }

    /// Close the database connection.
    pub fn close(&self) {
        self.db.close();
    }

    /// Delete the database (for testing/cleanup).
    pub async fn delete_database(db_name: &str) -> crate::Result<()> {
        idb::delete_database(db_name).await
    }

    // ========================================================================
    // AttestationStore methods (async equivalents)
    // ========================================================================

    /// Store an attestation.
    /// If an attestation with the same ID already exists, returns `StoreError::AlreadyExists`.
    pub async fn put(&self, attestation: Attestation) -> StoreResult<()> {
        // Check for duplicates
        if self.exists(&attestation.id).await? {
            return Err(StoreError::AlreadyExists(attestation.id));
        }

        let js_val = attestation_to_js(&attestation)?;

        let (tx, store) = idb::begin_transaction(&self.db, IdbTransactionMode::Readwrite)
            .map_err(StoreError::from)?;

        let req = store
            .add(&js_val)
            .map_err(|e| StoreError::Backend(format!("IDB add: {:?}", e)))?;
        idb::await_request(&req).await.map_err(StoreError::from)?;
        idb::await_transaction(&tx)
            .await
            .map_err(StoreError::from)?;

        Ok(())
    }

    /// Retrieve an attestation by ID.
    /// Returns `None` if not found.
    pub async fn get(&self, id: &str) -> StoreResult<Option<Attestation>> {
        let (tx, store) = idb::begin_transaction(&self.db, IdbTransactionMode::Readonly)
            .map_err(StoreError::from)?;

        let key = JsValue::from_str(id);
        let req = store
            .get(&key)
            .map_err(|e| StoreError::Backend(format!("IDB get: {:?}", e)))?;

        let result = idb::await_request(&req).await.map_err(StoreError::from)?;
        idb::await_transaction(&tx)
            .await
            .map_err(StoreError::from)?;

        if result.is_undefined() || result.is_null() {
            return Ok(None);
        }

        let attestation = js_to_attestation(&result)?;
        Ok(Some(attestation))
    }

    /// Check if an attestation exists.
    pub async fn exists(&self, id: &str) -> StoreResult<bool> {
        let (tx, store) = idb::begin_transaction(&self.db, IdbTransactionMode::Readonly)
            .map_err(StoreError::from)?;

        let key = JsValue::from_str(id);
        let req = store
            .count_with_key(&key)
            .map_err(|e| StoreError::Backend(format!("IDB count: {:?}", e)))?;

        let result = idb::await_request(&req).await.map_err(StoreError::from)?;
        idb::await_transaction(&tx)
            .await
            .map_err(StoreError::from)?;

        let count = result.as_f64().unwrap_or(0.0) as u32;
        Ok(count > 0)
    }

    /// Delete an attestation by ID.
    /// Returns `true` if the attestation was deleted, `false` if it didn't exist.
    pub async fn delete(&self, id: &str) -> StoreResult<bool> {
        let existed = self.exists(id).await?;
        if !existed {
            return Ok(false);
        }

        let (tx, store) = idb::begin_transaction(&self.db, IdbTransactionMode::Readwrite)
            .map_err(StoreError::from)?;

        let key = JsValue::from_str(id);
        let req = store
            .delete(&key)
            .map_err(|e| StoreError::Backend(format!("IDB delete: {:?}", e)))?;

        idb::await_request(&req).await.map_err(StoreError::from)?;
        idb::await_transaction(&tx)
            .await
            .map_err(StoreError::from)?;

        Ok(true)
    }

    /// Update an existing attestation.
    /// Returns `StoreError::NotFound` if the attestation doesn't exist.
    pub async fn update(&self, attestation: Attestation) -> StoreResult<()> {
        if !self.exists(&attestation.id).await? {
            return Err(StoreError::NotFound(attestation.id));
        }

        let js_val = attestation_to_js(&attestation)?;

        let (tx, store) = idb::begin_transaction(&self.db, IdbTransactionMode::Readwrite)
            .map_err(StoreError::from)?;

        let req = store
            .put(&js_val)
            .map_err(|e| StoreError::Backend(format!("IDB put: {:?}", e)))?;
        idb::await_request(&req).await.map_err(StoreError::from)?;
        idb::await_transaction(&tx)
            .await
            .map_err(StoreError::from)?;

        Ok(())
    }

    /// Get all attestation IDs.
    pub async fn ids(&self) -> StoreResult<Vec<String>> {
        let (tx, store) = idb::begin_transaction(&self.db, IdbTransactionMode::Readonly)
            .map_err(StoreError::from)?;

        let req = store
            .get_all_keys()
            .map_err(|e| StoreError::Backend(format!("IDB getAllKeys: {:?}", e)))?;

        let result = idb::await_request(&req).await.map_err(StoreError::from)?;
        idb::await_transaction(&tx)
            .await
            .map_err(StoreError::from)?;

        let array = js_sys::Array::from(&result);
        let mut ids: Vec<String> = Vec::with_capacity(array.length() as usize);
        for i in 0..array.length() {
            if let Some(s) = array.get(i).as_string() {
                ids.push(s);
            }
        }
        Ok(ids)
    }

    /// Get the total count of attestations.
    pub async fn count(&self) -> StoreResult<usize> {
        let (tx, store) = idb::begin_transaction(&self.db, IdbTransactionMode::Readonly)
            .map_err(StoreError::from)?;

        let req = store
            .count()
            .map_err(|e| StoreError::Backend(format!("IDB count: {:?}", e)))?;

        let result = idb::await_request(&req).await.map_err(StoreError::from)?;
        idb::await_transaction(&tx)
            .await
            .map_err(StoreError::from)?;

        Ok(result.as_f64().unwrap_or(0.0) as usize)
    }

    /// Clear all attestations.
    pub async fn clear(&self) -> StoreResult<()> {
        let (tx, store) = idb::begin_transaction(&self.db, IdbTransactionMode::Readwrite)
            .map_err(StoreError::from)?;

        let req = store
            .clear()
            .map_err(|e| StoreError::Backend(format!("IDB clear: {:?}", e)))?;

        idb::await_request(&req).await.map_err(StoreError::from)?;
        idb::await_transaction(&tx)
            .await
            .map_err(StoreError::from)?;

        Ok(())
    }

    // ========================================================================
    // QueryStore methods (async equivalents)
    // ========================================================================

    /// Execute an AX query filter and return matching attestations.
    pub async fn query(&self, filter: &AxFilter) -> StoreResult<AxResult> {
        let all = self.get_all().await?;

        let mut matching: Vec<Attestation> = all
            .into_iter()
            .filter(|a| matches_filter(a, filter))
            .collect();

        if let Some(limit) = filter.limit {
            matching.truncate(limit);
        }

        let summary = build_summary(&matching);

        Ok(AxResult {
            attestations: matching,
            conflicts: Vec::new(),
            summary,
        })
    }

    /// Get all distinct predicates in the store.
    pub async fn predicates(&self) -> StoreResult<Vec<String>> {
        let all = self.get_all().await?;
        let mut set = HashSet::new();
        for a in &all {
            for p in &a.predicates {
                set.insert(p.clone());
            }
        }
        let mut result: Vec<String> = set.into_iter().collect();
        result.sort();
        Ok(result)
    }

    /// Get all distinct contexts in the store.
    pub async fn contexts(&self) -> StoreResult<Vec<String>> {
        let all = self.get_all().await?;
        let mut set = HashSet::new();
        for a in &all {
            for c in &a.contexts {
                set.insert(c.clone());
            }
        }
        let mut result: Vec<String> = set.into_iter().collect();
        result.sort();
        Ok(result)
    }

    /// Get all distinct subjects in the store.
    pub async fn subjects(&self) -> StoreResult<Vec<String>> {
        let all = self.get_all().await?;
        let mut set = HashSet::new();
        for a in &all {
            for s in &a.subjects {
                set.insert(s.clone());
            }
        }
        let mut result: Vec<String> = set.into_iter().collect();
        result.sort();
        Ok(result)
    }

    /// Get all distinct actors in the store.
    pub async fn actors(&self) -> StoreResult<Vec<String>> {
        let all = self.get_all().await?;
        let mut set = HashSet::new();
        for a in &all {
            for actor in &a.actors {
                set.insert(actor.clone());
            }
        }
        let mut result: Vec<String> = set.into_iter().collect();
        result.sort();
        Ok(result)
    }

    /// Get storage statistics.
    pub async fn stats(&self) -> StoreResult<StorageStats> {
        Ok(StorageStats {
            total_attestations: self.count().await?,
            unique_subjects: self.subjects().await?.len(),
            unique_predicates: self.predicates().await?.len(),
            unique_contexts: self.contexts().await?.len(),
            unique_actors: self.actors().await?.len(),
        })
    }

    // ========================================================================
    // Internal helpers
    // ========================================================================

    /// Retrieve all attestations from the store.
    pub async fn get_all(&self) -> StoreResult<Vec<Attestation>> {
        let (tx, store) = idb::begin_transaction(&self.db, IdbTransactionMode::Readonly)
            .map_err(StoreError::from)?;

        let req = store
            .get_all()
            .map_err(|e| StoreError::Backend(format!("IDB getAll: {:?}", e)))?;

        let result = idb::await_request(&req).await.map_err(StoreError::from)?;
        idb::await_transaction(&tx)
            .await
            .map_err(StoreError::from)?;

        let array = js_sys::Array::from(&result);
        let mut attestations = Vec::with_capacity(array.length() as usize);
        for i in 0..array.length() {
            let js_val = array.get(i);
            let attestation = js_to_attestation(&js_val)?;
            attestations.push(attestation);
        }
        Ok(attestations)
    }
}

// ============================================================================
// JS <-> Attestation conversion
// ============================================================================

/// Convert an Attestation to a JS object for IndexedDB storage.
///
/// Array fields are stored as native JS arrays so IndexedDB multiEntry indexes work.
/// Timestamps are stored as numbers (milliseconds) for efficient range queries.
fn attestation_to_js(attestation: &Attestation) -> StoreResult<JsValue> {
    let obj = js_sys::Object::new();

    set_prop(&obj, "id", &JsValue::from_str(&attestation.id))?;
    set_prop(
        &obj,
        "subjects",
        &string_vec_to_js_array(&attestation.subjects),
    )?;
    set_prop(
        &obj,
        "predicates",
        &string_vec_to_js_array(&attestation.predicates),
    )?;
    set_prop(
        &obj,
        "contexts",
        &string_vec_to_js_array(&attestation.contexts),
    )?;
    set_prop(&obj, "actors", &string_vec_to_js_array(&attestation.actors))?;
    set_prop(
        &obj,
        "timestamp",
        &JsValue::from_f64(attestation.timestamp as f64),
    )?;
    set_prop(&obj, "source", &JsValue::from_str(&attestation.source))?;
    set_prop(
        &obj,
        "created_at",
        &JsValue::from_f64(attestation.created_at as f64),
    )?;

    // Attributes: store as JSON string if non-empty, null otherwise
    if attestation.attributes.is_empty() {
        set_prop(&obj, "attributes", &JsValue::NULL)?;
    } else {
        let json = serde_json::to_string(&attestation.attributes)
            .map_err(|e| StoreError::Serialization(e.to_string()))?;
        set_prop(&obj, "attributes", &JsValue::from_str(&json))?;
    }

    Ok(obj.into())
}

/// Convert a JS object from IndexedDB back to an Attestation.
fn js_to_attestation(val: &JsValue) -> StoreResult<Attestation> {
    let id = get_string_prop(val, "id")?;
    let subjects = get_string_array_prop(val, "subjects")?;
    let predicates = get_string_array_prop(val, "predicates")?;
    let contexts = get_string_array_prop(val, "contexts")?;
    let actors = get_string_array_prop(val, "actors")?;
    let timestamp = get_number_prop(val, "timestamp")? as i64;
    let source = get_string_prop(val, "source")?;
    let created_at = get_number_prop(val, "created_at")? as i64;

    let attributes_val = js_sys::Reflect::get(val, &"attributes".into())
        .map_err(|_| StoreError::Serialization("missing attributes".into()))?;

    let attributes = if attributes_val.is_null() || attributes_val.is_undefined() {
        HashMap::new()
    } else if let Some(json_str) = attributes_val.as_string() {
        serde_json::from_str(&json_str)
            .map_err(|e| StoreError::Serialization(format!("attributes: {}", e)))?
    } else {
        HashMap::new()
    };

    Ok(Attestation {
        id,
        subjects,
        predicates,
        contexts,
        actors,
        timestamp,
        source,
        attributes,
        created_at,
    })
}

/// Set a property on a JS object.
fn set_prop(obj: &js_sys::Object, key: &str, val: &JsValue) -> StoreResult<()> {
    js_sys::Reflect::set(obj, &key.into(), val)
        .map_err(|_| StoreError::Backend(format!("failed to set property: {}", key)))?;
    Ok(())
}

/// Get a string property from a JS object.
fn get_string_prop(val: &JsValue, key: &str) -> StoreResult<String> {
    let prop = js_sys::Reflect::get(val, &key.into())
        .map_err(|_| StoreError::Serialization(format!("missing property: {}", key)))?;
    prop.as_string()
        .ok_or_else(|| StoreError::Serialization(format!("{} is not a string", key)))
}

/// Get a number property from a JS object.
fn get_number_prop(val: &JsValue, key: &str) -> StoreResult<f64> {
    let prop = js_sys::Reflect::get(val, &key.into())
        .map_err(|_| StoreError::Serialization(format!("missing property: {}", key)))?;
    prop.as_f64()
        .ok_or_else(|| StoreError::Serialization(format!("{} is not a number", key)))
}

/// Get a string array property from a JS object.
fn get_string_array_prop(val: &JsValue, key: &str) -> StoreResult<Vec<String>> {
    let prop = js_sys::Reflect::get(val, &key.into())
        .map_err(|_| StoreError::Serialization(format!("missing property: {}", key)))?;
    let array = js_sys::Array::from(&prop);
    let mut result = Vec::with_capacity(array.length() as usize);
    for i in 0..array.length() {
        if let Some(s) = array.get(i).as_string() {
            result.push(s);
        }
    }
    Ok(result)
}

/// Convert a Vec<String> to a JS Array.
fn string_vec_to_js_array(vec: &[String]) -> JsValue {
    let array = js_sys::Array::new_with_length(vec.len() as u32);
    for (i, s) in vec.iter().enumerate() {
        array.set(i as u32, JsValue::from_str(s));
    }
    array.into()
}

// ============================================================================
// Query filtering (same logic as MemoryStore)
// ============================================================================

/// Check if an attestation matches the given filter.
fn matches_filter(attestation: &Attestation, filter: &AxFilter) -> bool {
    if !filter.subjects.is_empty() {
        let has_match = attestation
            .subjects
            .iter()
            .any(|s| filter.subjects.contains(s));
        if !has_match {
            return false;
        }
    }

    if !filter.predicates.is_empty() {
        let has_match = attestation
            .predicates
            .iter()
            .any(|p| filter.predicates.contains(p));
        if !has_match {
            return false;
        }
    }

    if !filter.contexts.is_empty() {
        let has_match = attestation
            .contexts
            .iter()
            .any(|c| filter.contexts.contains(c));
        if !has_match {
            return false;
        }
    }

    if !filter.actors.is_empty() {
        let has_match = attestation.actors.iter().any(|a| filter.actors.contains(a));
        if !has_match {
            return false;
        }
    }

    if let Some(start) = filter.time_start {
        if attestation.timestamp < start {
            return false;
        }
    }
    if let Some(end) = filter.time_end {
        if attestation.timestamp > end {
            return false;
        }
    }

    true
}

/// Build a summary from a list of attestations.
fn build_summary(attestations: &[Attestation]) -> AxSummary {
    let mut summary = AxSummary {
        total_attestations: attestations.len(),
        unique_subjects: HashMap::new(),
        unique_predicates: HashMap::new(),
        unique_contexts: HashMap::new(),
        unique_actors: HashMap::new(),
    };

    for attestation in attestations {
        for subject in &attestation.subjects {
            *summary.unique_subjects.entry(subject.clone()).or_insert(0) += 1;
        }
        for predicate in &attestation.predicates {
            *summary
                .unique_predicates
                .entry(predicate.clone())
                .or_insert(0) += 1;
        }
        for context in &attestation.contexts {
            *summary.unique_contexts.entry(context.clone()).or_insert(0) += 1;
        }
        for actor in &attestation.actors {
            *summary.unique_actors.entry(actor.clone()).or_insert(0) += 1;
        }
    }

    summary
}
