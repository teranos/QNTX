//! Generic SQL execution FFI for Go's database/sql/driver
//!
//! Exposes `sql_exec`, `sql_query`, `sql_begin`, `sql_commit`, `sql_rollback`
//! so Go can route all SQL through Rust's single SQLite connection.
//!
//! Results are serialized as JSON — no streaming needed since Go's side
//! pre-fetches all rows anyway (database/sql/driver.Rows).

use std::os::raw::c_char;
use std::ptr;

use rusqlite::types::ValueRef;

use qntx_ffi_common::{cstr_to_str, cstring_new_or_empty, free_cstring, FfiResult};

use crate::store::ReadConn;
use crate::SqliteStore;

const MAX_SQL_LENGTH: usize = 1_000_000; // 1MB
const MAX_PARAMS_LENGTH: usize = 1_000_000; // 1MB

// ============================================================================
// Result types
// ============================================================================

/// Result for exec operations (INSERT, UPDATE, DELETE, DDL)
#[repr(C)]
pub struct ExecResultC {
    pub success: bool,
    pub error_msg: *mut c_char,
    pub last_insert_id: i64,
    pub rows_affected: i64,
}

impl ExecResultC {
    fn ok(last_insert_id: i64, rows_affected: i64) -> Self {
        Self {
            success: true,
            error_msg: ptr::null_mut(),
            last_insert_id,
            rows_affected,
        }
    }
}

impl FfiResult for ExecResultC {
    const ERROR_FALLBACK: &'static str = "exec error contains null";

    fn error_fields(error_msg: *mut c_char) -> Self {
        Self {
            success: false,
            error_msg,
            last_insert_id: 0,
            rows_affected: 0,
        }
    }
}

/// Result for query operations (SELECT)
#[repr(C)]
pub struct QueryResultC {
    pub success: bool,
    pub error_msg: *mut c_char,
    pub columns_json: *mut c_char, // JSON array of column names
    pub rows_json: *mut c_char,    // JSON array of row arrays
}

impl QueryResultC {
    fn ok(columns: String, rows: String) -> Self {
        Self {
            success: true,
            error_msg: ptr::null_mut(),
            columns_json: cstring_new_or_empty(&columns),
            rows_json: cstring_new_or_empty(&rows),
        }
    }
}

impl FfiResult for QueryResultC {
    const ERROR_FALLBACK: &'static str = "query error contains null";

    fn error_fields(error_msg: *mut c_char) -> Self {
        Self {
            success: false,
            error_msg,
            columns_json: ptr::null_mut(),
            rows_json: ptr::null_mut(),
        }
    }
}

// ============================================================================
// Shared helpers
// ============================================================================

/// Parse JSON params into rusqlite-compatible boxed values.
/// Extracted from store.rs:164-183 for reuse.
fn parse_params(params_json: &str) -> Result<Vec<Box<dyn rusqlite::types::ToSql>>, String> {
    let params: Vec<serde_json::Value> = if params_json.is_empty() || params_json == "[]" {
        Vec::new()
    } else {
        serde_json::from_str(params_json).map_err(|e| format!("invalid params JSON: {}", e))?
    };

    Ok(params
        .iter()
        .map(|v| -> Box<dyn rusqlite::types::ToSql> {
            match v {
                serde_json::Value::String(s) => Box::new(s.clone()),
                serde_json::Value::Number(n) => {
                    if let Some(i) = n.as_i64() {
                        Box::new(i)
                    } else if let Some(f) = n.as_f64() {
                        Box::new(f)
                    } else {
                        Box::new(n.to_string())
                    }
                }
                serde_json::Value::Bool(b) => Box::new(*b),
                serde_json::Value::Null => Box::new(rusqlite::types::Null),
                // Decode {"$blob": "base64data"} back to raw bytes
                serde_json::Value::Object(map) => {
                    if let Some(serde_json::Value::String(b64)) = map.get("$blob") {
                        use base64::Engine;
                        match base64::engine::general_purpose::STANDARD.decode(b64) {
                            Ok(bytes) => Box::new(bytes),
                            Err(_) => Box::new(v.to_string()),
                        }
                    } else {
                        Box::new(v.to_string())
                    }
                }
                _ => Box::new(v.to_string()),
            }
        })
        .collect())
}

// ============================================================================
// FFI functions
// ============================================================================

/// Execute a non-query SQL statement (INSERT, UPDATE, DELETE, DDL).
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn sql_exec(
    store: *mut SqliteStore,
    sql: *const c_char,
    params_json: *const c_char,
) -> ExecResultC {
    if store.is_null() {
        return ExecResultC::error("null store pointer");
    }

    let sql_str = match unsafe { cstr_to_str(sql) } {
        Ok(s) => s,
        Err(e) => return ExecResultC::error(e),
    };
    if sql_str.len() > MAX_SQL_LENGTH {
        return ExecResultC::error("SQL exceeds maximum length");
    }

    let params_str = match unsafe { cstr_to_str(params_json) } {
        Ok(s) => s,
        Err(e) => return ExecResultC::error(e),
    };
    if params_str.len() > MAX_PARAMS_LENGTH {
        return ExecResultC::error("params JSON exceeds maximum length");
    }

    let store = unsafe { &mut *store };

    let param_refs = match parse_params(params_str) {
        Ok(p) => p,
        Err(e) => return ExecResultC::error(&e),
    };
    let param_slice: Vec<&dyn rusqlite::types::ToSql> =
        param_refs.iter().map(|p| p.as_ref()).collect();

    match store.conn.execute(sql_str, param_slice.as_slice()) {
        Ok(rows_affected) => {
            let last_insert_id = store.conn.last_insert_rowid();
            ExecResultC::ok(last_insert_id, rows_affected as i64)
        }
        Err(e) => ExecResultC::error(&format!("{}", e)),
    }
}

/// Execute a query SQL statement (SELECT) and return all rows as JSON.
///
/// columns_json: `["col1","col2",...]`
/// rows_json: `[[val1,val2,...],...]`
///
/// Values are typed: null, integer, float, string, or base64-encoded blob.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn sql_query(
    store: *const SqliteStore,
    sql: *const c_char,
    params_json: *const c_char,
) -> QueryResultC {
    if store.is_null() {
        return QueryResultC::error("null store pointer");
    }

    let sql_str = match unsafe { cstr_to_str(sql) } {
        Ok(s) => s,
        Err(e) => return QueryResultC::error(e),
    };
    if sql_str.len() > MAX_SQL_LENGTH {
        return QueryResultC::error("SQL exceeds maximum length");
    }

    let params_str = match unsafe { cstr_to_str(params_json) } {
        Ok(s) => s,
        Err(e) => return QueryResultC::error(e),
    };
    if params_str.len() > MAX_PARAMS_LENGTH {
        return QueryResultC::error("params JSON exceeds maximum length");
    }

    let store = unsafe { &*store };

    let param_refs = match parse_params(params_str) {
        Ok(p) => p,
        Err(e) => return QueryResultC::error(&e),
    };
    let param_slice: Vec<&dyn rusqlite::types::ToSql> =
        param_refs.iter().map(|p| p.as_ref()).collect();

    let mut stmt = match store.conn.prepare(sql_str) {
        Ok(s) => s,
        Err(e) => return QueryResultC::error(&format!("{}", e)),
    };

    // Extract column names
    let columns: Vec<String> = stmt.column_names().iter().map(|c| c.to_string()).collect();

    let columns_json = match serde_json::to_string(&columns) {
        Ok(j) => j,
        Err(e) => return QueryResultC::error(&format!("failed to serialize columns: {}", e)),
    };

    // Execute and collect rows
    let column_count = columns.len();
    let rows_result = stmt.query_map(param_slice.as_slice(), |row| {
        let mut values = Vec::with_capacity(column_count);
        for i in 0..column_count {
            let val = match row.get_ref(i)? {
                ValueRef::Null => serde_json::Value::Null,
                ValueRef::Integer(i) => serde_json::Value::Number(i.into()),
                ValueRef::Real(f) => serde_json::Value::Number(
                    serde_json::Number::from_f64(f).unwrap_or_else(|| 0i64.into()),
                ),
                ValueRef::Text(t) => {
                    let s = String::from_utf8_lossy(t).into_owned();
                    serde_json::Value::String(s)
                }
                ValueRef::Blob(b) => {
                    use base64::Engine;
                    let encoded = base64::engine::general_purpose::STANDARD.encode(b);
                    // Wrap in object to distinguish from regular strings
                    let mut map = serde_json::Map::new();
                    map.insert("$blob".to_string(), serde_json::Value::String(encoded));
                    serde_json::Value::Object(map)
                }
            };
            values.push(val);
        }
        Ok(serde_json::Value::Array(values))
    });

    let rows_iter = match rows_result {
        Ok(r) => r,
        Err(e) => return QueryResultC::error(&format!("{}", e)),
    };

    let mut all_rows = Vec::new();
    for row_result in rows_iter {
        match row_result {
            Ok(row) => all_rows.push(row),
            Err(e) => return QueryResultC::error(&format!("row iteration failed: {}", e)),
        }
    }

    let rows_json = match serde_json::to_string(&all_rows) {
        Ok(j) => j,
        Err(e) => return QueryResultC::error(&format!("failed to serialize rows: {}", e)),
    };

    QueryResultC::ok(columns_json, rows_json)
}

/// Begin an immediate transaction.
///
/// Uses raw `BEGIN IMMEDIATE` SQL instead of rusqlite::Transaction
/// to avoid borrow issues — Go's mutex serializes access.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn sql_begin(store: *mut SqliteStore) -> ExecResultC {
    if store.is_null() {
        return ExecResultC::error("null store pointer");
    }
    let store = unsafe { &mut *store };
    match store.conn.execute_batch("BEGIN IMMEDIATE") {
        Ok(()) => ExecResultC::ok(0, 0),
        Err(e) => ExecResultC::error(&format!("{}", e)),
    }
}

/// Commit the current transaction.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn sql_commit(store: *mut SqliteStore) -> ExecResultC {
    if store.is_null() {
        return ExecResultC::error("null store pointer");
    }
    let store = unsafe { &mut *store };
    match store.conn.execute_batch("COMMIT") {
        Ok(()) => ExecResultC::ok(0, 0),
        Err(e) => ExecResultC::error(&format!("{}", e)),
    }
}

/// Rollback the current transaction.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn sql_rollback(store: *mut SqliteStore) -> ExecResultC {
    if store.is_null() {
        return ExecResultC::error("null store pointer");
    }
    let store = unsafe { &mut *store };
    match store.conn.execute_batch("ROLLBACK") {
        Ok(()) => ExecResultC::ok(0, 0),
        Err(e) => ExecResultC::error(&format!("{}", e)),
    }
}

/// Execute a query SQL statement through the read connection.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn read_conn_sql_query(
    rc: *const ReadConn,
    sql: *const c_char,
    params_json: *const c_char,
) -> QueryResultC {
    if rc.is_null() {
        return QueryResultC::error("null read connection");
    }

    let sql_str = match unsafe { cstr_to_str(sql) } {
        Ok(s) => s,
        Err(e) => return QueryResultC::error(e),
    };
    if sql_str.len() > MAX_SQL_LENGTH {
        return QueryResultC::error("SQL exceeds maximum length");
    }

    let params_str = match unsafe { cstr_to_str(params_json) } {
        Ok(s) => s,
        Err(e) => return QueryResultC::error(e),
    };
    if params_str.len() > MAX_PARAMS_LENGTH {
        return QueryResultC::error("params JSON exceeds maximum length");
    }

    let rc = unsafe { &*rc };

    let param_refs = match parse_params(params_str) {
        Ok(p) => p,
        Err(e) => return QueryResultC::error(&e),
    };
    let param_slice: Vec<&dyn rusqlite::types::ToSql> =
        param_refs.iter().map(|p| p.as_ref()).collect();

    let mut stmt = match rc.conn.prepare(sql_str) {
        Ok(s) => s,
        Err(e) => return QueryResultC::error(&format!("{}", e)),
    };

    let columns: Vec<String> = stmt.column_names().iter().map(|c| c.to_string()).collect();
    let columns_json = match serde_json::to_string(&columns) {
        Ok(j) => j,
        Err(e) => return QueryResultC::error(&format!("failed to serialize columns: {}", e)),
    };

    let column_count = columns.len();
    let rows_result = stmt.query_map(param_slice.as_slice(), |row| {
        let mut values = Vec::with_capacity(column_count);
        for i in 0..column_count {
            let val = match row.get_ref(i)? {
                ValueRef::Null => serde_json::Value::Null,
                ValueRef::Integer(i) => serde_json::Value::Number(i.into()),
                ValueRef::Real(f) => serde_json::Value::Number(
                    serde_json::Number::from_f64(f).unwrap_or_else(|| 0i64.into()),
                ),
                ValueRef::Text(t) => {
                    let s = String::from_utf8_lossy(t).into_owned();
                    serde_json::Value::String(s)
                }
                ValueRef::Blob(b) => {
                    use base64::Engine;
                    let encoded = base64::engine::general_purpose::STANDARD.encode(b);
                    let mut map = serde_json::Map::new();
                    map.insert("$blob".to_string(), serde_json::Value::String(encoded));
                    serde_json::Value::Object(map)
                }
            };
            values.push(val);
        }
        Ok(serde_json::Value::Array(values))
    });

    let rows_iter = match rows_result {
        Ok(r) => r,
        Err(e) => return QueryResultC::error(&format!("{}", e)),
    };

    let mut all_rows = Vec::new();
    for row_result in rows_iter {
        match row_result {
            Ok(row) => all_rows.push(row),
            Err(e) => return QueryResultC::error(&format!("row iteration failed: {}", e)),
        }
    }

    let rows_json = match serde_json::to_string(&all_rows) {
        Ok(j) => j,
        Err(e) => return QueryResultC::error(&format!("failed to serialize rows: {}", e)),
    };

    QueryResultC::ok(columns_json, rows_json)
}

// ============================================================================
// Memory Management
// ============================================================================

#[no_mangle]
pub extern "C" fn exec_result_free(result: ExecResultC) {
    unsafe { free_cstring(result.error_msg) };
}

#[no_mangle]
pub extern "C" fn query_result_free(result: QueryResultC) {
    unsafe {
        free_cstring(result.error_msg);
        free_cstring(result.columns_json);
        free_cstring(result.rows_json);
    }
}

// ============================================================================
// Tests
// ============================================================================

#[cfg(test)]
mod tests {
    use super::*;
    use crate::ffi::{storage_free, storage_new_memory};
    use std::ffi::CString;

    fn with_store(f: impl FnOnce(*mut SqliteStore)) {
        let store = storage_new_memory();
        assert!(!store.is_null());
        f(store);
        storage_free(store);
    }

    #[test]
    fn test_sql_exec_create_and_insert() {
        with_store(|store| {
            let sql =
                CString::new("CREATE TABLE test (id INTEGER PRIMARY KEY, name TEXT)").unwrap();
            let params = CString::new("[]").unwrap();
            let result = sql_exec(store, sql.as_ptr(), params.as_ptr());
            assert!(result.success, "CREATE TABLE failed");
            exec_result_free(result);

            let sql = CString::new("INSERT INTO test (id, name) VALUES (?, ?)").unwrap();
            let params = CString::new(r#"[1, "alice"]"#).unwrap();
            let result = sql_exec(store, sql.as_ptr(), params.as_ptr());
            assert!(result.success, "INSERT failed");
            assert_eq!(result.last_insert_id, 1);
            assert_eq!(result.rows_affected, 1);
            exec_result_free(result);
        });
    }

    #[test]
    fn test_sql_query_returns_typed_values() {
        with_store(|store| {
            // Create and populate
            let sql =
                CString::new("CREATE TABLE test (id INTEGER, name TEXT, score REAL)").unwrap();
            let params = CString::new("[]").unwrap();
            let r = sql_exec(store, sql.as_ptr(), params.as_ptr());
            assert!(r.success);
            exec_result_free(r);

            let sql = CString::new("INSERT INTO test VALUES (1, 'alice', 9.5)").unwrap();
            let r = sql_exec(store, sql.as_ptr(), params.as_ptr());
            assert!(r.success);
            exec_result_free(r);

            let sql = CString::new("INSERT INTO test VALUES (2, NULL, 8.0)").unwrap();
            let r = sql_exec(store, sql.as_ptr(), params.as_ptr());
            assert!(r.success);
            exec_result_free(r);

            // Query
            let sql = CString::new("SELECT id, name, score FROM test ORDER BY id").unwrap();
            let result = sql_query(store, sql.as_ptr(), params.as_ptr());
            assert!(result.success, "SELECT failed");

            let cols_str = unsafe { std::ffi::CStr::from_ptr(result.columns_json) }
                .to_str()
                .unwrap();
            let cols: Vec<String> = serde_json::from_str(cols_str).unwrap();
            assert_eq!(cols, vec!["id", "name", "score"]);

            let rows_str = unsafe { std::ffi::CStr::from_ptr(result.rows_json) }
                .to_str()
                .unwrap();
            let rows: Vec<Vec<serde_json::Value>> = serde_json::from_str(rows_str).unwrap();
            assert_eq!(rows.len(), 2);
            assert_eq!(rows[0][0], serde_json::json!(1));
            assert_eq!(rows[0][1], serde_json::json!("alice"));
            assert_eq!(rows[1][1], serde_json::Value::Null);

            query_result_free(result);
        });
    }

    #[test]
    fn test_sql_begin_commit() {
        with_store(|store| {
            let sql = CString::new("CREATE TABLE test (id INTEGER)").unwrap();
            let params = CString::new("[]").unwrap();
            let r = sql_exec(store, sql.as_ptr(), params.as_ptr());
            assert!(r.success);
            exec_result_free(r);

            let r = sql_begin(store);
            assert!(r.success, "BEGIN failed");
            exec_result_free(r);

            let sql = CString::new("INSERT INTO test VALUES (1)").unwrap();
            let r = sql_exec(store, sql.as_ptr(), params.as_ptr());
            assert!(r.success);
            exec_result_free(r);

            let r = sql_commit(store);
            assert!(r.success, "COMMIT failed");
            exec_result_free(r);

            // Verify data persisted
            let sql = CString::new("SELECT COUNT(*) FROM test").unwrap();
            let r = sql_query(store, sql.as_ptr(), params.as_ptr());
            assert!(r.success);
            let rows_str = unsafe { std::ffi::CStr::from_ptr(r.rows_json) }
                .to_str()
                .unwrap();
            let rows: Vec<Vec<serde_json::Value>> = serde_json::from_str(rows_str).unwrap();
            assert_eq!(rows[0][0], serde_json::json!(1));
            query_result_free(r);
        });
    }

    #[test]
    fn test_sql_begin_rollback() {
        with_store(|store| {
            let sql = CString::new("CREATE TABLE test (id INTEGER)").unwrap();
            let params = CString::new("[]").unwrap();
            let r = sql_exec(store, sql.as_ptr(), params.as_ptr());
            assert!(r.success);
            exec_result_free(r);

            let r = sql_begin(store);
            assert!(r.success);
            exec_result_free(r);

            let sql = CString::new("INSERT INTO test VALUES (1)").unwrap();
            let r = sql_exec(store, sql.as_ptr(), params.as_ptr());
            assert!(r.success);
            exec_result_free(r);

            let r = sql_rollback(store);
            assert!(r.success, "ROLLBACK failed");
            exec_result_free(r);

            // Verify data was rolled back
            let sql = CString::new("SELECT COUNT(*) FROM test").unwrap();
            let r = sql_query(store, sql.as_ptr(), params.as_ptr());
            assert!(r.success);
            let rows_str = unsafe { std::ffi::CStr::from_ptr(r.rows_json) }
                .to_str()
                .unwrap();
            let rows: Vec<Vec<serde_json::Value>> = serde_json::from_str(rows_str).unwrap();
            assert_eq!(rows[0][0], serde_json::json!(0));
            query_result_free(r);
        });
    }

    #[test]
    fn test_sql_exec_null_store() {
        let sql = CString::new("SELECT 1").unwrap();
        let params = CString::new("[]").unwrap();
        let r = sql_exec(ptr::null_mut(), sql.as_ptr(), params.as_ptr());
        assert!(!r.success);
        exec_result_free(r);
    }

    #[test]
    fn test_sql_query_null_store() {
        let sql = CString::new("SELECT 1").unwrap();
        let params = CString::new("[]").unwrap();
        let r = sql_query(ptr::null(), sql.as_ptr(), params.as_ptr());
        assert!(!r.success);
        query_result_free(r);
    }
}
