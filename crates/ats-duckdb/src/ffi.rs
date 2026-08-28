//! C-compatible FFI for the DuckDB attestation store.
//!
//! Mirrors the surface of `ats-sqlite/src/ffi.rs` — same result types, same
//! ownership rules — so the Go side can share memory-management helpers.
//!
//! Ownership: `duckdb_storage_new` allocates on the Rust heap; caller must
//! `duckdb_storage_free`. Strings returned in result structs are owned by the
//! caller and must be freed with `duckdb_string_free`.

use std::os::raw::c_char;
use std::ptr;

use ats::storage::AttestationStore;
use qntx_ffi_common::{
    cstr_to_str, cstring_new_or_empty, cstring_new_or_fallback, free_boxed, free_cstring, FfiResult,
};
use qntx_proto::proto_convert;

use crate::{DuckdbStore, QueryFilter};

// ============================================================================
// Result structs (shape-identical to ats-sqlite/src/ffi.rs)
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

/// Write the reason a constructor is returning null into the caller's slot.

/// A constructor answers with a pointer, so the cause has nowhere to ride
/// except an out-parameter. Printing it to stderr left the caller holding a
/// null and writing its own guess about why.
fn fail_with<T>(error_out: *mut *mut c_char, said: &str) -> *mut T {
    if !error_out.is_null() {
        unsafe { *error_out = cstring_new_or_fallback(said, "the reason could not be encoded") };
    }
    ptr::null_mut()
}

/// Open a DuckDB-backed store at the given location URL.
/// Returns NULL on failure, writing the reason into `error_out`.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_storage_new(
    location: *const c_char,
    namespace: *const c_char,
    error_out: *mut *mut c_char,
) -> *mut DuckdbStore {
    let loc = match unsafe { cstr_to_str(location) } {
        Ok(s) => s,
        Err(e) => return fail_with(error_out, &format!("invalid location string: {e}")),
    };
    let ns = match unsafe { cstr_to_str(namespace) } {
        Ok(s) => s,
        Err(e) => return fail_with(error_out, &format!("invalid namespace string: {e}")),
    };
    match DuckdbStore::open(loc, ns) {
        Ok(store) => Box::into_raw(Box::new(store)),
        Err(e) => fail_with(error_out, &format!("failed to open {loc} for {ns}: {e}")),
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
/// Same input/output shape as `ats-sqlite`'s `storage_query`, so the Go
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

/// Resolve many ids at once, in `duckdb_storage_query`'s array shape.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_storage_get_many(
    store: *const DuckdbStore,
    ids_json: *const c_char,
) -> AttestationResultC {
    if store.is_null() {
        return AttestationResultC::error("null store pointer");
    }
    let json_str = match unsafe { cstr_to_str(ids_json) } {
        Ok(s) => s,
        Err(e) => return AttestationResultC::error(e),
    };
    if json_str.len() > MAX_JSON_LENGTH {
        return AttestationResultC::error("id list JSON exceeds maximum length");
    }
    let ids: Vec<String> = match serde_json::from_str(json_str) {
        Ok(v) => v,
        Err(e) => {
            return AttestationResultC::error(&format!("failed to parse id list JSON: {}", e))
        }
    };
    if ids.iter().any(|id| id.len() > MAX_ID_LENGTH) {
        return AttestationResultC::error("ID exceeds maximum length");
    }
    let store = unsafe { &*store };
    // Ids not held come back absent, so the caller matches on id, not order.
    let attestations = match store.get_many(&ids) {
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

use crate::namespace_store::{NamespaceStore, Owner};
use crate::tokens::{TokenRecord, TokenStore};
use crate::users::{UserRecord, UserStore};

#[repr(C)]
pub struct NamespacesResultC {
    pub success: bool,
    pub error_msg: *mut c_char,
    pub namespaces_json: *mut c_char,
}

impl NamespacesResultC {
    fn ok(json: String) -> Self {
        Self {
            success: true,
            error_msg: ptr::null_mut(),
            namespaces_json: cstring_new_or_empty(&json),
        }
    }
}

impl FfiResult for NamespacesResultC {
    const ERROR_FALLBACK: &'static str = "error message contains null";
    fn error_fields(error_msg: *mut c_char) -> Self {
        Self {
            success: false,
            error_msg,
            namespaces_json: ptr::null_mut(),
        }
    }
}

/// Open namespace management at a storage location.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_namespaces_new(
    location: *const c_char,
    error_out: *mut *mut c_char,
) -> *mut NamespaceStore {
    let loc = match unsafe { cstr_to_str(location) } {
        Ok(s) => s,
        Err(e) => {
            return fail_with(
                error_out,
                &format!("invalid namespace location string: {e}"),
            )
        }
    };
    match NamespaceStore::open(loc) {
        Ok(store) => Box::into_raw(Box::new(store)),
        Err(e) => fail_with(
            error_out,
            &format!("failed to open namespaces at {loc}: {e}"),
        ),
    }
}

#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_namespaces_free(store: *mut NamespaceStore) {
    unsafe { free_boxed(store) };
}

/// Every namespace as JSON. Caller frees with `duckdb_namespaces_result_free`.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_namespaces_list(store: *const NamespaceStore) -> NamespacesResultC {
    if store.is_null() {
        return NamespacesResultC::error("null namespace store pointer");
    }
    let store = unsafe { &*store };
    let found = match store.list() {
        Ok(found) => found,
        Err(e) => return NamespacesResultC::error(&format!("failed to list namespaces: {}", e)),
    };
    match serde_json::to_string(&found) {
        Ok(json) => NamespacesResultC::ok(json),
        Err(e) => NamespacesResultC::error(&format!("failed to serialize namespaces: {}", e)),
    }
}

/// Create `name` by recording who owns it. `owner_json` is an `Owner`.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_namespaces_create(
    store: *const NamespaceStore,
    name: *const c_char,
    owner_json: *const c_char,
) -> StorageResultC {
    if store.is_null() {
        return StorageResultC::error("null namespace store pointer");
    }
    let name = match unsafe { cstr_to_str(name) } {
        Ok(s) => s,
        Err(e) => return StorageResultC::error(&format!("invalid namespace name: {}", e)),
    };
    let json = match unsafe { cstr_to_str(owner_json) } {
        Ok(s) => s,
        Err(e) => return StorageResultC::error(&format!("invalid owner json: {}", e)),
    };
    let owner: Owner = match serde_json::from_str(json) {
        Ok(owner) => owner,
        Err(e) => return StorageResultC::error(&format!("failed to parse owner json: {}", e)),
    };
    match unsafe { &*store }.create(name, &owner) {
        Ok(()) => StorageResultC::ok(),
        Err(e) => StorageResultC::error(&format!("failed to create namespace {}: {}", name, e)),
    }
}

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

#[repr(C)]
pub struct UsersResultC {
    pub success: bool,
    pub error_msg: *mut c_char,
    pub users_json: *mut c_char,
}

impl UsersResultC {
    fn ok(json: String) -> Self {
        Self {
            success: true,
            error_msg: ptr::null_mut(),
            users_json: cstring_new_or_empty(&json),
        }
    }
}

impl FfiResult for UsersResultC {
    const ERROR_FALLBACK: &'static str = "error message contains null";
    fn error_fields(error_msg: *mut c_char) -> Self {
        Self {
            success: false,
            error_msg,
            users_json: ptr::null_mut(),
        }
    }
}

/// Open the User store, which lives in the system namespace (ADR-031).
///
/// A null return writes the reason into `error_out`, which the caller owns and
/// frees with `duckdb_string_free`. Printing it here instead left Go holding a
/// null pointer and inventing a summary for a cause it never received.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_users_new(
    location: *const c_char,
    error_out: *mut *mut c_char,
) -> *mut UserStore {
    let loc = match unsafe { cstr_to_str(location) } {
        Ok(s) => s,
        Err(e) => return fail_with(error_out, &format!("invalid user location string: {e}")),
    };
    match UserStore::open(loc) {
        Ok(store) => Box::into_raw(Box::new(store)),
        Err(e) => fail_with(error_out, &format!("failed to open users at {loc}: {e}")),
    }
}

#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_users_free(store: *mut UserStore) {
    unsafe { free_boxed(store) };
}

/// Write a User whole, so its keys and accounts can never disagree.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_users_put(
    store: *mut UserStore,
    record_json: *const c_char,
) -> StorageResultC {
    if store.is_null() {
        return StorageResultC::error("null user store pointer");
    }
    let json_str = match unsafe { cstr_to_str(record_json) } {
        Ok(s) => s,
        Err(e) => return StorageResultC::error(e),
    };
    if json_str.len() > MAX_JSON_LENGTH {
        return StorageResultC::error("user JSON exceeds maximum length");
    }
    let record: UserRecord = match serde_json::from_str(json_str) {
        Ok(r) => r,
        Err(e) => return StorageResultC::error(&format!("failed to parse user JSON: {}", e)),
    };
    let store = unsafe { &*store };
    match store.put(&record) {
        Ok(()) => StorageResultC::ok(),
        Err(e) => StorageResultC::error(&format!("{}", e)),
    }
}

/// The User a route reaches as `UserRecord` JSON, or JSON null for none.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_users_by_route(
    store: *const UserStore,
    route: *const c_char,
) -> UsersResultC {
    if store.is_null() {
        return UsersResultC::error("null user store pointer");
    }
    let route_str = match unsafe { cstr_to_str(route) } {
        Ok(s) => s,
        Err(e) => return UsersResultC::error(e),
    };
    if route_str.len() > MAX_JSON_LENGTH {
        return UsersResultC::error("route exceeds maximum length");
    }
    let store = unsafe { &*store };
    let found = match store.by_route(route_str) {
        Ok(found) => found,
        Err(e) => return UsersResultC::error(&format!("{}", e)),
    };
    match serde_json::to_string(&found) {
        Ok(json) => UsersResultC::ok(json),
        Err(e) => UsersResultC::error(&format!("failed to serialize the User: {}", e)),
    }
}

/// Every User as JSON. How many there are is what decides ROOT.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_users_list(store: *const UserStore) -> UsersResultC {
    if store.is_null() {
        return UsersResultC::error("null user store pointer");
    }
    let store = unsafe { &*store };
    let users = match store.all() {
        Ok(users) => users,
        Err(e) => return UsersResultC::error(&format!("{}", e)),
    };
    match serde_json::to_string(&users) {
        Ok(json) => UsersResultC::ok(json),
        Err(e) => UsersResultC::error(&format!("failed to serialize users: {}", e)),
    }
}

/// Open the token store. A record names the namespace it authorizes.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_tokens_new(
    location: *const c_char,
    error_out: *mut *mut c_char,
) -> *mut TokenStore {
    let loc = match unsafe { cstr_to_str(location) } {
        Ok(s) => s,
        Err(e) => return fail_with(error_out, &format!("invalid token location string: {e}")),
    };
    match TokenStore::open(loc) {
        Ok(store) => Box::into_raw(Box::new(store)),
        Err(e) => fail_with(error_out, &format!("failed to open tokens at {loc}: {e}")),
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

/// The live token this hash names as `TokenSummary` JSON, `null` if none.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_tokens_resolve(
    store: *const TokenStore,
    hash: *const c_char,
    now_ms: i64,
) -> TokensResultC {
    if store.is_null() {
        return TokensResultC::error("null token store pointer");
    }
    let hash_str = match unsafe { cstr_to_str(hash) } {
        Ok(s) => s,
        Err(e) => return TokensResultC::error(e),
    };
    if hash_str.len() > MAX_ID_LENGTH {
        return TokensResultC::error("token hash exceeds maximum length");
    }
    let store = unsafe { &*store };
    let resolved = store
        .resolve(hash_str, now_ms)
        .map(crate::tokens::TokenSummary::from);
    match serde_json::to_string(&resolved) {
        Ok(json) => TokensResultC::ok(json),
        Err(e) => TokensResultC::error(&format!("failed to serialize the token: {}", e)),
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

// Watchers: two prefixes behind one handle.

use crate::watchers::{WatcherRecord, WatcherStore};

#[repr(C)]
pub struct WatchersResultC {
    pub success: bool,
    pub error_msg: *mut c_char,
    pub watchers_json: *mut c_char,
}

impl WatchersResultC {
    fn ok(json: String) -> Self {
        Self {
            success: true,
            error_msg: ptr::null_mut(),
            watchers_json: cstring_new_or_empty(&json),
        }
    }
}

impl FfiResult for WatchersResultC {
    const ERROR_FALLBACK: &'static str = "error message contains null";
    fn error_fields(error_msg: *mut c_char) -> Self {
        Self {
            success: false,
            error_msg,
            watchers_json: ptr::null_mut(),
        }
    }
}

/// Open the watcher store at `location`. NULL on failure, reason in `error_out`.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_watchers_new(
    location: *const c_char,
    namespace: *const c_char,
    error_out: *mut *mut c_char,
) -> *mut WatcherStore {
    let loc = match unsafe { cstr_to_str(location) } {
        Ok(s) => s,
        Err(e) => return fail_with(error_out, &format!("invalid watcher location string: {e}")),
    };
    let ns = match unsafe { cstr_to_str(namespace) } {
        Ok(s) => s,
        Err(e) => return fail_with(error_out, &format!("invalid watcher namespace string: {e}")),
    };
    match WatcherStore::open(loc, ns) {
        Ok(store) => Box::into_raw(Box::new(store)),
        Err(e) => fail_with(
            error_out,
            &format!("failed to open watchers at {loc} for {ns}: {e}"),
        ),
    }
}

/// Flushes before closing, so events the last tick buffered are not lost.
///
/// Returns null when they were written, and the reason when they were not —
/// which is fires that happened and are now gone. The store is freed either
/// way; the caller frees the message with `duckdb_string_free`.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_watchers_free(store: *mut WatcherStore) -> *mut c_char {
    let mut said = ptr::null_mut();
    if !store.is_null() {
        if let Err(e) = unsafe { (*store).flush() } {
            said = cstring_new_or_fallback(
                &format!("failed to flush watcher fires on close: {e}"),
                "watcher fires were lost on close and the reason could not be encoded",
            );
        }
    }
    unsafe { free_boxed(store) };
    said
}

/// Declare a watcher; returns when its object is durable.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_watchers_put(
    store: *mut WatcherStore,
    record_json: *const c_char,
) -> StorageResultC {
    if store.is_null() {
        return StorageResultC::error("null watcher store pointer");
    }
    let json_str = match unsafe { cstr_to_str(record_json) } {
        Ok(s) => s,
        Err(e) => return StorageResultC::error(e),
    };
    if json_str.len() > MAX_JSON_LENGTH {
        return StorageResultC::error("watcher JSON exceeds maximum length");
    }
    let record: WatcherRecord = match serde_json::from_str(json_str) {
        Ok(r) => r,
        Err(e) => return StorageResultC::error(&format!("failed to parse watcher JSON: {}", e)),
    };
    let store = unsafe { &mut *store };
    match store.put(record) {
        Ok(()) => StorageResultC::ok(),
        Err(e) => StorageResultC::error(&format!("{}", e)),
    }
}

/// Every declaration as a JSON array; free with `duckdb_watchers_result_free`.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_watchers_list(store: *const WatcherStore) -> WatchersResultC {
    if store.is_null() {
        return WatchersResultC::error("null watcher store pointer");
    }
    let store = unsafe { &*store };
    match serde_json::to_string(&store.list()) {
        Ok(json) => WatchersResultC::ok(json),
        Err(e) => WatchersResultC::error(&format!("failed to serialize watchers: {}", e)),
    }
}

/// The last `limit` fires of one watcher as JSON, newest first.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_watchers_recent_fires(
    store: *const WatcherStore,
    id: *const c_char,
    limit: i64,
) -> WatchersResultC {
    if store.is_null() {
        return WatchersResultC::error("null watcher store pointer");
    }
    let id_str = match unsafe { cstr_to_str(id) } {
        Ok(s) => s,
        Err(e) => return WatchersResultC::error(e),
    };
    let store = unsafe { &*store };
    let found = match store.recent_fires(id_str, limit.max(0) as usize) {
        Ok(found) => found,
        Err(e) => return WatchersResultC::error(&format!("failed to read recent fires: {e}")),
    };
    match serde_json::to_string(&found) {
        Ok(json) => WatchersResultC::ok(json),
        Err(e) => WatchersResultC::error(&format!("failed to serialize recent fires: {e}")),
    }
}

/// Withdraw a declaration. An id matching nothing is an error.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_watchers_delete(
    store: *mut WatcherStore,
    id: *const c_char,
) -> StorageResultC {
    if store.is_null() {
        return StorageResultC::error("null watcher store pointer");
    }
    let id_str = match unsafe { cstr_to_str(id) } {
        Ok(s) => s,
        Err(e) => return StorageResultC::error(e),
    };
    let store = unsafe { &mut *store };
    match store.delete(id_str) {
        Ok(true) => StorageResultC::ok(),
        Ok(false) => StorageResultC::error(&format!("watcher {} not found", id_str)),
        Err(e) => StorageResultC::error(&format!("{}", e)),
    }
}

/// Note a fire. Returns without reaching storage — that is the point.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_watchers_record_fire(
    store: *mut WatcherStore,
    id: *const c_char,
    at_ms: i64,
    attestation_id: *const c_char,
) -> StorageResultC {
    if store.is_null() {
        return StorageResultC::error("null watcher store pointer");
    }
    let id_str = match unsafe { cstr_to_str(id) } {
        Ok(s) => s,
        Err(e) => return StorageResultC::error(e),
    };
    // A schedule run passes an empty string: it has no attestation behind it.
    let as_id = match unsafe { cstr_to_str(attestation_id) } {
        Ok(s) => s,
        Err(e) => return StorageResultC::error(e),
    };
    unsafe { &mut *store }.record_fire(id_str, at_ms, as_id);
    StorageResultC::ok()
}

/// Note an error against a watcher. Buffered like a fire.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_watchers_record_error(
    store: *mut WatcherStore,
    id: *const c_char,
    at_ms: i64,
    message: *const c_char,
    attestation_id: *const c_char,
) -> StorageResultC {
    if store.is_null() {
        return StorageResultC::error("null watcher store pointer");
    }
    let id_str = match unsafe { cstr_to_str(id) } {
        Ok(s) => s,
        Err(e) => return StorageResultC::error(e),
    };
    let msg = match unsafe { cstr_to_str(message) } {
        Ok(s) => s,
        Err(e) => return StorageResultC::error(e),
    };
    let as_id = match unsafe { cstr_to_str(attestation_id) } {
        Ok(s) => s,
        Err(e) => return StorageResultC::error(e),
    };
    unsafe { &mut *store }.record_error(id_str, at_ms, msg, as_id);
    StorageResultC::ok()
}

/// Write the buffered events as one file. Go decides how often.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_watchers_flush(store: *mut WatcherStore) -> StorageResultC {
    if store.is_null() {
        return StorageResultC::error("null watcher store pointer");
    }
    match unsafe { &mut *store }.flush() {
        Ok(()) => StorageResultC::ok(),
        Err(e) => StorageResultC::error(&format!("{}", e)),
    }
}

/// The counters for one watcher. Never having fired is a zero, not an error.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_watchers_tally(
    store: *const WatcherStore,
    id: *const c_char,
) -> WatchersResultC {
    if store.is_null() {
        return WatchersResultC::error("null watcher store pointer");
    }
    let id_str = match unsafe { cstr_to_str(id) } {
        Ok(s) => s,
        Err(e) => return WatchersResultC::error(e),
    };
    let store = unsafe { &*store };
    match serde_json::to_string(&store.tally(id_str)) {
        Ok(json) => WatchersResultC::ok(json),
        Err(e) => WatchersResultC::error(&format!("failed to serialize tally: {}", e)),
    }
}

// Schedules: a cold declaration prefix and a hot tick prefix, one handle.

use crate::schedules::ScheduleStore;
use qntx_proto::ScheduleDeclaration;

#[repr(C)]
pub struct SchedulesResultC {
    pub success: bool,
    pub error_msg: *mut c_char,
    pub schedules_json: *mut c_char,
}

impl SchedulesResultC {
    fn ok(json: String) -> Self {
        Self {
            success: true,
            error_msg: ptr::null_mut(),
            schedules_json: cstring_new_or_empty(&json),
        }
    }
}

impl FfiResult for SchedulesResultC {
    const ERROR_FALLBACK: &'static str = "error message contains null";
    fn error_fields(error_msg: *mut c_char) -> Self {
        Self {
            success: false,
            error_msg,
            schedules_json: ptr::null_mut(),
        }
    }
}

/// Open the schedule store at `location`. NULL on failure, reason in `error_out`.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_schedules_new(
    location: *const c_char,
    namespace: *const c_char,
    error_out: *mut *mut c_char,
) -> *mut ScheduleStore {
    let loc = match unsafe { cstr_to_str(location) } {
        Ok(s) => s,
        Err(e) => return fail_with(error_out, &format!("invalid schedule location string: {e}")),
    };
    let ns = match unsafe { cstr_to_str(namespace) } {
        Ok(s) => s,
        Err(e) => {
            return fail_with(
                error_out,
                &format!("invalid schedule namespace string: {e}"),
            )
        }
    };
    match ScheduleStore::open(loc, ns) {
        Ok(store) => Box::into_raw(Box::new(store)),
        Err(e) => fail_with(
            error_out,
            &format!("failed to open schedules at {loc} for {ns}: {e}"),
        ),
    }
}

/// Flushes before closing, so ticks the last run buffered are not lost.
///
/// Returns null when they were written, and the reason when they were not —
/// ticks that happened and are now gone, which is a schedule that will run
/// again believing it never ran.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_schedules_free(store: *mut ScheduleStore) -> *mut c_char {
    let mut said = ptr::null_mut();
    if !store.is_null() {
        if let Err(e) = unsafe { (*store).flush() } {
            said = cstring_new_or_fallback(
                &format!("failed to flush schedule ticks on close: {e}"),
                "schedule ticks were lost on close and the reason could not be encoded",
            );
        }
    }
    unsafe { free_boxed(store) };
    said
}

/// Declare a schedule; returns when its object is durable.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_schedules_put(
    store: *mut ScheduleStore,
    declaration_json: *const c_char,
) -> StorageResultC {
    if store.is_null() {
        return StorageResultC::error("null schedule store pointer");
    }
    let json_str = match unsafe { cstr_to_str(declaration_json) } {
        Ok(s) => s,
        Err(e) => return StorageResultC::error(e),
    };
    if json_str.len() > MAX_JSON_LENGTH {
        return StorageResultC::error("schedule JSON exceeds maximum length");
    }
    let declaration: ScheduleDeclaration = match serde_json::from_str(json_str) {
        Ok(d) => d,
        Err(e) => return StorageResultC::error(&format!("failed to parse schedule JSON: {}", e)),
    };
    match unsafe { &mut *store }.put(declaration) {
        Ok(()) => StorageResultC::ok(),
        Err(e) => StorageResultC::error(&format!("{}", e)),
    }
}

/// Every declaration as a JSON array; free with `duckdb_schedules_result_free`.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_schedules_list(store: *const ScheduleStore) -> SchedulesResultC {
    if store.is_null() {
        return SchedulesResultC::error("null schedule store pointer");
    }
    match serde_json::to_string(&unsafe { &*store }.list()) {
        Ok(json) => SchedulesResultC::ok(json),
        Err(e) => SchedulesResultC::error(&format!("failed to serialize schedules: {}", e)),
    }
}

/// What is owed at `now_ms`, as a JSON array of declarations.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_schedules_due(
    store: *const ScheduleStore,
    now_ms: i64,
) -> SchedulesResultC {
    if store.is_null() {
        return SchedulesResultC::error("null schedule store pointer");
    }
    match serde_json::to_string(&unsafe { &*store }.due(now_ms)) {
        Ok(json) => SchedulesResultC::ok(json),
        Err(e) => SchedulesResultC::error(&format!("failed to serialize due schedules: {}", e)),
    }
}

/// The soonest run owed, or an empty array when nothing is scheduled.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_schedules_next(store: *const ScheduleStore) -> SchedulesResultC {
    if store.is_null() {
        return SchedulesResultC::error("null schedule store pointer");
    }
    let next: Vec<ScheduleDeclaration> = unsafe { &*store }.next_scheduled().into_iter().collect();
    match serde_json::to_string(&next) {
        Ok(json) => SchedulesResultC::ok(json),
        Err(e) => SchedulesResultC::error(&format!("failed to serialize next schedule: {}", e)),
    }
}

/// Withdraw a declaration. An id matching nothing is an error.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_schedules_delete(
    store: *mut ScheduleStore,
    id: *const c_char,
) -> StorageResultC {
    if store.is_null() {
        return StorageResultC::error("null schedule store pointer");
    }
    let id_str = match unsafe { cstr_to_str(id) } {
        Ok(s) => s,
        Err(e) => return StorageResultC::error(e),
    };
    match unsafe { &mut *store }.delete(id_str) {
        Ok(true) => StorageResultC::ok(),
        Ok(false) => StorageResultC::error(&format!("schedule {} not found", id_str)),
        Err(e) => StorageResultC::error(&format!("{}", e)),
    }
}

/// Note a run. Buffered until flush, like a watcher fire.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_schedules_record_run(
    store: *mut ScheduleStore,
    id: *const c_char,
    at_ms: i64,
    execution_id: *const c_char,
    next_run_at_ms: i64,
) -> StorageResultC {
    if store.is_null() {
        return StorageResultC::error("null schedule store pointer");
    }
    let id_str = match unsafe { cstr_to_str(id) } {
        Ok(s) => s,
        Err(e) => return StorageResultC::error(e),
    };
    let exec_str = match unsafe { cstr_to_str(execution_id) } {
        Ok(s) => s,
        Err(e) => return StorageResultC::error(e),
    };
    unsafe { &mut *store }.record_run(id_str, at_ms, exec_str, next_run_at_ms);
    StorageResultC::ok()
}

/// Move the next run without a run having happened.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_schedules_reschedule(
    store: *mut ScheduleStore,
    id: *const c_char,
    at_ms: i64,
    next_run_at_ms: i64,
) -> StorageResultC {
    if store.is_null() {
        return StorageResultC::error("null schedule store pointer");
    }
    let id_str = match unsafe { cstr_to_str(id) } {
        Ok(s) => s,
        Err(e) => return StorageResultC::error(e),
    };
    unsafe { &mut *store }.reschedule(id_str, at_ms, next_run_at_ms);
    StorageResultC::ok()
}

/// Write the buffered ticks.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_schedules_flush(store: *mut ScheduleStore) -> StorageResultC {
    if store.is_null() {
        return StorageResultC::error("null schedule store pointer");
    }
    match unsafe { &mut *store }.flush() {
        Ok(()) => StorageResultC::ok(),
        Err(e) => StorageResultC::error(&format!("{}", e)),
    }
}

/// What the ticks derive for one schedule. Never having run is a zero.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_schedules_progress(
    store: *const ScheduleStore,
    id: *const c_char,
) -> SchedulesResultC {
    if store.is_null() {
        return SchedulesResultC::error("null schedule store pointer");
    }
    let id_str = match unsafe { cstr_to_str(id) } {
        Ok(s) => s,
        Err(e) => return SchedulesResultC::error(e),
    };
    match serde_json::to_string(&unsafe { &*store }.progress(id_str)) {
        Ok(json) => SchedulesResultC::ok(json),
        Err(e) => SchedulesResultC::error(&format!("failed to serialize progress: {}", e)),
    }
}

qntx_ffi_common::define_string_free!(duckdb_string_free);

#[no_mangle]
pub extern "C" fn duckdb_schedules_result_free(result: SchedulesResultC) {
    unsafe {
        free_cstring(result.error_msg);
        free_cstring(result.schedules_json);
    }
}

#[no_mangle]
pub extern "C" fn duckdb_watchers_result_free(result: WatchersResultC) {
    unsafe {
        free_cstring(result.error_msg);
        free_cstring(result.watchers_json);
    }
}

#[no_mangle]
pub extern "C" fn duckdb_tokens_result_free(result: TokensResultC) {
    unsafe {
        free_cstring(result.error_msg);
        free_cstring(result.tokens_json);
    }
}

#[no_mangle]
pub extern "C" fn duckdb_users_result_free(result: UsersResultC) {
    unsafe {
        free_cstring(result.error_msg);
        free_cstring(result.users_json);
    }
}

#[no_mangle]
pub extern "C" fn duckdb_namespaces_result_free(result: NamespacesResultC) {
    unsafe {
        free_cstring(result.error_msg);
        free_cstring(result.namespaces_json);
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

use crate::nodeidentity::{IdentityRecord, IdentityStore};

/// Open the system namespace's identity store. NULL on failure, details to stderr.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_identity_new(
    location: *const c_char,
    error_out: *mut *mut c_char,
) -> *mut IdentityStore {
    let loc = match unsafe { cstr_to_str(location) } {
        Ok(s) => s,
        Err(e) => return fail_with(error_out, &format!("invalid identity location string: {e}")),
    };
    match IdentityStore::open(loc) {
        Ok(store) => Box::into_raw(Box::new(store)),
        Err(e) => fail_with(
            error_out,
            &format!("failed to open node identity at {loc}: {e}"),
        ),
    }
}

#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_identity_free(store: *mut IdentityStore) {
    unsafe { free_boxed(store) };
}

/// The identity as JSON, empty when there is none — first boot, not an error.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_identity_load(store: *const IdentityStore) -> TokensResultC {
    if store.is_null() {
        return TokensResultC::error("null identity store pointer");
    }
    let store = unsafe { &*store };
    match store.current() {
        Some(record) => match serde_json::to_string(record) {
            Ok(json) => TokensResultC::ok(json),
            Err(e) => TokensResultC::error(&format!("failed to encode node identity: {}", e)),
        },
        None => TokensResultC::ok(String::new()),
    }
}

/// Write the node's identity. `record_json` is an `IdentityRecord`.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn duckdb_identity_save(
    store: *mut IdentityStore,
    record_json: *const c_char,
) -> StorageResultC {
    if store.is_null() {
        return StorageResultC::error("null identity store pointer");
    }
    let json_str = match unsafe { cstr_to_str(record_json) } {
        Ok(s) => s,
        Err(e) => return StorageResultC::error(e),
    };
    if json_str.len() > MAX_JSON_LENGTH {
        return StorageResultC::error("node identity JSON exceeds maximum length");
    }
    let record: IdentityRecord = match serde_json::from_str(json_str) {
        Ok(r) => r,
        Err(e) => {
            return StorageResultC::error(&format!("failed to parse node identity JSON: {}", e))
        }
    };
    let store = unsafe { &mut *store };
    match store.save(record) {
        Ok(()) => StorageResultC::ok(),
        Err(e) => StorageResultC::error(&format!("{}", e)),
    }
}
