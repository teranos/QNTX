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
use rusqlite::OptionalExtension;
use qntx_ffi_common::{
    cstr_to_str, cstring_new_or_empty, free_boxed, free_cstring, vec_into_raw, FfiResult,
};
use qntx_proto::proto_convert;

use crate::store::ReadConn;
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
        Err(e) => {
            eprintln!("qntx-sqlite: failed to create in-memory store: {}", e);
            ptr::null_mut()
        }
    }
}

#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn storage_new_file(path: *const c_char) -> *mut SqliteStore {
    let path_str = match unsafe { cstr_to_str(path) } {
        Ok(s) => s,
        Err(e) => {
            eprintln!("qntx-sqlite: invalid path string: {}", e);
            return ptr::null_mut();
        }
    };

    match SqliteStore::open(Path::new(path_str)) {
        Ok(store) => Box::into_raw(Box::new(store)),
        Err(e) => {
            eprintln!("qntx-sqlite: failed to open {}: {}", path_str, e);
            ptr::null_mut()
        }
    }
}

#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn storage_free(store: *mut SqliteStore) {
    unsafe { free_boxed(store) };
}

// ============================================================================
// Read Connection (separate pointer, independent of SqliteStore)
// ============================================================================

/// Open a read-only connection from a file-backed store.
/// Returns NULL for in-memory stores or on failure.
/// The returned pointer is independent of the store — Go can access it
/// without creating overlapping Rust references.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn storage_open_read_conn(store: *const SqliteStore) -> *mut ReadConn {
    if store.is_null() {
        return ptr::null_mut();
    }
    let store = unsafe { &*store };
    match store.open_read_conn() {
        Ok(rc) => Box::into_raw(Box::new(rc)),
        Err(e) => {
            eprintln!("qntx-sqlite: failed to open read connection: {}", e);
            ptr::null_mut()
        }
    }
}

/// Free a read connection.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn read_conn_free(rc: *mut ReadConn) {
    unsafe { free_boxed(rc) };
}

/// Get an attestation by ID through the read connection.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn read_conn_get(rc: *const ReadConn, id: *const c_char) -> AttestationResultC {
    if rc.is_null() {
        return AttestationResultC::error("null read connection");
    }
    let id_str = match unsafe { cstr_to_str(id) } {
        Ok(s) => s,
        Err(e) => return AttestationResultC::error(e),
    };
    if id_str.len() > MAX_ID_LENGTH {
        return AttestationResultC::error("ID exceeds maximum length");
    }
    let rc = unsafe { &*rc };
    let mut stmt = match rc.conn.prepare(
        "SELECT id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at, signature, signer_did FROM attestations WHERE id = ?",
    ) {
        Ok(s) => s,
        Err(e) => return AttestationResultC::error(&format!("{}", e)),
    };
    let result = match stmt
        .query_row([id_str], |row| {
            Ok((
                row.get::<_, String>(0)?,
                row.get::<_, String>(1)?,
                row.get::<_, String>(2)?,
                row.get::<_, String>(3)?,
                row.get::<_, String>(4)?,
                row.get::<_, String>(5)?,
                row.get::<_, String>(6)?,
                row.get::<_, Option<String>>(7)?,
                row.get::<_, String>(8)?,
                row.get::<_, Option<Vec<u8>>>(9)?,
                row.get::<_, Option<String>>(10)?,
            ))
        })
        .optional()
    {
        Ok(r) => r,
        Err(e) => return AttestationResultC::error(&format!("{}", e)),
    };
    match result {
        None => AttestationResultC::not_found(),
        Some(row_data) => {
            match SqliteStore::row_to_attestation(row_data) {
                Ok(attestation) => {
                    let proto = proto_convert::to_proto(attestation);
                    match serde_json::to_string(&proto) {
                        Ok(json) => AttestationResultC::ok(json),
                        Err(e) => AttestationResultC::error(&format!("failed to serialize: {}", e)),
                    }
                }
                Err(e) => AttestationResultC::error(&format!("{}", e)),
            }
        }
    }
}

/// Check if an attestation exists through the read connection.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn read_conn_exists(rc: *const ReadConn, id: *const c_char) -> StorageResultC {
    if rc.is_null() {
        return StorageResultC::error("null read connection");
    }
    let id_str = match unsafe { cstr_to_str(id) } {
        Ok(s) => s,
        Err(e) => return StorageResultC::error(e),
    };
    let rc = unsafe { &*rc };
    match rc.conn.query_row(
        "SELECT 1 FROM attestations WHERE id = ?",
        [id_str],
        |_| Ok(()),
    ) {
        Ok(()) => StorageResultC::ok(),
        Err(rusqlite::Error::QueryReturnedNoRows) => StorageResultC {
            success: false,
            error_msg: ptr::null_mut(),
        },
        Err(e) => StorageResultC::error(&format!("{}", e)),
    }
}

/// Query attestations through the read connection.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn read_conn_query(
    rc: *const ReadConn,
    filter_json: *const c_char,
) -> AttestationResultC {
    if rc.is_null() {
        return AttestationResultC::error("null read connection");
    }
    let filter_str = match unsafe { cstr_to_str(filter_json) } {
        Ok(s) => s,
        Err(e) => return AttestationResultC::error(e),
    };
    if filter_str.len() > MAX_JSON_LENGTH {
        return AttestationResultC::error("filter JSON exceeds maximum length");
    }
    let rc = unsafe { &*rc };
    let filter: qntx_core::AxFilter = match serde_json::from_str(filter_str) {
        Ok(f) => f,
        Err(e) => return AttestationResultC::error(&format!("invalid filter JSON: {}", e)),
    };

    // Build the same query as QueryStore::query but using rc.conn
    use crate::store::build_query_sql;
    let (sql, params) = build_query_sql(&filter);

    let mut stmt = match rc.conn.prepare(&sql) {
        Ok(s) => s,
        Err(e) => return AttestationResultC::error(&format!("{}", e)),
    };

    let param_refs: Vec<&dyn rusqlite::ToSql> =
        params.iter().map(|p| p as &dyn rusqlite::ToSql).collect();

    let rows = match stmt
        .query_map(&param_refs[..], |row| {
            Ok((
                row.get::<_, String>(0)?,
                row.get::<_, String>(1)?,
                row.get::<_, String>(2)?,
                row.get::<_, String>(3)?,
                row.get::<_, String>(4)?,
                row.get::<_, String>(5)?,
                row.get::<_, String>(6)?,
                row.get::<_, Option<String>>(7)?,
                row.get::<_, String>(8)?,
                row.get::<_, Option<Vec<u8>>>(9)?,
                row.get::<_, Option<String>>(10)?,
            ))
        })
    {
        Ok(r) => r,
        Err(e) => return AttestationResultC::error(&format!("{}", e)),
    };

    let mut attestations = Vec::new();
    for row_result in rows {
        let row_data = match row_result {
            Ok(r) => r,
            Err(e) => return AttestationResultC::error(&format!("{}", e)),
        };
        match SqliteStore::row_to_attestation(row_data) {
            Ok(a) => attestations.push(a),
            Err(e) => return AttestationResultC::error(&format!("{}", e)),
        }
    }

    let proto_attestations: Vec<qntx_proto::Attestation> = attestations
        .into_iter()
        .map(proto_convert::to_proto)
        .collect();
    match serde_json::to_string(&proto_attestations) {
        Ok(json) => AttestationResultC::ok(json),
        Err(e) => AttestationResultC::error(&format!("failed to serialize results: {}", e)),
    }
}

/// Get all attestation IDs through the read connection.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn read_conn_ids(rc: *const ReadConn) -> StringArrayResultC {
    if rc.is_null() {
        return StringArrayResultC::error("null read connection");
    }
    let rc = unsafe { &*rc };
    match query_distinct(&rc.conn, "SELECT id FROM attestations ORDER BY created_at DESC") {
        Ok(ids) => StringArrayResultC::ok(ids),
        Err(e) => StringArrayResultC::error(&e),
    }
}

/// Get total count through the read connection.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn read_conn_count(rc: *const ReadConn) -> CountResultC {
    if rc.is_null() {
        return CountResultC::error("null read connection");
    }
    let rc = unsafe { &*rc };
    match rc.conn.query_row("SELECT COUNT(*) FROM attestations", [], |row| row.get::<_, usize>(0)) {
        Ok(count) => CountResultC::ok(count),
        Err(e) => CountResultC::error(&format!("{}", e)),
    }
}

/// Get distinct predicates through the read connection.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn read_conn_predicates(rc: *const ReadConn) -> StringArrayResultC {
    if rc.is_null() {
        return StringArrayResultC::error("null read connection");
    }
    let rc = unsafe { &*rc };
    match query_distinct(&rc.conn, "SELECT DISTINCT predicate FROM attestation_predicates ORDER BY predicate") {
        Ok(values) => StringArrayResultC::ok(values),
        Err(e) => StringArrayResultC::error(&e.to_string()),
    }
}

/// Get distinct contexts through the read connection.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn read_conn_contexts(rc: *const ReadConn) -> StringArrayResultC {
    if rc.is_null() {
        return StringArrayResultC::error("null read connection");
    }
    let rc = unsafe { &*rc };
    match query_distinct(&rc.conn, "SELECT DISTINCT context FROM attestation_contexts ORDER BY context") {
        Ok(values) => StringArrayResultC::ok(values),
        Err(e) => StringArrayResultC::error(&e.to_string()),
    }
}

/// Get storage stats through the read connection.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn read_conn_stats(rc: *const ReadConn) -> AttestationResultC {
    if rc.is_null() {
        return AttestationResultC::error("null read connection");
    }
    let rc = unsafe { &*rc };
    let count = |sql: &str| -> Result<usize, String> {
        rc.conn.query_row(sql, [], |row| row.get::<_, usize>(0))
            .map_err(|e| format!("{}", e))
    };
    let total = match count("SELECT COUNT(*) FROM attestations") {
        Ok(v) => v, Err(e) => return AttestationResultC::error(&e),
    };
    let subjects = match count("SELECT COUNT(DISTINCT subject) FROM attestation_subjects") {
        Ok(v) => v, Err(e) => return AttestationResultC::error(&e),
    };
    let predicates = match count("SELECT COUNT(DISTINCT predicate) FROM attestation_predicates") {
        Ok(v) => v, Err(e) => return AttestationResultC::error(&e),
    };
    let contexts = match count("SELECT COUNT(DISTINCT context) FROM attestation_contexts") {
        Ok(v) => v, Err(e) => return AttestationResultC::error(&e),
    };
    let actors = match count("SELECT COUNT(DISTINCT actor) FROM attestation_actors") {
        Ok(v) => v, Err(e) => return AttestationResultC::error(&e),
    };
    let json = format!(
        r#"{{"total_attestations":{},"unique_subjects":{},"unique_predicates":{},"unique_contexts":{},"unique_actors":{}}}"#,
        total, subjects, predicates, contexts, actors,
    );
    AttestationResultC::ok(json)
}

/// Execute a raw SQL query through the read connection.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn read_conn_query_raw(
    rc: *const ReadConn,
    sql: *const c_char,
    params_json: *const c_char,
) -> AttestationResultC {
    if rc.is_null() {
        return AttestationResultC::error("null read connection");
    }
    let sql_str = match unsafe { cstr_to_str(sql) } {
        Ok(s) => s,
        Err(e) => return AttestationResultC::error(e),
    };
    let params_str = match unsafe { cstr_to_str(params_json) } {
        Ok(s) => s,
        Err(e) => return AttestationResultC::error(e),
    };
    let sql_upper = sql_str.trim().to_uppercase();
    if !sql_upper.starts_with("SELECT") {
        return AttestationResultC::error("raw query must be a SELECT statement");
    }
    let rc = unsafe { &*rc };

    let params: Vec<serde_json::Value> = if params_str.is_empty() || params_str == "[]" {
        Vec::new()
    } else {
        match serde_json::from_str(params_str) {
            Ok(p) => p,
            Err(e) => return AttestationResultC::error(&format!("invalid params JSON: {}", e)),
        }
    };
    let mut stmt = match rc.conn.prepare(sql_str) {
        Ok(s) => s,
        Err(e) => return AttestationResultC::error(&format!("{}", e)),
    };
    let param_refs: Vec<Box<dyn rusqlite::types::ToSql>> = params
        .iter()
        .map(|v| -> Box<dyn rusqlite::types::ToSql> {
            match v {
                serde_json::Value::String(s) => Box::new(s.clone()),
                serde_json::Value::Number(n) => {
                    if let Some(i) = n.as_i64() { Box::new(i) }
                    else if let Some(f) = n.as_f64() { Box::new(f) }
                    else { Box::new(n.to_string()) }
                }
                serde_json::Value::Bool(b) => Box::new(*b),
                serde_json::Value::Null => Box::new(rusqlite::types::Null),
                _ => Box::new(v.to_string()),
            }
        })
        .collect();
    let param_slice: Vec<&dyn rusqlite::types::ToSql> =
        param_refs.iter().map(|p| p.as_ref()).collect();

    let rows = match stmt.query_map(param_slice.as_slice(), |row| {
        Ok((
            row.get::<_, String>(0)?,
            row.get::<_, String>(1)?,
            row.get::<_, String>(2)?,
            row.get::<_, String>(3)?,
            row.get::<_, String>(4)?,
            row.get::<_, String>(5)?,
            row.get::<_, String>(6)?,
            row.get::<_, Option<String>>(7)?,
            row.get::<_, String>(8)?,
            row.get::<_, Option<Vec<u8>>>(9)?,
            row.get::<_, Option<String>>(10)?,
        ))
    }) {
        Ok(r) => r,
        Err(e) => return AttestationResultC::error(&format!("{}", e)),
    };
    let mut attestations = Vec::new();
    for row_result in rows {
        let row_data = match row_result {
            Ok(r) => r,
            Err(e) => return AttestationResultC::error(&format!("{}", e)),
        };
        match SqliteStore::row_to_attestation(row_data) {
            Ok(a) => attestations.push(a),
            Err(e) => return AttestationResultC::error(&format!("{}", e)),
        }
    }
    let protos: Vec<qntx_proto::Attestation> = attestations
        .into_iter()
        .map(proto_convert::to_proto)
        .collect();
    match serde_json::to_string(&protos) {
        Ok(json) => AttestationResultC::ok(json),
        Err(e) => AttestationResultC::error(&format!("failed to serialize results: {}", e)),
    }
}

/// Run PRAGMA integrity_check through the read connection.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn read_conn_integrity_check(rc: *const ReadConn) -> StringArrayResultC {
    if rc.is_null() {
        return StringArrayResultC::error("null read connection");
    }
    let rc = unsafe { &*rc };
    match query_distinct(&rc.conn, "PRAGMA integrity_check") {
        Ok(lines) => StringArrayResultC::ok(lines),
        Err(e) => StringArrayResultC::error(&format!("integrity check failed: {}", e)),
    }
}

/// Helper: query a single-column result set.
fn query_distinct(conn: &rusqlite::Connection, sql: &str) -> Result<Vec<String>, String> {
    let mut stmt = conn.prepare(sql).map_err(|e| format!("{}", e))?;
    let rows = stmt
        .query_map([], |row| row.get::<_, String>(0))
        .map_err(|e| format!("{}", e))?;
    let mut results = Vec::new();
    for row in rows {
        results.push(row.map_err(|e| format!("{}", e))?);
    }
    Ok(results)
}

/// Set enforcement config on the store. When set, enforcement runs after every put().
/// config_json: `{"actor_context_limit":16,"actor_contexts_limit":64,"entity_actors_limit":64}`
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn storage_set_enforcement_config(
    store: *mut SqliteStore,
    config_json: *const c_char,
) -> StorageResultC {
    if store.is_null() {
        return StorageResultC::error("null store pointer");
    }

    let json_str = match unsafe { cstr_to_str(config_json) } {
        Ok(s) => s,
        Err(e) => return StorageResultC::error(e),
    };

    let store = unsafe { &mut *store };

    let config: qntx_core::storage::enforcement::EnforcementConfig =
        match serde_json::from_str(json_str) {
            Ok(c) => c,
            Err(e) => {
                return StorageResultC::error(&format!("invalid enforcement config JSON: {}", e))
            }
        };

    store.set_enforcement_config(config);
    StorageResultC::ok()
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
        Ok(false) => StorageResultC {
            success: false,
            error_msg: ptr::null_mut(),
        },
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

    let proto: qntx_proto::Attestation = match serde_json::from_str(json_str) {
        Ok(a) => a,
        Err(e) => return StorageResultC::error(&format!("failed to parse JSON: {}", e)),
    };
    let attestation = proto_convert::from_proto(proto);

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

    // Convert attestations to proto types and serialize
    let proto_attestations: Vec<qntx_proto::Attestation> = result
        .attestations
        .into_iter()
        .map(proto_convert::to_proto)
        .collect();
    match serde_json::to_string(&proto_attestations) {
        Ok(json) => AttestationResultC::ok(json),
        Err(e) => AttestationResultC::error(&format!("failed to serialize results: {}", e)),
    }
}

// ============================================================================
// Enforcement & Stats
// ============================================================================

/// Enforce storage limits and log events. Returns JSON array of enforcement events.
///
/// Input JSON: `{"actors":["a"],"contexts":["c"],"subjects":["s"],"config":{...}}`
/// Output JSON: `[{"event_type":"...","actor":"...","deleted_count":N,...},...]`
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn storage_enforce_limits(
    store: *mut SqliteStore,
    input_json: *const c_char,
) -> AttestationResultC {
    if store.is_null() {
        return AttestationResultC::error("null store pointer");
    }

    let json_str = match unsafe { cstr_to_str(input_json) } {
        Ok(s) => s,
        Err(e) => return AttestationResultC::error(e),
    };

    if json_str.len() > MAX_JSON_LENGTH {
        return AttestationResultC::error("enforcement input JSON exceeds maximum length");
    }

    let store = unsafe { &mut *store };

    let input: qntx_core::storage::EnforcementInput = match serde_json::from_str(json_str) {
        Ok(i) => i,
        Err(e) => {
            return AttestationResultC::error(&format!("invalid enforcement input JSON: {}", e))
        }
    };

    match store.enforce_limits(&input) {
        Ok(events) => match serde_json::to_string(&events) {
            Ok(json) => AttestationResultC::ok(json),
            Err(e) => {
                AttestationResultC::error(&format!("failed to serialize enforcement events: {}", e))
            }
        },
        Err(e) => AttestationResultC::error(&format!("enforcement failed: {}", e)),
    }
}

/// Get storage statistics. Returns JSON with counts.
///
/// Output JSON: `{"total_attestations":N,"unique_subjects":N,...}`
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn storage_get_stats(store: *const SqliteStore) -> AttestationResultC {
    if store.is_null() {
        return AttestationResultC::error("null store pointer");
    }

    let store = unsafe { &*store };

    use qntx_core::storage::QueryStore;
    match store.stats() {
        Ok(stats) => {
            // Serialize StorageStats — it doesn't derive Serialize so do it manually
            let json = format!(
                r#"{{"total_attestations":{},"unique_subjects":{},"unique_predicates":{},"unique_contexts":{},"unique_actors":{}}}"#,
                stats.total_attestations,
                stats.unique_subjects,
                stats.unique_predicates,
                stats.unique_contexts,
                stats.unique_actors,
            );
            AttestationResultC::ok(json)
        }
        Err(e) => AttestationResultC::error(&format!("failed to get stats: {}", e)),
    }
}

// ============================================================================
// Distinct Value Queries
// ============================================================================

/// Get all distinct predicates.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn storage_predicates(store: *const SqliteStore) -> StringArrayResultC {
    if store.is_null() {
        return StringArrayResultC::error("null store pointer");
    }
    let store = unsafe { &*store };
    use qntx_core::storage::QueryStore;
    match store.predicates() {
        Ok(values) => StringArrayResultC::ok(values),
        Err(e) => StringArrayResultC::error(&format!("{}", e)),
    }
}

/// Get all distinct contexts.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn storage_contexts(store: *const SqliteStore) -> StringArrayResultC {
    if store.is_null() {
        return StringArrayResultC::error("null store pointer");
    }
    let store = unsafe { &*store };
    use qntx_core::storage::QueryStore;
    match store.contexts() {
        Ok(values) => StringArrayResultC::ok(values),
        Err(e) => StringArrayResultC::error(&format!("{}", e)),
    }
}

// ============================================================================
// Raw Query (Go query builder → Rust connection)
// ============================================================================

/// Execute a raw SQL query against attestations through Rust's connection.
///
/// Go keeps its query builder logic; Rust just executes the SQL.
/// The query MUST select standard attestation columns in order.
/// `params_json` is a JSON array of bind parameters, e.g. `["value1", 42]`.
/// Returns JSON array of matching attestations.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn storage_query_raw(
    store: *const SqliteStore,
    sql: *const c_char,
    params_json: *const c_char,
) -> AttestationResultC {
    if store.is_null() {
        return AttestationResultC::error("null store pointer");
    }

    let sql_str = match unsafe { cstr_to_str(sql) } {
        Ok(s) => s,
        Err(e) => return AttestationResultC::error(e),
    };

    let params_str = match unsafe { cstr_to_str(params_json) } {
        Ok(s) => s,
        Err(e) => return AttestationResultC::error(e),
    };

    // Safety: reject writes through the raw query path
    let sql_upper = sql_str.trim().to_uppercase();
    if !sql_upper.starts_with("SELECT") {
        return AttestationResultC::error("raw query must be a SELECT statement");
    }

    let store = unsafe { &*store };

    match store.query_attestations_raw(sql_str, params_str) {
        Ok(attestations) => {
            let protos: Vec<qntx_proto::Attestation> = attestations
                .into_iter()
                .map(proto_convert::to_proto)
                .collect();
            match serde_json::to_string(&protos) {
                Ok(json) => AttestationResultC::ok(json),
                Err(e) => AttestationResultC::error(&format!("failed to serialize results: {}", e)),
            }
        }
        Err(e) => AttestationResultC::error(&format!("raw query failed: {}", e)),
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
// Integrity
// ============================================================================

/// Run PRAGMA integrity_check and return result lines.
/// A healthy database returns a single-element array: ["ok"].
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn storage_integrity_check(store: *const SqliteStore) -> StringArrayResultC {
    let store = unsafe {
        match store.as_ref() {
            Some(s) => s,
            None => return StringArrayResultC::error("null store pointer"),
        }
    };
    match store.integrity_check() {
        Ok(lines) => StringArrayResultC::ok(lines),
        Err(e) => StringArrayResultC::error(&format!("integrity check failed: {}", e)),
    }
}

// ============================================================================
// Backup
// ============================================================================

/// Create a hot backup of the database to the given path.
/// Safe to call while the database is in use.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn storage_backup(
    store: *const SqliteStore,
    dest_path: *const c_char,
) -> StorageResultC {
    let store = unsafe {
        match store.as_ref() {
            Some(s) => s,
            None => return StorageResultC::error("null store pointer"),
        }
    };
    let dest = match unsafe { cstr_to_str(dest_path) } {
        Ok(s) => s,
        Err(e) => return StorageResultC::error(e),
    };
    match store.backup(dest) {
        Ok(()) => StorageResultC::ok(),
        Err(e) => StorageResultC::error(&format!("backup failed: {}", e)),
    }
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
