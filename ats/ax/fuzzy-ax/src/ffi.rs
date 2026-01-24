//! C-compatible FFI interface for FuzzyEngine
//!
//! This module exposes qntx_core's FuzzyEngine through a C ABI via a thread-safe
//! wrapper, enabling integration with Go via CGO.
//!
//! # Memory Ownership Rules
//!
//! - `fuzzy_engine_new()` allocates on Rust heap, caller owns pointer
//! - `fuzzy_engine_free()` must be called to deallocate
//! - Result structs and their strings are owned by caller after return
//! - Free functions must be called to deallocate results

use std::ffi::{CStr, CString};
use std::os::raw::c_char;
use std::ptr;
use std::slice;

use crate::{ThreadSafeFuzzyEngine, VocabularyType};

// Safety limits
const MAX_QUERY_LENGTH: usize = 1000;
const MAX_VOCABULARY_SIZE: usize = 100_000;

/// C-compatible match result
#[repr(C)]
pub struct RustMatchC {
    pub value: *mut c_char,
    pub score: f64,
    pub strategy: *mut c_char,
}

/// C-compatible result wrapper for find_matches
#[repr(C)]
pub struct RustMatchResultC {
    pub success: bool,
    pub error_msg: *mut c_char,
    pub matches: *mut RustMatchC,
    pub matches_len: usize,
    pub search_time_us: u64,
}

/// C-compatible result for rebuild_index
#[repr(C)]
pub struct RustRebuildResultC {
    pub success: bool,
    pub error_msg: *mut c_char,
    pub predicate_count: usize,
    pub context_count: usize,
    pub build_time_ms: u64,
    pub index_hash: *mut c_char,
}

/// C-compatible attribute match result
#[repr(C)]
pub struct RustAttributeMatchC {
    pub node_id: *mut c_char,
    pub field_name: *mut c_char,
    pub field_value: *mut c_char,
    pub excerpt: *mut c_char,
    pub score: f64,
    pub strategy: *mut c_char,
}

/// C-compatible result wrapper for find_attribute_matches
#[repr(C)]
pub struct RustAttributeMatchResultC {
    pub success: bool,
    pub error_msg: *mut c_char,
    pub matches: *mut RustAttributeMatchC,
    pub matches_len: usize,
    pub search_time_us: u64,
}

impl RustMatchResultC {
    fn error(msg: &str) -> Self {
        Self {
            success: false,
            error_msg: CString::new(msg)
                .unwrap_or_else(|_| CString::new(" ").expect("space is valid"))
                .into_raw(),
            matches: ptr::null_mut(),
            matches_len: 0,
            search_time_us: 0,
        }
    }
}

impl RustRebuildResultC {
    fn error(msg: &str) -> Self {
        Self {
            success: false,
            error_msg: CString::new(msg)
                .unwrap_or_else(|_| CString::new(" ").expect("space is valid"))
                .into_raw(),
            predicate_count: 0,
            context_count: 0,
            build_time_ms: 0,
            index_hash: ptr::null_mut(),
        }
    }
}

impl RustAttributeMatchResultC {
    fn error(msg: &str) -> Self {
        Self {
            success: false,
            error_msg: CString::new(msg)
                .unwrap_or_else(|_| CString::new(" ").expect("space is valid"))
                .into_raw(),
            matches: ptr::null_mut(),
            matches_len: 0,
            search_time_us: 0,
        }
    }
}

// ============================================================================
// Engine Lifecycle
// ============================================================================

#[no_mangle]
pub extern "C" fn fuzzy_init_logger() {
    crate::init_logger();
}

#[no_mangle]
pub extern "C" fn fuzzy_engine_new() -> *mut ThreadSafeFuzzyEngine {
    crate::init_logger();
    let engine = Box::new(ThreadSafeFuzzyEngine::new());
    debug!("Created new ThreadSafeFuzzyEngine (backed by qntx-core)");
    Box::into_raw(engine)
}

#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn fuzzy_engine_free(engine: *mut ThreadSafeFuzzyEngine) {
    if !engine.is_null() {
        unsafe {
            let _ = Box::from_raw(engine);
        }
    }
}

// ============================================================================
// Index Management
// ============================================================================

#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn fuzzy_engine_rebuild_index(
    engine: *const ThreadSafeFuzzyEngine,
    predicates: *const *const c_char,
    predicates_len: usize,
    contexts: *const *const c_char,
    contexts_len: usize,
) -> RustRebuildResultC {
    if engine.is_null() {
        return RustRebuildResultC::error("null engine pointer");
    }

    let engine = unsafe { &*engine };

    if predicates_len > MAX_VOCABULARY_SIZE || contexts_len > MAX_VOCABULARY_SIZE {
        return RustRebuildResultC::error("vocabulary exceeds maximum size");
    }

    let predicates_vec = if predicates.is_null() || predicates_len == 0 {
        Vec::new()
    } else {
        match convert_string_array(predicates, predicates_len) {
            Ok(v) => v,
            Err(e) => return RustRebuildResultC::error(&e),
        }
    };

    let contexts_vec = if contexts.is_null() || contexts_len == 0 {
        Vec::new()
    } else {
        match convert_string_array(contexts, contexts_len) {
            Ok(v) => v,
            Err(e) => return RustRebuildResultC::error(&e),
        }
    };

    let (pred_count, ctx_count, build_time, hash) =
        engine.rebuild_index(predicates_vec, contexts_vec);

    RustRebuildResultC {
        success: true,
        error_msg: ptr::null_mut(),
        predicate_count: pred_count,
        context_count: ctx_count,
        build_time_ms: build_time,
        index_hash: CString::new(hash).unwrap_or_default().into_raw(),
    }
}

#[no_mangle]
pub extern "C" fn fuzzy_rebuild_result_free(result: RustRebuildResultC) {
    if !result.error_msg.is_null() {
        unsafe { let _ = CString::from_raw(result.error_msg); }
    }
    if !result.index_hash.is_null() {
        unsafe { let _ = CString::from_raw(result.index_hash); }
    }
}

// ============================================================================
// Matching
// ============================================================================

#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn fuzzy_engine_find_matches(
    engine: *const ThreadSafeFuzzyEngine,
    query: *const c_char,
    vocabulary_type: i32,
    limit: usize,
    min_score: f64,
) -> RustMatchResultC {
    if engine.is_null() {
        return RustMatchResultC::error("null engine pointer");
    }
    if query.is_null() {
        return RustMatchResultC::error("null query pointer");
    }

    let engine = unsafe { &*engine };

    let query_str = match unsafe { CStr::from_ptr(query) }.to_str() {
        Ok(s) => s,
        Err(_) => return RustMatchResultC::error("invalid UTF-8 in query"),
    };

    if query_str.len() > MAX_QUERY_LENGTH {
        return RustMatchResultC::error("query exceeds maximum length");
    }

    let vocab_type = if vocabulary_type == 1 {
        VocabularyType::Contexts
    } else {
        VocabularyType::Predicates
    };

    let limit = if limit > 0 { Some(limit) } else { None };
    let min_score = if min_score > 0.0 { Some(min_score) } else { None };

    let (matches, search_time_us) = engine.find_matches(query_str, vocab_type, limit, min_score);

    if matches.is_empty() {
        return RustMatchResultC {
            success: true,
            error_msg: ptr::null_mut(),
            matches: ptr::null_mut(),
            matches_len: 0,
            search_time_us,
        };
    }

    let mut c_matches: Vec<RustMatchC> = Vec::with_capacity(matches.len());
    for m in matches {
        let value_cstr = match CString::new(m.value) {
            Ok(cs) => cs,
            Err(_) => return RustMatchResultC::error("match value contains null bytes"),
        };
        let strategy_cstr = match CString::new(m.strategy) {
            Ok(cs) => cs,
            Err(_) => return RustMatchResultC::error("strategy contains null bytes"),
        };
        c_matches.push(RustMatchC {
            value: value_cstr.into_raw(),
            score: m.score,
            strategy: strategy_cstr.into_raw(),
        });
    }

    let matches_len = c_matches.len();
    let matches_ptr = Box::into_raw(c_matches.into_boxed_slice()) as *mut RustMatchC;

    RustMatchResultC {
        success: true,
        error_msg: ptr::null_mut(),
        matches: matches_ptr,
        matches_len,
        search_time_us,
    }
}

#[no_mangle]
pub extern "C" fn fuzzy_match_result_free(result: RustMatchResultC) {
    if !result.error_msg.is_null() {
        unsafe { let _ = CString::from_raw(result.error_msg); }
    }

    if !result.matches.is_null() && result.matches_len > 0 {
        let matches_slice = unsafe { slice::from_raw_parts_mut(result.matches, result.matches_len) };
        for m in matches_slice.iter() {
            if !m.value.is_null() {
                unsafe { let _ = CString::from_raw(m.value); }
            }
            if !m.strategy.is_null() {
                unsafe { let _ = CString::from_raw(m.strategy); }
            }
        }
        unsafe {
            let _ = Box::from_raw(std::ptr::slice_from_raw_parts_mut(
                result.matches,
                result.matches_len,
            ));
        }
    }
}

// ============================================================================
// Attribute Matching (CGO-specific)
// ============================================================================

#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn fuzzy_engine_find_attribute_matches(
    engine: *const ThreadSafeFuzzyEngine,
    query: *const c_char,
    attributes_json: *const c_char,
    rich_string_fields: *const *const c_char,
    rich_string_fields_len: usize,
    node_id: *const c_char,
) -> RustAttributeMatchResultC {
    if engine.is_null() {
        return RustAttributeMatchResultC::error("null engine");
    }
    let engine = unsafe { &*engine };

    if query.is_null() {
        return RustAttributeMatchResultC::error("null query");
    }
    let query_str = match unsafe { CStr::from_ptr(query) }.to_str() {
        Ok(s) => s,
        Err(_) => return RustAttributeMatchResultC::error("invalid UTF-8 in query"),
    };

    if query_str.len() > MAX_QUERY_LENGTH {
        return RustAttributeMatchResultC::error("query too long");
    }

    if attributes_json.is_null() {
        return RustAttributeMatchResultC::error("null attributes_json");
    }
    let attributes_str = match unsafe { CStr::from_ptr(attributes_json) }.to_str() {
        Ok(s) => s,
        Err(_) => return RustAttributeMatchResultC::error("invalid UTF-8 in attributes"),
    };

    if node_id.is_null() {
        return RustAttributeMatchResultC::error("null node_id");
    }
    let node_id_str = match unsafe { CStr::from_ptr(node_id) }.to_str() {
        Ok(s) => s,
        Err(_) => return RustAttributeMatchResultC::error("invalid UTF-8 in node_id"),
    };

    let fields = if rich_string_fields_len > 0 && !rich_string_fields.is_null() {
        match convert_string_array(rich_string_fields, rich_string_fields_len) {
            Ok(f) => f,
            Err(e) => return RustAttributeMatchResultC::error(&e),
        }
    } else {
        Vec::new()
    };

    let start = std::time::Instant::now();
    let matches = engine.find_attribute_matches(query_str, attributes_str, &fields, node_id_str);
    let search_time_us = start.elapsed().as_micros() as u64;

    if matches.is_empty() {
        return RustAttributeMatchResultC {
            success: true,
            error_msg: ptr::null_mut(),
            matches: ptr::null_mut(),
            matches_len: 0,
            search_time_us,
        };
    }

    let mut c_matches: Vec<RustAttributeMatchC> = Vec::with_capacity(matches.len());
    for m in matches {
        c_matches.push(RustAttributeMatchC {
            node_id: CString::new(m.node_id).unwrap_or_default().into_raw(),
            field_name: CString::new(m.field_name).unwrap_or_default().into_raw(),
            field_value: CString::new(m.field_value).unwrap_or_default().into_raw(),
            excerpt: CString::new(m.excerpt).unwrap_or_default().into_raw(),
            score: m.score,
            strategy: CString::new(m.strategy).unwrap_or_default().into_raw(),
        });
    }

    let matches_len = c_matches.len();
    let matches_ptr = Box::into_raw(c_matches.into_boxed_slice()) as *mut RustAttributeMatchC;

    RustAttributeMatchResultC {
        success: true,
        error_msg: ptr::null_mut(),
        matches: matches_ptr,
        matches_len,
        search_time_us,
    }
}

#[no_mangle]
pub extern "C" fn fuzzy_attribute_result_free(result: RustAttributeMatchResultC) {
    if !result.error_msg.is_null() {
        unsafe { let _ = CString::from_raw(result.error_msg); }
    }

    if !result.matches.is_null() && result.matches_len > 0 {
        let matches_slice = unsafe { slice::from_raw_parts_mut(result.matches, result.matches_len) };
        for m in matches_slice.iter() {
            if !m.node_id.is_null() { unsafe { let _ = CString::from_raw(m.node_id); } }
            if !m.field_name.is_null() { unsafe { let _ = CString::from_raw(m.field_name); } }
            if !m.field_value.is_null() { unsafe { let _ = CString::from_raw(m.field_value); } }
            if !m.excerpt.is_null() { unsafe { let _ = CString::from_raw(m.excerpt); } }
            if !m.strategy.is_null() { unsafe { let _ = CString::from_raw(m.strategy); } }
        }
        unsafe {
            let _ = Box::from_raw(std::ptr::slice_from_raw_parts_mut(
                result.matches,
                result.matches_len,
            ));
        }
    }
}

// ============================================================================
// Utilities
// ============================================================================

#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn fuzzy_engine_get_hash(engine: *const ThreadSafeFuzzyEngine) -> *mut c_char {
    if engine.is_null() {
        return ptr::null_mut();
    }
    let engine = unsafe { &*engine };
    CString::new(engine.get_index_hash()).unwrap_or_default().into_raw()
}

#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn fuzzy_engine_is_ready(engine: *const ThreadSafeFuzzyEngine) -> bool {
    if engine.is_null() {
        return false;
    }
    let engine = unsafe { &*engine };
    engine.is_ready()
}

#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn fuzzy_string_free(s: *mut c_char) {
    if !s.is_null() {
        unsafe { let _ = CString::from_raw(s); }
    }
}

#[no_mangle]
pub extern "C" fn fuzzy_engine_version() -> *const c_char {
    concat!(env!("CARGO_PKG_VERSION"), "\0").as_ptr() as *const c_char
}

// ============================================================================
// Helpers
// ============================================================================

fn convert_string_array(arr: *const *const c_char, len: usize) -> Result<Vec<String>, String> {
    let slice = unsafe { slice::from_raw_parts(arr, len) };
    let mut result = Vec::with_capacity(len);

    for (i, &ptr) in slice.iter().enumerate() {
        if ptr.is_null() {
            return Err(format!("null string at index {}", i));
        }
        match unsafe { CStr::from_ptr(ptr) }.to_str() {
            Ok(s) => result.push(s.to_string()),
            Err(_) => return Err(format!("invalid UTF-8 at index {}", i)),
        }
    }

    Ok(result)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_engine_lifecycle() {
        let engine = fuzzy_engine_new();
        assert!(!engine.is_null());
        fuzzy_engine_free(engine);
    }

    #[test]
    fn test_null_engine_handling() {
        let result = fuzzy_engine_find_matches(
            ptr::null(),
            CString::new("test").unwrap().as_ptr(),
            0,
            10,
            0.6,
        );
        assert!(!result.success);
        fuzzy_match_result_free(result);
    }

    #[test]
    fn test_find_matches_ffi() {
        let engine = fuzzy_engine_new();

        let predicates = [
            CString::new("is_author_of").unwrap(),
            CString::new("works_at").unwrap(),
        ];
        let pred_ptrs: Vec<*const c_char> = predicates.iter().map(|s| s.as_ptr()).collect();

        let rebuild_result =
            fuzzy_engine_rebuild_index(engine, pred_ptrs.as_ptr(), pred_ptrs.len(), ptr::null(), 0);
        assert!(rebuild_result.success);
        fuzzy_rebuild_result_free(rebuild_result);

        let query = CString::new("author").unwrap();
        let result = fuzzy_engine_find_matches(engine, query.as_ptr(), 0, 10, 0.5);

        assert!(result.success);
        assert!(result.matches_len > 0);

        fuzzy_match_result_free(result);
        fuzzy_engine_free(engine);
    }
}
