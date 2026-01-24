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

/// C-compatible attribute match result
#[repr(C)]
pub struct RustAttributeMatchC {
    /// The ID of the attestation/node (owned, must be freed)
    pub node_id: *mut c_char,
    /// The name of the matched field (owned, must be freed)
    pub field_name: *mut c_char,
    /// The full value of the matched field (owned, must be freed)
    pub field_value: *mut c_char,
    /// An excerpt showing the match in context (owned, must be freed)
    pub excerpt: *mut c_char,
    /// Match score 0.0-1.0
    pub score: f64,
    /// Matching strategy name (owned, must be freed)
    pub strategy: *mut c_char,
}

/// C-compatible result wrapper for find_attribute_matches
#[repr(C)]
pub struct RustAttributeMatchResultC {
    /// True if operation succeeded
    pub success: bool,
    /// Error message if success is false (owned, must be freed)
    pub error_msg: *mut c_char,
    /// Array of matches (owned, must be freed with fuzzy_attribute_result_free)
    pub matches: *mut RustAttributeMatchC,
    /// Number of matches in array
    pub matches_len: usize,
    /// Search time in microseconds
    pub search_time_us: u64,
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

impl RustAttributeMatchResultC {
    fn error(msg: &str) -> Self {
        Self {
            success: false,
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

/// Initialize the Rust logging system.
///
/// This should be called once at program startup before creating any engines.
/// The log level can be controlled via the RUST_LOG environment variable:
/// - RUST_LOG=qntx_fuzzy=debug
/// - RUST_LOG=qntx_fuzzy=trace
///
/// Safe to call multiple times (only initializes once).
#[no_mangle]
pub extern "C" fn fuzzy_init_logger() {
    crate::init_logger();
}

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
    // Initialize logging on first engine creation
    crate::init_logger();

    let engine = Box::new(FuzzyEngine::new());
    debug!("Created new FuzzyEngine at {:p}", &*engine);
    Box::into_raw(engine)
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
        // Handle null bytes gracefully - return error instead of panicking
        let value_cstr = match CString::new(m.value) {
            Ok(cs) => cs,
            Err(_) => return RustMatchResultC::error("match value contains null bytes"),
        };
        let strategy_cstr = match CString::new(m.strategy) {
            Ok(cs) => cs,
            Err(_) => return RustMatchResultC::error("strategy name contains null bytes"),
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

/// Search for matches in RichStringFields of attestations.
///
/// # Parameters
/// - `engine`: FuzzyEngine pointer from `fuzzy_engine_new`
/// - `query`: Null-terminated search query
/// - `attributes_json`: Null-terminated JSON string containing attributes
/// - `rich_string_fields`: Array of field names to search
/// - `rich_string_fields_len`: Number of field names
/// - `node_id`: Null-terminated ID of the attestation/node
///
/// # Safety
/// - All pointers must be valid and non-null
/// - Strings must be null-terminated UTF-8
/// - Result must be freed with `fuzzy_attribute_result_free`
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn fuzzy_engine_find_attribute_matches(
    engine: *const FuzzyEngine,
    query: *const c_char,
    attributes_json: *const c_char,
    rich_string_fields: *const *const c_char,
    rich_string_fields_len: usize,
    node_id: *const c_char,
) -> RustAttributeMatchResultC {
    // Validate engine
    if engine.is_null() {
        return RustAttributeMatchResultC::error("null engine");
    }
    let engine = unsafe { &*engine };

    // Validate and convert query
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

    // Validate and convert attributes JSON
    if attributes_json.is_null() {
        return RustAttributeMatchResultC::error("null attributes_json");
    }
    let attributes_str = match unsafe { CStr::from_ptr(attributes_json) }.to_str() {
        Ok(s) => s,
        Err(_) => return RustAttributeMatchResultC::error("invalid UTF-8 in attributes_json"),
    };

    // Validate and convert node_id
    if node_id.is_null() {
        return RustAttributeMatchResultC::error("null node_id");
    }
    let node_id_str = match unsafe { CStr::from_ptr(node_id) }.to_str() {
        Ok(s) => s,
        Err(_) => return RustAttributeMatchResultC::error("invalid UTF-8 in node_id"),
    };

    // Convert rich_string_fields array
    let fields = if rich_string_fields_len > 0 && !rich_string_fields.is_null() {
        match convert_string_array(rich_string_fields, rich_string_fields_len) {
            Ok(f) => f,
            Err(e) => return RustAttributeMatchResultC::error(&e),
        }
    } else {
        Vec::new()
    };

    // Perform search
    let start = std::time::Instant::now();
    let matches = engine.find_attribute_matches(query_str, attributes_str, &fields, node_id_str);
    let search_time_us = start.elapsed().as_micros() as u64;

    // Convert results to C format
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
            node_id: CString::new(m.node_id)
                .expect("node_id should not contain null bytes")
                .into_raw(),
            field_name: CString::new(m.field_name)
                .expect("field_name should not contain null bytes")
                .into_raw(),
            field_value: CString::new(m.field_value)
                .expect("field_value should not contain null bytes")
                .into_raw(),
            excerpt: CString::new(m.excerpt)
                .expect("excerpt should not contain null bytes")
                .into_raw(),
            score: m.score,
            strategy: CString::new(m.strategy)
                .expect("strategy name should not contain null bytes")
                .into_raw(),
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

/// Free a RustAttributeMatchResultC and all contained strings.
///
/// # Safety
/// - `result` must be from `fuzzy_engine_find_attribute_matches`
/// - `result` must not be used after this call
#[no_mangle]
pub extern "C" fn fuzzy_attribute_result_free(result: RustAttributeMatchResultC) {
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
            if !m.node_id.is_null() {
                unsafe {
                    let _ = CString::from_raw(m.node_id);
                }
            }
            if !m.field_name.is_null() {
                unsafe {
                    let _ = CString::from_raw(m.field_name);
                }
            }
            if !m.field_value.is_null() {
                unsafe {
                    let _ = CString::from_raw(m.field_value);
                }
            }
            if !m.excerpt.is_null() {
                unsafe {
                    let _ = CString::from_raw(m.excerpt);
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

/// Returns the library version string.
///
/// # Safety
///
/// The returned pointer is valid for the lifetime of the program.
/// Do not free this pointer - it points to static memory.
///
/// # Returns
///
/// A null-terminated C string containing the version (e.g., "0.1.0")
#[no_mangle]
pub extern "C" fn fuzzy_engine_version() -> *const c_char {
    // SAFETY: This is a compile-time constant string literal
    concat!(env!("CARGO_PKG_VERSION"), "\0").as_ptr() as *const c_char
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
        let predicates = [
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
