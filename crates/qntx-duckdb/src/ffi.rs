//! C-compatible FFI for the DuckDB attestation store.
//!
//! Mirrors the surface of `qntx-sqlite/src/ffi.rs` — same result types, same
//! ownership rules — so the Go side can share memory-management helpers.
//!
//! Ownership: `duckdb_storage_new` allocates on the Rust heap; caller must
//! `duckdb_storage_free`. Strings returned in result structs are owned by the
//! caller and must be freed with `duckdb_string_free`.

use std::os::raw::c_char;
use std::ptr;

use qntx_core::storage::AttestationStore;
use qntx_ffi_common::{cstr_to_str, cstring_new_or_empty, free_boxed, free_cstring, FfiResult};
use qntx_proto::proto_convert;

use crate::DuckdbStore;

// ============================================================================
// Result structs (shape-identical to qntx-sqlite/src/ffi.rs)
// ============================================================================

const MAX_ID_LENGTH: usize = 256;
const MAX_JSON_LENGTH: usize = 1_000_000;

#[repr(C)]
pub struct StorageResultC {
    pub success: bool,
    pub error_msg: *mut c_char,
}

#[repr(C)]
pub struct AttestationResultC {
    pub success: bool,
    pub error_msg: *mut c_char,
    pub attestation_json: *mut c_char,
}

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
// Store lifecycle
// ============================================================================

/// Open a DuckDB-backed store at the given location URL.
/// Returns NULL on failure (details go to stderr).
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_storage_new(location: *const c_char) -> *mut DuckdbStore {
    let loc = match unsafe { cstr_to_str(location) } {
        Ok(s) => s,
        Err(e) => {
            eprintln!("qntx-duckdb: invalid location string: {}", e);
            return ptr::null_mut();
        }
    };
    match DuckdbStore::open(loc) {
        Ok(store) => Box::into_raw(Box::new(store)),
        Err(e) => {
            eprintln!("qntx-duckdb: failed to open {}: {}", loc, e);
            ptr::null_mut()
        }
    }
}

#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_storage_free(store: *mut DuckdbStore) {
    unsafe { free_boxed(store) };
}

// ============================================================================
// CRUD
// ============================================================================

#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_storage_put(
    store: *mut DuckdbStore,
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
    let proto: qntx_proto::Attestation = match serde_json::from_str(json_str) {
        Ok(a) => a,
        Err(e) => return StorageResultC::error(&format!("failed to parse JSON: {}", e)),
    };
    let attestation = proto_convert::from_proto(proto);
    match store.put(attestation) {
        Ok(()) => StorageResultC::ok(),
        Err(e) => StorageResultC::error(&format!("{}", e)),
    }
}

#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_storage_get(
    store: *const DuckdbStore,
    id: *const c_char,
) -> AttestationResultC {
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
        Ok(Some(attestation)) => {
            let proto = proto_convert::to_proto(attestation);
            match serde_json::to_string(&proto) {
                Ok(json) => AttestationResultC::ok(json),
                Err(e) => AttestationResultC::error(&format!("failed to serialize: {}", e)),
            }
        }
        Ok(None) => AttestationResultC::not_found(),
        Err(e) => AttestationResultC::error(&format!("{}", e)),
    }
}

#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_storage_exists(
    store: *const DuckdbStore,
    id: *const c_char,
) -> StorageResultC {
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
        Ok(false) => StorageResultC {
            success: false,
            error_msg: ptr::null_mut(),
        },
        Err(e) => StorageResultC::error(&format!("{}", e)),
    }
}

#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_storage_delete(
    store: *mut DuckdbStore,
    id: *const c_char,
) -> StorageResultC {
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
pub extern "C" fn duckdb_storage_count(store: *const DuckdbStore) -> CountResultC {
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
pub extern "C" fn duckdb_storage_clear(store: *mut DuckdbStore) -> StorageResultC {
    if store.is_null() {
        return StorageResultC::error("null store pointer");
    }
    let store = unsafe { &mut *store };
    match store.clear() {
        Ok(()) => StorageResultC::ok(),
        Err(e) => StorageResultC::error(&format!("{}", e)),
    }
}

// ============================================================================
// Memory management
// ============================================================================

qntx_ffi_common::define_string_free!(duckdb_string_free);

#[no_mangle]
pub extern "C" fn duckdb_storage_result_free(result: StorageResultC) {
    unsafe { free_cstring(result.error_msg) };
}

#[no_mangle]
pub extern "C" fn duckdb_attestation_result_free(result: AttestationResultC) {
    unsafe {
        free_cstring(result.error_msg);
        free_cstring(result.attestation_json);
    }
}

#[no_mangle]
pub extern "C" fn duckdb_count_result_free(result: CountResultC) {
    unsafe { free_cstring(result.error_msg) };
}

// ============================================================================
// Utilities
// ============================================================================

qntx_ffi_common::define_version_fn!(duckdb_storage_version);
