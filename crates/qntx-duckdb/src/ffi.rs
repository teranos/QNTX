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

use crate::{DuckdbStore, QueryFilter};

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

/// Filter query. `filter_json` is the JSON serialization of `QueryFilter`
/// (see `lib.rs`). Returns a JSON array of attestations (empty array when
/// no rows match) via `AttestationResultC::attestation_json`; caller frees
/// with `duckdb_attestation_result_free`.
///
/// Same input/output shape as `qntx-sqlite`'s `storage_query`, so the Go
/// wrapper can build the same JSON for either backend.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_storage_query(
    store: *const DuckdbStore,
    filter_json: *const c_char,
) -> AttestationResultC {
    if store.is_null() {
        return AttestationResultC::error("null store pointer");
    }
    let json_str = match unsafe { cstr_to_str(filter_json) } {
        Ok(s) => s,
        Err(e) => return AttestationResultC::error(e),
    };
    if json_str.len() > MAX_JSON_LENGTH {
        return AttestationResultC::error("filter JSON exceeds maximum length");
    }
    let filter: QueryFilter = match serde_json::from_str(json_str) {
        Ok(f) => f,
        Err(e) => return AttestationResultC::error(&format!("failed to parse filter JSON: {}", e)),
    };
    let store = unsafe { &*store };
    let attestations = match store.query(&filter) {
        Ok(a) => a,
        Err(e) => return AttestationResultC::error(&format!("{}", e)),
    };
    let protos: Vec<qntx_proto::Attestation> = attestations
        .into_iter()
        .map(proto_convert::to_proto)
        .collect();
    match serde_json::to_string(&protos) {
        Ok(json) => AttestationResultC::ok(json),
        Err(e) => AttestationResultC::error(&format!("failed to serialize results: {}", e)),
    }
}

/// Flush the in-memory buffer to a new Parquet file under `<location>/attestations/`.
/// Called by Go on a fixed interval and at shutdown; also runs from Drop as
/// a safety net if the process exits without an explicit flush.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_storage_flush(store: *const DuckdbStore) -> StorageResultC {
    if store.is_null() {
        return StorageResultC::error("null store pointer");
    }
    let store = unsafe { &*store };
    match store.flush() {
        Ok(()) => StorageResultC::ok(),
        Err(e) => StorageResultC::error(&format!("{}", e)),
    }
}

// ============================================================================
// Access tokens (ADR-025)
// ============================================================================
//
// A separate handle from the attestation store: tokens are objects under
// `<location>/access_tokens/`, not rows in the in-memory DuckDB table, and
// they outlive any flush.
//
// `now_ms` is supplied by Go on every call that needs a clock. Rust never
// reads one, so the same inputs always produce the same result and a test can
// name the instant a token expired.

use crate::tokens::{TokenRecord, TokenStore};

#[repr(C)]
pub struct TokensResultC {
    pub success: bool,
    pub error_msg: *mut c_char,
    pub tokens_json: *mut c_char,
}

impl TokensResultC {
    fn ok(json: String) -> Self {
        Self {
            success: true,
            error_msg: ptr::null_mut(),
            tokens_json: cstring_new_or_empty(&json),
        }
    }
}

impl FfiResult for TokensResultC {
    const ERROR_FALLBACK: &'static str = "error message contains null";
    fn error_fields(error_msg: *mut c_char) -> Self {
        Self {
            success: false,
            error_msg,
            tokens_json: ptr::null_mut(),
        }
    }
}

/// Open the token store at the given location URL.
/// Returns NULL on failure (details go to stderr).
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_tokens_new(location: *const c_char) -> *mut TokenStore {
    let loc = match unsafe { cstr_to_str(location) } {
        Ok(s) => s,
        Err(e) => {
            eprintln!("qntx-duckdb: invalid token location string: {}", e);
            return ptr::null_mut();
        }
    };
    match TokenStore::open(loc) {
        Ok(store) => Box::into_raw(Box::new(store)),
        Err(e) => {
            eprintln!("qntx-duckdb: failed to open tokens at {}: {}", loc, e);
            ptr::null_mut()
        }
    }
}

#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_tokens_free(store: *mut TokenStore) {
    unsafe { free_boxed(store) };
}

/// Store a token. `record_json` is a `TokenRecord` — Go mints the raw token
/// and hashes it, so the raw value never crosses this boundary in either
/// direction.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_tokens_put(
    store: *mut TokenStore,
    record_json: *const c_char,
) -> StorageResultC {
    if store.is_null() {
        return StorageResultC::error("null token store pointer");
    }
    let json_str = match unsafe { cstr_to_str(record_json) } {
        Ok(s) => s,
        Err(e) => return StorageResultC::error(e),
    };
    if json_str.len() > MAX_JSON_LENGTH {
        return StorageResultC::error("token JSON exceeds maximum length");
    }
    let record: TokenRecord = match serde_json::from_str(json_str) {
        Ok(r) => r,
        Err(e) => return StorageResultC::error(&format!("failed to parse token JSON: {}", e)),
    };
    let store = unsafe { &mut *store };
    match store.put(record) {
        Ok(()) => StorageResultC::ok(),
        Err(e) => StorageResultC::error(&format!("{}", e)),
    }
}

/// Whether the token with this hash authorizes a request at `now_ms`.
///
/// `success` carries the answer and a false one is not an error — same shape
/// as `duckdb_storage_exists`. A caller must distinguish the two by checking
/// `error_msg`, because "this token is revoked" and "the store broke" have to
/// reach the middleware differently.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_tokens_lookup(
    store: *const TokenStore,
    hash: *const c_char,
    now_ms: i64,
) -> StorageResultC {
    if store.is_null() {
        return StorageResultC::error("null token store pointer");
    }
    let hash_str = match unsafe { cstr_to_str(hash) } {
        Ok(s) => s,
        Err(e) => return StorageResultC::error(e),
    };
    if hash_str.len() > MAX_ID_LENGTH {
        return StorageResultC::error("token hash exceeds maximum length");
    }
    let store = unsafe { &*store };
    if store.lookup(hash_str, now_ms) {
        StorageResultC::ok()
    } else {
        StorageResultC {
            success: false,
            error_msg: ptr::null_mut(),
        }
    }
}

/// Every token as JSON, hashes stripped. Caller frees with
/// `duckdb_tokens_result_free`.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_tokens_list(store: *const TokenStore) -> TokensResultC {
    if store.is_null() {
        return TokensResultC::error("null token store pointer");
    }
    let store = unsafe { &*store };
    match serde_json::to_string(&store.summaries()) {
        Ok(json) => TokensResultC::ok(json),
        Err(e) => TokensResultC::error(&format!("failed to serialize tokens: {}", e)),
    }
}

/// Revoke the token with this id. Unknown ids are an error rather than a
/// silent success — a revoke that matched nothing must not read as done.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_tokens_revoke(
    store: *mut TokenStore,
    id: *const c_char,
    now_ms: i64,
) -> StorageResultC {
    token_amend(store, id, "revoke", |store, id| store.revoke(id, now_ms))
}

/// Lift a revocation. Unknown ids are an error, same reasoning as revoke.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_tokens_enable(
    store: *mut TokenStore,
    id: *const c_char,
) -> StorageResultC {
    token_amend(store, id, "enable", |store, id| store.enable(id))
}

/// Record that the token with this hash was used at `now_ms`.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_tokens_touch(
    store: *mut TokenStore,
    hash: *const c_char,
    now_ms: i64,
) -> StorageResultC {
    token_amend(store, hash, "touch", |store, hash| {
        store.touch(hash, now_ms)
    })
}

/// Shared body for the operations that name one token and change it.
fn token_amend(
    store: *mut TokenStore,
    key: *const c_char,
    operation: &str,
    change: impl FnOnce(&mut TokenStore, &str) -> crate::error::Result<bool>,
) -> StorageResultC {
    if store.is_null() {
        return StorageResultC::error("null token store pointer");
    }
    let key_str = match unsafe { cstr_to_str(key) } {
        Ok(s) => s,
        Err(e) => return StorageResultC::error(e),
    };
    if key_str.len() > MAX_ID_LENGTH {
        return StorageResultC::error("token identifier exceeds maximum length");
    }
    let store = unsafe { &mut *store };
    match change(store, key_str) {
        Ok(true) => StorageResultC::ok(),
        Ok(false) => StorageResultC::error(&format!("no token matched {key_str} on {operation}")),
        Err(e) => StorageResultC::error(&format!("{}", e)),
    }
}

// ============================================================================
// Memory management
// ============================================================================

qntx_ffi_common::define_string_free!(duckdb_string_free);

#[no_mangle]
pub extern "C" fn duckdb_tokens_result_free(result: TokensResultC) {
    unsafe {
        free_cstring(result.error_msg);
        free_cstring(result.tokens_json);
    }
}

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
