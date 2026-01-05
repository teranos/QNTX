//! C-compatible FFI interface for FuzzyEngine
//!
//! This module exposes the FuzzyEngine functionality through a C ABI,
//! enabling integration with Go via CGO or any other language with C FFI.
//!
//! # Memory Ownership Rules
//!
//! - `fuzzy_engine_new()` allocates on Rust heap, caller owns pointer
//! - `fuzzy_engine_free()` must be called to deallocate
//! - `RustMatchResultC` and its strings are owned by caller after return
//! - `fuzzy_match_result_free()` must be called to deallocate results
//!
//! # Thread Safety
//!
//! The FuzzyEngine is internally thread-safe via RwLock. Multiple threads
//! can call `fuzzy_engine_find_matches` concurrently on the same engine.
//!
//! # Safety
//!
//! All public FFI functions handle null pointer checks internally.
//! The caller is responsible for passing valid pointers as documented.

use std::ffi::{CStr, CString};
use std::os::raw::c_char;
use std::ptr;
use std::slice;

use crate::engine::{FuzzyEngine, VocabularyType};

// Safety limits to prevent DoS attacks
const MAX_QUERY_LENGTH: usize = 1000;
const MAX_VOCABULARY_SIZE: usize = 100_000;

/// C-compatible match result
#[repr(C)]
pub struct RustMatchC {
    /// Null-terminated UTF-8 string (owned, must be freed)
    pub value: *mut c_char,
    /// Match score 0.0-1.0
    pub score: f64,
    /// Matching strategy name (owned, must be freed)
    pub strategy: *mut c_char,
}

/// C-compatible result wrapper for find_matches
#[repr(C)]
pub struct RustMatchResultC {
    /// True if operation succeeded
    pub success: bool,
    /// Error message if success is false (owned, must be freed)
    pub error_msg: *mut c_char,
    /// Array of matches (owned, must be freed with fuzzy_match_result_free)
    pub matches: *mut RustMatchC,
    /// Number of matches in array
    pub matches_len: usize,
    /// Search time in microseconds
    pub search_time_us: u64,
}

/// C-compatible result for rebuild_index
#[repr(C)]
pub struct RustRebuildResultC {
    /// True if operation succeeded
    pub success: bool,
    /// Error message if success is false (owned)
    pub error_msg: *mut c_char,
    /// Number of predicates indexed
    pub predicate_count: usize,
    /// Number of contexts indexed
    pub context_count: usize,
    /// Build time in milliseconds
    pub build_time_ms: u64,
    /// Hash for change detection (owned)
    pub index_hash: *mut c_char,
}

impl RustMatchResultC {
    fn error(msg: &str) -> Self {
        Self {
            success: false,
            // Safe fallback: single space cannot contain null bytes
            error_msg: CString::new(msg)
                .unwrap_or_else(|_| CString::new(" ").expect("space is valid CString"))
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
            // Safe fallback: single space cannot contain null bytes
            error_msg: CString::new(msg)
                .unwrap_or_else(|_| CString::new(" ").expect("space is valid CString"))
                .into_raw(),
            predicate_count: 0,
            context_count: 0,
            build_time_ms: 0,
            index_hash: ptr::null_mut(),
        }
    }
}

// ============================================================================
// Engine Lifecycle
// ============================================================================

/// Create a new FuzzyEngine instance.
///
/// # Returns
/// Pointer to FuzzyEngine, or NULL on allocation failure.
/// Caller owns the pointer and must call `fuzzy_engine_free` to deallocate.
///
/// # Safety
/// The returned pointer is valid until `fuzzy_engine_free` is called.
#[no_mangle]
pub extern "C" fn fuzzy_engine_new() -> *mut FuzzyEngine {
    Box::into_raw(Box::new(FuzzyEngine::new()))
}

/// Free a FuzzyEngine instance.
///
/// # Safety
/// - `engine` must be a valid pointer from `fuzzy_engine_new`
/// - `engine` must not be used after this call
/// - Safe to call with NULL (no-op)
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn fuzzy_engine_free(engine: *mut FuzzyEngine) {
    if !engine.is_null() {
        unsafe {
            let _ = Box::from_raw(engine);
        }
    }
}

// ============================================================================
// Index Management
// ============================================================================

/// Rebuild the fuzzy index with new vocabulary.
///
/// # Arguments
/// - `engine`: Valid FuzzyEngine pointer
/// - `predicates`: Array of null-terminated UTF-8 strings
/// - `predicates_len`: Number of strings in predicates array
/// - `contexts`: Array of null-terminated UTF-8 strings
/// - `contexts_len`: Number of strings in contexts array
///
/// # Returns
/// RustRebuildResultC with operation status. Caller must call
/// `fuzzy_rebuild_result_free` to deallocate.
///
/// # Safety
/// - `engine` must be valid
/// - String arrays must contain valid pointers for their stated lengths
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn fuzzy_engine_rebuild_index(
    engine: *const FuzzyEngine,
    predicates: *const *const c_char,
    predicates_len: usize,
    contexts: *const *const c_char,
    contexts_len: usize,
) -> RustRebuildResultC {
    // Validate engine pointer
    if engine.is_null() {
        return RustRebuildResultC::error("null engine pointer");
    }

    let engine = unsafe { &*engine };

    // Validate vocabulary sizes to prevent DoS
    if predicates_len > MAX_VOCABULARY_SIZE {
        return RustRebuildResultC::error("predicate vocabulary exceeds maximum size");
    }
    if contexts_len > MAX_VOCABULARY_SIZE {
        return RustRebuildResultC::error("context vocabulary exceeds maximum size");
    }

    // Convert C string arrays to Vec<String>
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

    // Rebuild index
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

/// Free a RustRebuildResultC.
#[no_mangle]
pub extern "C" fn fuzzy_rebuild_result_free(result: RustRebuildResultC) {
    if !result.error_msg.is_null() {
        unsafe {
            let _ = CString::from_raw(result.error_msg);
        }
    }
    if !result.index_hash.is_null() {
        unsafe {
            let _ = CString::from_raw(result.index_hash);
        }
    }
}

// ============================================================================
// Matching
// ============================================================================

/// Find matches for a query in the vocabulary.
///
/// # Arguments
/// - `engine`: Valid FuzzyEngine pointer
/// - `query`: Null-terminated UTF-8 query string
/// - `vocabulary_type`: 0 for predicates, 1 for contexts
/// - `limit`: Maximum results (0 for default of 20)
/// - `min_score`: Minimum score 0.0-1.0 (0.0 for default of 0.6)
///
/// # Returns
/// RustMatchResultC with matches. Caller must call `fuzzy_match_result_free`.
///
/// # Safety
/// - `engine` must be valid
/// - `query` must be a valid null-terminated string
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn fuzzy_engine_find_matches(
    engine: *const FuzzyEngine,
    query: *const c_char,
    vocabulary_type: i32,
    limit: usize,
    min_score: f64,
) -> RustMatchResultC {
    // Validate pointers
    if engine.is_null() {
        return RustMatchResultC::error("null engine pointer");
    }
    if query.is_null() {
        return RustMatchResultC::error("null query pointer");
    }

    let engine = unsafe { &*engine };

    // Convert query to Rust string
    let query_str = match unsafe { CStr::from_ptr(query) }.to_str() {
        Ok(s) => s,
        Err(_) => return RustMatchResultC::error("invalid UTF-8 in query"),
    };

    // Validate query length to prevent DoS
    if query_str.len() > MAX_QUERY_LENGTH {
        return RustMatchResultC::error("query exceeds maximum length");
    }

    // Convert vocabulary type
    let vocab_type = if vocabulary_type == 1 {
        VocabularyType::Contexts
    } else {
        VocabularyType::Predicates
    };

    // Apply defaults
    let limit = if limit > 0 { Some(limit) } else { None };
    let min_score = if min_score > 0.0 {
        Some(min_score)
    } else {
        None
    };

    // Execute search
    let (matches, search_time_us) = engine.find_matches(query_str, vocab_type, limit, min_score);

    // Convert results to C format
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
        c_matches.push(RustMatchC {
            // Safe: value and strategy come from Rust engine (not user input)
            // Panic here indicates a bug in the engine
            value: CString::new(m.value)
                .expect("match value should not contain null bytes")
                .into_raw(),
            score: m.score,
            strategy: CString::new(m.strategy)
                .expect("strategy name should not contain null bytes")
                .into_raw(),
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

/// Free a RustMatchResultC and all contained strings.
///
/// # Safety
/// - `result` must be from `fuzzy_engine_find_matches`
/// - `result` must not be used after this call
#[no_mangle]
pub extern "C" fn fuzzy_match_result_free(result: RustMatchResultC) {
    // Free error message
    if !result.error_msg.is_null() {
        unsafe {
            let _ = CString::from_raw(result.error_msg);
        }
    }

    // Free match array and strings
    if !result.matches.is_null() && result.matches_len > 0 {
        let matches_slice =
            unsafe { slice::from_raw_parts_mut(result.matches, result.matches_len) };

        for m in matches_slice.iter() {
            if !m.value.is_null() {
                unsafe {
                    let _ = CString::from_raw(m.value);
                }
            }
            if !m.strategy.is_null() {
                unsafe {
                    let _ = CString::from_raw(m.strategy);
                }
            }
        }

        // Free the array itself
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

/// Get the current index hash for change detection.
///
/// # Returns
/// Null-terminated hash string. Caller must free with `fuzzy_string_free`.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn fuzzy_engine_get_hash(engine: *const FuzzyEngine) -> *mut c_char {
    if engine.is_null() {
        return ptr::null_mut();
    }

    let engine = unsafe { &*engine };
    CString::new(engine.get_index_hash())
        .unwrap_or_default()
        .into_raw()
}

/// Check if the engine index is ready (has vocabulary).
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn fuzzy_engine_is_ready(engine: *const FuzzyEngine) -> bool {
    if engine.is_null() {
        return false;
    }
    let engine = unsafe { &*engine };
    engine.is_ready()
}

/// Free a string returned by FFI functions.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn fuzzy_string_free(s: *mut c_char) {
    if !s.is_null() {
        unsafe {
            let _ = CString::from_raw(s);
        }
    }
}

// ============================================================================
// Helpers
// ============================================================================

/// Convert a C string array to Vec<String>
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

        // Build index
        let predicates = vec![
            CString::new("is_author_of").unwrap(),
            CString::new("works_at").unwrap(),
        ];
        let pred_ptrs: Vec<*const c_char> = predicates.iter().map(|s| s.as_ptr()).collect();

        let rebuild_result =
            fuzzy_engine_rebuild_index(engine, pred_ptrs.as_ptr(), pred_ptrs.len(), ptr::null(), 0);
        assert!(rebuild_result.success);
        fuzzy_rebuild_result_free(rebuild_result);

        // Find matches
        let query = CString::new("author").unwrap();
        let result = fuzzy_engine_find_matches(engine, query.as_ptr(), 0, 10, 0.5);

        assert!(result.success);
        assert!(result.matches_len > 0);

        fuzzy_match_result_free(result);
        fuzzy_engine_free(engine);
    }
}
