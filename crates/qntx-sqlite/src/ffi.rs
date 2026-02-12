//! C-compatible FFI interface for SqliteStore
//!
//! Exposes qntx-sqlite through a C ABI for integration with Go via CGO.
//!
//! # Memory Ownership Rules
//!
//! - `storage_new()` allocates on Rust heap, caller owns pointer
//! - `storage_free()` must be called to deallocate
//! - String results are owned by caller and must be freed with `storage_string_free()`
//! - JSON strings passed to functions are copied, caller retains ownership

use std::os::raw::c_char;
use std::path::Path;
use std::ptr;

use qntx_core::storage::AttestationStore;
use qntx_core::Attestation;
use qntx_ffi_common::{
    cstr_to_str, cstring_new_or_empty, free_boxed, free_cstring, vec_into_raw, FfiResult,
};

use crate::SqliteStore;

// Safety limits
const MAX_ID_LENGTH: usize = 256;
const MAX_JSON_LENGTH: usize = 1_000_000; // 1MB

/// C-compatible result wrapper
#[repr(C)]
pub struct StorageResultC {
    pub success: bool,
    pub error_msg: *mut c_char,
}

/// C-compatible attestation result (for get operations)
#[repr(C)]
pub struct AttestationResultC {
    pub success: bool,
    pub error_msg: *mut c_char,
    pub attestation_json: *mut c_char, // NULL if not found
}

/// C-compatible string array result (for ids operation)
#[repr(C)]
pub struct StringArrayResultC {
    pub success: bool,
    pub error_msg: *mut c_char,
    pub strings: *mut *mut c_char,
    pub strings_len: usize,
}

/// C-compatible count result
#[repr(C)]
pub struct CountResultC {
    pub success: bool,
    pub error_msg: *mut c_char,
    pub count: usize,
}

impl StorageResultC {
    fn ok() -> Self {
        Self {
            success: true,
            error_msg: ptr::null_mut(),
        }
    }
}

impl FfiResult for StorageResultC {
    const ERROR_FALLBACK: &'static str = "error message contains null";

    fn error_fields(error_msg: *mut c_char) -> Self {
        Self {
            success: false,
            error_msg,
        }
    }
}

impl AttestationResultC {
    fn ok(json: String) -> Self {
        Self {
            success: true,
            error_msg: ptr::null_mut(),
            attestation_json: cstring_new_or_empty(&json),
        }
    }

    fn not_found() -> Self {
        Self {
            success: true,
            error_msg: ptr::null_mut(),
            attestation_json: ptr::null_mut(),
        }
    }
}

impl FfiResult for AttestationResultC {
    const ERROR_FALLBACK: &'static str = "error message contains null";

    fn error_fields(error_msg: *mut c_char) -> Self {
        Self {
            success: false,
            error_msg,
            attestation_json: ptr::null_mut(),
        }
    }
}

impl StringArrayResultC {
    fn ok(strings: Vec<String>) -> Self {
        let c_strings: Vec<*mut c_char> = strings
            .into_iter()
            .map(|s| cstring_new_or_empty(&s))
            .collect();

        let (ptr, len) = vec_into_raw(c_strings);

        Self {
            success: true,
            error_msg: ptr::null_mut(),
            strings: ptr,
            strings_len: len,
        }
    }
}

impl FfiResult for StringArrayResultC {
    const ERROR_FALLBACK: &'static str = "error message contains null";

    fn error_fields(error_msg: *mut c_char) -> Self {
        Self {
            success: false,
            error_msg,
            strings: ptr::null_mut(),
            strings_len: 0,
        }
    }
}

impl CountResultC {
    fn ok(count: usize) -> Self {
        Self {
            success: true,
            error_msg: ptr::null_mut(),
            count,
        }
    }
}

impl FfiResult for CountResultC {
    const ERROR_FALLBACK: &'static str = "error message contains null";

    fn error_fields(error_msg: *mut c_char) -> Self {
        Self {
            success: false,
            error_msg,
            count: 0,
        }
    }
}

// ============================================================================
// Store Lifecycle
// ============================================================================

#[no_mangle]
pub extern "C" fn storage_new_memory() -> *mut SqliteStore {
    match SqliteStore::in_memory() {
        Ok(store) => Box::into_raw(Box::new(store)),
        Err(_) => ptr::null_mut(),
    }
}

#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn storage_new_file(path: *const c_char) -> *mut SqliteStore {
    let path_str = match unsafe { cstr_to_str(path) } {
        Ok(s) => s,
        Err(_) => return ptr::null_mut(),
    };

    match SqliteStore::open(Path::new(path_str)) {
        Ok(store) => Box::into_raw(Box::new(store)),
        Err(_) => ptr::null_mut(),
    }
}

#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn storage_free(store: *mut SqliteStore) {
    unsafe { free_boxed(store) };
}

// ============================================================================
// CRUD Operations
// ============================================================================

#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn storage_put(
    store: *mut SqliteStore,
    attestation_json: *const c_char,
) -> StorageResultC {
    if store.is_null() {
        return StorageResultC::error("null store pointer");
    }

    let json_str = match unsafe { cstr_to_str(attestation_json) } {
        Ok(s) => s,
        Err(e) => return StorageResultC::error(e),
    };

    if json_str.len() > MAX_JSON_LENGTH {
        return StorageResultC::error("attestation JSON exceeds maximum length");
    }

    let store = unsafe { &mut *store };

    let attestation: Attestation = match serde_json::from_str(json_str) {
        Ok(a) => a,
        Err(e) => return StorageResultC::error(&format!("failed to parse JSON: {}", e)),
    };

    match store.put(attestation) {
        Ok(()) => StorageResultC::ok(),
        Err(e) => StorageResultC::error(&format!("{}", e)),
    }
}

#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn storage_get(store: *const SqliteStore, id: *const c_char) -> AttestationResultC {
    if store.is_null() {
        return AttestationResultC::error("null store pointer");
    }

    let id_str = match unsafe { cstr_to_str(id) } {
        Ok(s) => s,
        Err(e) => return AttestationResultC::error(e),
    };

    if id_str.len() > MAX_ID_LENGTH {
        return AttestationResultC::error("ID exceeds maximum length");
    }

    let store = unsafe { &*store };

    match store.get(id_str) {
        Ok(Some(attestation)) => match serde_json::to_string(&attestation) {
            Ok(json) => AttestationResultC::ok(json),
            Err(e) => AttestationResultC::error(&format!("failed to serialize: {}", e)),
        },
        Ok(None) => AttestationResultC::not_found(),
        Err(e) => AttestationResultC::error(&format!("{}", e)),
    }
}

#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn storage_exists(store: *const SqliteStore, id: *const c_char) -> StorageResultC {
    if store.is_null() {
        return StorageResultC::error("null store pointer");
    }

    let id_str = match unsafe { cstr_to_str(id) } {
        Ok(s) => s,
        Err(e) => return StorageResultC::error(e),
    };

    let store = unsafe { &*store };

    match store.exists(id_str) {
        Ok(true) => StorageResultC::ok(),
        Ok(false) => StorageResultC::error("not found"),
        Err(e) => StorageResultC::error(&format!("{}", e)),
    }
}

#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn storage_delete(store: *mut SqliteStore, id: *const c_char) -> StorageResultC {
    if store.is_null() {
        return StorageResultC::error("null store pointer");
    }

    let id_str = match unsafe { cstr_to_str(id) } {
        Ok(s) => s,
        Err(e) => return StorageResultC::error(e),
    };

    let store = unsafe { &mut *store };

    match store.delete(id_str) {
        Ok(true) => StorageResultC::ok(),
        Ok(false) => StorageResultC::error("not found"),
        Err(e) => StorageResultC::error(&format!("{}", e)),
    }
}

#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn storage_update(
    store: *mut SqliteStore,
    attestation_json: *const c_char,
) -> StorageResultC {
    if store.is_null() {
        return StorageResultC::error("null store pointer");
    }

    let json_str = match unsafe { cstr_to_str(attestation_json) } {
        Ok(s) => s,
        Err(e) => return StorageResultC::error(e),
    };

    let store = unsafe { &mut *store };

    let attestation: Attestation = match serde_json::from_str(json_str) {
        Ok(a) => a,
        Err(e) => return StorageResultC::error(&format!("failed to parse JSON: {}", e)),
    };

    match store.update(attestation) {
        Ok(()) => StorageResultC::ok(),
        Err(e) => StorageResultC::error(&format!("{}", e)),
    }
}

#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn storage_ids(store: *const SqliteStore) -> StringArrayResultC {
    if store.is_null() {
        return StringArrayResultC::error("null store pointer");
    }

    let store = unsafe { &*store };

    match store.ids() {
        Ok(ids) => StringArrayResultC::ok(ids),
        Err(e) => StringArrayResultC::error(&format!("{}", e)),
    }
}

#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn storage_count(store: *const SqliteStore) -> CountResultC {
    if store.is_null() {
        return CountResultC::error("null store pointer");
    }

    let store = unsafe { &*store };

    match store.count() {
        Ok(count) => CountResultC::ok(count),
        Err(e) => CountResultC::error(&format!("{}", e)),
    }
}

#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn storage_clear(store: *mut SqliteStore) -> StorageResultC {
    if store.is_null() {
        return StorageResultC::error("null store pointer");
    }

    let store = unsafe { &mut *store };

    match store.clear() {
        Ok(()) => StorageResultC::ok(),
        Err(e) => StorageResultC::error(&format!("{}", e)),
    }
}

/// Query attestations with filters (returns JSON array of matching attestations)
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn storage_query(
    store: *const SqliteStore,
    filter_json: *const c_char,
) -> AttestationResultC {
    if store.is_null() {
        return AttestationResultC::error("null store pointer");
    }

    let filter_str = match unsafe { cstr_to_str(filter_json) } {
        Ok(s) => s,
        Err(e) => return AttestationResultC::error(e),
    };

    if filter_str.len() > MAX_JSON_LENGTH {
        return AttestationResultC::error("filter JSON exceeds maximum length");
    }

    let store = unsafe { &*store };

    // Parse filter JSON
    let filter: qntx_core::AxFilter = match serde_json::from_str(filter_str) {
        Ok(f) => f,
        Err(e) => return AttestationResultC::error(&format!("invalid filter JSON: {}", e)),
    };

    // Query attestations
    use qntx_core::storage::QueryStore;
    let result = match store.query(&filter) {
        Ok(r) => r,
        Err(e) => return AttestationResultC::error(&format!("query failed: {}", e)),
    };

    // Convert attestations to JSON array
    match serde_json::to_string(&result.attestations) {
        Ok(json) => AttestationResultC::ok(json),
        Err(e) => AttestationResultC::error(&format!("failed to serialize results: {}", e)),
    }
}

// ============================================================================
// Memory Management
// ============================================================================

qntx_ffi_common::define_string_free!(storage_string_free);

#[no_mangle]
pub extern "C" fn storage_result_free(result: StorageResultC) {
    unsafe { free_cstring(result.error_msg) };
}

#[no_mangle]
pub extern "C" fn attestation_result_free(result: AttestationResultC) {
    unsafe {
        free_cstring(result.error_msg);
        free_cstring(result.attestation_json);
    }
}

#[no_mangle]
pub extern "C" fn string_array_result_free(result: StringArrayResultC) {
    unsafe {
        free_cstring(result.error_msg);
        qntx_ffi_common::free_cstring_array(result.strings, result.strings_len);
    }
}

#[no_mangle]
pub extern "C" fn count_result_free(result: CountResultC) {
    unsafe { free_cstring(result.error_msg) };
}

// ============================================================================
// Utilities
// ============================================================================

qntx_ffi_common::define_version_fn!(storage_version);

#[cfg(test)]
mod tests {
    use super::*;
    use std::ffi::CString;

    #[test]
    fn test_lifecycle() {
        let store = storage_new_memory();
        assert!(!store.is_null());
        storage_free(store);
    }

    #[test]
    fn test_put_and_get() {
        let store = storage_new_memory();
        assert!(!store.is_null());

        let json = r#"{"id":"AS-1","subjects":["ALICE"],"predicates":["knows"],"contexts":["work"],"actors":["human:bob"],"timestamp":1000,"source":"test","attributes":{},"created_at":1000}"#;
        let json_cstr = CString::new(json).unwrap();

        let put_result = storage_put(store, json_cstr.as_ptr());
        assert!(put_result.success);
        storage_result_free(put_result);

        let id_cstr = CString::new("AS-1").unwrap();
        let get_result = storage_get(store, id_cstr.as_ptr());
        assert!(get_result.success);
        assert!(!get_result.attestation_json.is_null());
        attestation_result_free(get_result);

        storage_free(store);
    }
}
