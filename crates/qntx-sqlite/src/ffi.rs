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

use std::ffi::{CStr, CString};
use std::os::raw::c_char;
use std::path::Path;
use std::ptr;

use qntx_core::storage::AttestationStore;
use qntx_core::Attestation;

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

    fn error(msg: &str) -> Self {
        Self {
            success: false,
            error_msg: CString::new(msg)
                .unwrap_or_else(|_| CString::new("error message contains null").unwrap())
                .into_raw(),
        }
    }
}

impl AttestationResultC {
    fn ok(json: String) -> Self {
        Self {
            success: true,
            error_msg: ptr::null_mut(),
            attestation_json: CString::new(json).unwrap_or_default().into_raw(),
        }
    }

    fn not_found() -> Self {
        Self {
            success: true,
            error_msg: ptr::null_mut(),
            attestation_json: ptr::null_mut(),
        }
    }

    fn error(msg: &str) -> Self {
        Self {
            success: false,
            error_msg: CString::new(msg)
                .unwrap_or_else(|_| CString::new("error message contains null").unwrap())
                .into_raw(),
            attestation_json: ptr::null_mut(),
        }
    }
}

impl StringArrayResultC {
    fn ok(strings: Vec<String>) -> Self {
        let c_strings: Vec<*mut c_char> = strings
            .into_iter()
            .map(|s| CString::new(s).unwrap_or_default().into_raw())
            .collect();

        let len = c_strings.len();
        let ptr = if len > 0 {
            Box::into_raw(c_strings.into_boxed_slice()) as *mut *mut c_char
        } else {
            ptr::null_mut()
        };

        Self {
            success: true,
            error_msg: ptr::null_mut(),
            strings: ptr,
            strings_len: len,
        }
    }

    fn error(msg: &str) -> Self {
        Self {
            success: false,
            error_msg: CString::new(msg)
                .unwrap_or_else(|_| CString::new("error message contains null").unwrap())
                .into_raw(),
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

    fn error(msg: &str) -> Self {
        Self {
            success: false,
            error_msg: CString::new(msg)
                .unwrap_or_else(|_| CString::new("error message contains null").unwrap())
                .into_raw(),
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
    if path.is_null() {
        return ptr::null_mut();
    }

    let path_str = match unsafe { CStr::from_ptr(path) }.to_str() {
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
    if !store.is_null() {
        unsafe {
            let _ = Box::from_raw(store);
        }
    }
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
    if attestation_json.is_null() {
        return StorageResultC::error("null attestation JSON");
    }

    let store = unsafe { &mut *store };

    let json_str = match unsafe { CStr::from_ptr(attestation_json) }.to_str() {
        Ok(s) => s,
        Err(_) => return StorageResultC::error("invalid UTF-8 in attestation JSON"),
    };

    if json_str.len() > MAX_JSON_LENGTH {
        return StorageResultC::error("attestation JSON exceeds maximum length");
    }

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
    if id.is_null() {
        return AttestationResultC::error("null ID");
    }

    let store = unsafe { &*store };

    let id_str = match unsafe { CStr::from_ptr(id) }.to_str() {
        Ok(s) => s,
        Err(_) => return AttestationResultC::error("invalid UTF-8 in ID"),
    };

    if id_str.len() > MAX_ID_LENGTH {
        return AttestationResultC::error("ID exceeds maximum length");
    }

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
    if id.is_null() {
        return StorageResultC::error("null ID");
    }

    let store = unsafe { &*store };

    let id_str = match unsafe { CStr::from_ptr(id) }.to_str() {
        Ok(s) => s,
        Err(_) => return StorageResultC::error("invalid UTF-8 in ID"),
    };

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
    if id.is_null() {
        return StorageResultC::error("null ID");
    }

    let store = unsafe { &mut *store };

    let id_str = match unsafe { CStr::from_ptr(id) }.to_str() {
        Ok(s) => s,
        Err(_) => return StorageResultC::error("invalid UTF-8 in ID"),
    };

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
    if attestation_json.is_null() {
        return StorageResultC::error("null attestation JSON");
    }

    let store = unsafe { &mut *store };

    let json_str = match unsafe { CStr::from_ptr(attestation_json) }.to_str() {
        Ok(s) => s,
        Err(_) => return StorageResultC::error("invalid UTF-8 in attestation JSON"),
    };

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
    if filter_json.is_null() {
        return AttestationResultC::error("null filter JSON");
    }

    let filter_str = match unsafe { CStr::from_ptr(filter_json) }.to_str() {
        Ok(s) => s,
        Err(_) => return AttestationResultC::error("invalid UTF-8 in filter JSON"),
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

#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn storage_string_free(s: *mut c_char) {
    if !s.is_null() {
        unsafe {
            let _ = CString::from_raw(s);
        }
    }
}

#[no_mangle]
pub extern "C" fn storage_result_free(result: StorageResultC) {
    if !result.error_msg.is_null() {
        unsafe {
            let _ = CString::from_raw(result.error_msg);
        }
    }
}

#[no_mangle]
pub extern "C" fn attestation_result_free(result: AttestationResultC) {
    if !result.error_msg.is_null() {
        unsafe {
            let _ = CString::from_raw(result.error_msg);
        }
    }
    if !result.attestation_json.is_null() {
        unsafe {
            let _ = CString::from_raw(result.attestation_json);
        }
    }
}

#[no_mangle]
pub extern "C" fn string_array_result_free(result: StringArrayResultC) {
    if !result.error_msg.is_null() {
        unsafe {
            let _ = CString::from_raw(result.error_msg);
        }
    }

    if !result.strings.is_null() && result.strings_len > 0 {
        let strings_slice = unsafe {
            std::slice::from_raw_parts_mut(result.strings, result.strings_len)
        };
        for s in strings_slice.iter() {
            if !s.is_null() {
                unsafe {
                    let _ = CString::from_raw(*s);
                }
            }
        }
        unsafe {
            let _ = Box::from_raw(std::ptr::slice_from_raw_parts_mut(
                result.strings,
                result.strings_len,
            ));
        }
    }
}

#[no_mangle]
pub extern "C" fn count_result_free(result: CountResultC) {
    if !result.error_msg.is_null() {
        unsafe {
            let _ = CString::from_raw(result.error_msg);
        }
    }
}

// ============================================================================
// Utilities
// ============================================================================

#[no_mangle]
pub extern "C" fn storage_version() -> *const c_char {
    concat!(env!("CARGO_PKG_VERSION"), "\0").as_ptr() as *const c_char
}

#[cfg(test)]
mod tests {
    use super::*;

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
