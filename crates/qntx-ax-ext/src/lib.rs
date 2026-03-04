//! qntx-ax-ext: SQLite loadable extension for AX attestation queries
//!
//! Provides three SQL functions:
//!
//! - `ax_parse(query_text)` → filter JSON (parse natural language into AxFilter)
//! - `ax_query(filter_json)` → results JSON (query attestations table with filter)
//! - `ax(query_text)` → results JSON (parse + query in one call)
//!
//! # Usage
//!
//! ```sql
//! .load libqntx_ax_ext
//!
//! SELECT ax_parse('ALICE is author of GitHub');
//! -- → {"subjects":["ALICE"],"predicates":["author"],"contexts":["GitHub"]}
//!
//! SELECT ax('is member of ACME');
//! -- → {"attestations":[...],"conflicts":[],"summary":{...}}
//! ```

use sqlite_loadable::prelude::*;
use sqlite_loadable::{api, define_scalar_function, ext, FunctionFlags};

use qntx_core::attestation::{Attestation, AxFilter, AxResult, AxSummary};

use std::collections::HashMap;
use std::ffi::{CStr, CString};
use std::os::raw::{c_char, c_uint};

// ============================================================================
// Extension Entry Point
// ============================================================================

/// Stored API pointer table — needed for sqlite3_errmsg which sqlite-loadable doesn't wrap.
static mut API: *const ext::sqlite3_api_routines = std::ptr::null();

/// Manual entrypoint (replaces #[sqlite_entrypoint] macro) so we can capture p_api.
#[no_mangle]
pub unsafe extern "C" fn sqlite3_qntxax_init(
    db: *mut sqlite3,
    _pz_err_msg: *mut *mut c_char,
    p_api: *mut ext::sqlite3_api_routines,
) -> c_uint {
    ext::faux_sqlite_extension_init2(p_api);
    API = p_api;

    match init(db) {
        Ok(()) => 0,
        Err(err) => err.code_extended(),
    }
}

fn init(db: *mut sqlite3) -> sqlite_loadable::Result<()> {
    define_scalar_function(db, "ax_parse", 1, ax_parse_fn, FunctionFlags::UTF8 | FunctionFlags::DETERMINISTIC)?;
    define_scalar_function(db, "ax_query", 1, ax_query_fn, FunctionFlags::UTF8)?;
    define_scalar_function(db, "ax", 1, ax_fn, FunctionFlags::UTF8)?;

    Ok(())
}

/// Get SQLite error message from db handle via the stored API pointer table.
unsafe fn errmsg(db: *mut sqlite3) -> String {
    if !API.is_null() {
        if let Some(f) = (*API).errmsg {
            let ptr = f(db);
            if !ptr.is_null() {
                return CStr::from_ptr(ptr).to_string_lossy().into_owned();
            }
        }
    }
    String::from("unknown error")
}

// ============================================================================
// ax_parse(query_text) → filter JSON
// ============================================================================

fn ax_parse_fn(ctx: *mut sqlite3_context, values: &[*mut sqlite3_value]) -> sqlite_loadable::Result<()> {
    let query_text = api::value_text_notnull(values.get(0).ok_or_else(|| {
        sqlite_loadable::Error::new_message("ax_parse: expected 1 argument")
    })?)?;

    match parse_to_filter(query_text) {
        Ok(json) => api::result_text(ctx, json)?,
        Err(e) => api::result_error(ctx, &e)?,
    }
    Ok(())
}

fn parse_to_filter(query_text: &str) -> std::result::Result<String, String> {
    let parsed = qntx_core::parser::Parser::parse(query_text)
        .map_err(|e| format!("ax_parse: {}", e))?;

    let filter = AxFilter {
        subjects: parsed.subjects.iter().map(|s| s.to_string()).collect(),
        predicates: parsed.predicates.iter().map(|s| s.to_string()).collect(),
        contexts: parsed.contexts.iter().map(|s| s.to_string()).collect(),
        actors: parsed.actors.iter().map(|s| s.to_string()).collect(),
        ..Default::default()
    };

    serde_json::to_string(&filter).map_err(|e| format!("ax_parse: serialization: {}", e))
}

// ============================================================================
// ax_query(filter_json) → results JSON
// ============================================================================

fn ax_query_fn(ctx: *mut sqlite3_context, values: &[*mut sqlite3_value]) -> sqlite_loadable::Result<()> {
    let filter_json = api::value_text_notnull(values.get(0).ok_or_else(|| {
        sqlite_loadable::Error::new_message("ax_query: expected 1 argument")
    })?)?;

    let db = unsafe { ext::sqlite3ext_context_db_handle(ctx) };

    match unsafe { execute_query(db, filter_json) } {
        Ok(json) => api::result_text(ctx, json)?,
        Err(e) => api::result_error(ctx, &e)?,
    }
    Ok(())
}

// ============================================================================
// ax(query_text) → results JSON (parse + query)
// ============================================================================

fn ax_fn(ctx: *mut sqlite3_context, values: &[*mut sqlite3_value]) -> sqlite_loadable::Result<()> {
    let query_text = api::value_text_notnull(values.get(0).ok_or_else(|| {
        sqlite_loadable::Error::new_message("ax: expected 1 argument")
    })?)?;

    let filter_json = match parse_to_filter(query_text) {
        Ok(j) => j,
        Err(e) => {
            api::result_error(ctx, &e)?;
            return Ok(());
        }
    };

    let db = unsafe { ext::sqlite3ext_context_db_handle(ctx) };

    match unsafe { execute_query(db, &filter_json) } {
        Ok(json) => api::result_text(ctx, json)?,
        Err(e) => api::result_error(ctx, &e)?,
    }
    Ok(())
}

// ============================================================================
// Query Execution (raw SQLite C API through extension pointer table)
// ============================================================================

/// Execute an ax query against the attestations table using the host's db connection.
unsafe fn execute_query(
    db: *mut sqlite3,
    filter_json: &str,
) -> std::result::Result<String, String> {
    let filter: AxFilter = serde_json::from_str(filter_json)
        .map_err(|e| format!("ax_query: invalid filter JSON: {}", e))?;

    let (sql, params) = build_sql(&filter);

    // Prepare statement
    let mut stmt: *mut ext::sqlite3_stmt = std::ptr::null_mut();
    let sql_cstr =
        CString::new(sql.as_str()).map_err(|_| "ax_query: SQL contains null byte".to_string())?;

    let rc = ext::sqlite3ext_prepare_v2(
        db,
        sql_cstr.as_ptr(),
        -1,
        &mut stmt,
        std::ptr::null_mut(),
    );
    if rc != 0 {
        return Err(format!("ax_query: prepare failed: {}", errmsg(db)));
    }

    // Bind parameters — keep CStrings alive until after step loop
    let param_cstrs: Vec<CString> = params
        .iter()
        .map(|p| CString::new(p.as_str()).unwrap_or_default())
        .collect();

    for (i, cstr) in param_cstrs.iter().enumerate() {
        let rc = ext::sqlite3ext_bind_text(
            stmt,
            (i + 1) as i32,
            cstr.as_ptr(),
            -1,
            // SQLITE_TRANSIENT = -1 cast to destructor type — tells SQLite to copy the string
            std::mem::transmute(-1isize),
        );
        if rc != 0 {
            ext::sqlite3ext_finalize(stmt);
            return Err(format!("ax_query: bind param {}: {}", i + 1, errmsg(db)));
        }
    }

    // Step through results
    let mut attestations = Vec::new();
    loop {
        let rc = ext::sqlite3ext_step(stmt);
        if rc == 101 {
            // SQLITE_DONE
            break;
        }
        if rc != 100 {
            // Not SQLITE_ROW
            ext::sqlite3ext_finalize(stmt);
            return Err(format!("ax_query: step failed: {}", errmsg(db)));
        }

        match row_to_attestation(stmt) {
            Ok(a) => attestations.push(a),
            Err(e) => {
                ext::sqlite3ext_finalize(stmt);
                return Err(format!("ax_query: row parse: {}", e));
            }
        }
    }

    ext::sqlite3ext_finalize(stmt);

    let summary = build_summary(&attestations);
    let result = AxResult {
        attestations,
        conflicts: Vec::new(),
        summary,
    };

    serde_json::to_string(&result).map_err(|e| format!("ax_query: serialization: {}", e))
}

// ============================================================================
// SQL Query Builder (mirrors qntx-sqlite/src/store.rs QueryStore::query)
// ============================================================================

fn build_sql(filter: &AxFilter) -> (String, Vec<String>) {
    let mut sql = String::from(
        "SELECT id, subjects, predicates, contexts, actors, timestamp, source, \
         attributes, created_at, signature, signer_did \
         FROM attestations WHERE 1=1",
    );
    let mut params: Vec<String> = Vec::new();

    if !filter.subjects.is_empty() {
        let placeholders: String = filter
            .subjects
            .iter()
            .map(|_| "?")
            .collect::<Vec<_>>()
            .join(", ");
        sql.push_str(&format!(
            " AND EXISTS (SELECT 1 FROM json_each(subjects) WHERE value IN ({}))",
            placeholders
        ));
        params.extend(filter.subjects.iter().cloned());
    }

    if !filter.predicates.is_empty() {
        let placeholders: String = filter
            .predicates
            .iter()
            .map(|_| "?")
            .collect::<Vec<_>>()
            .join(", ");
        sql.push_str(&format!(
            " AND EXISTS (SELECT 1 FROM json_each(predicates) WHERE value IN ({}))",
            placeholders
        ));
        params.extend(filter.predicates.iter().cloned());
    }

    if !filter.contexts.is_empty() {
        let placeholders: String = filter
            .contexts
            .iter()
            .map(|_| "?")
            .collect::<Vec<_>>()
            .join(", ");
        sql.push_str(&format!(
            " AND EXISTS (SELECT 1 FROM json_each(contexts) WHERE value IN ({}))",
            placeholders
        ));
        params.extend(filter.contexts.iter().cloned());
    }

    if !filter.actors.is_empty() {
        let placeholders: String = filter
            .actors
            .iter()
            .map(|_| "?")
            .collect::<Vec<_>>()
            .join(", ");
        sql.push_str(&format!(
            " AND EXISTS (SELECT 1 FROM json_each(actors) WHERE value IN ({}))",
            placeholders
        ));
        params.extend(filter.actors.iter().cloned());
    }

    sql.push_str(" ORDER BY created_at DESC");

    if let Some(limit) = filter.limit {
        sql.push_str(&format!(" LIMIT {}", limit));
    }

    (sql, params)
}

// ============================================================================
// Row Parsing
// ============================================================================

/// Extract an Attestation from the current row of a prepared statement.
unsafe fn row_to_attestation(
    stmt: *mut ext::sqlite3_stmt,
) -> std::result::Result<Attestation, String> {
    let id = column_text(stmt, 0);
    let subjects_json = column_text(stmt, 1);
    let predicates_json = column_text(stmt, 2);
    let contexts_json = column_text(stmt, 3);
    let actors_json = column_text(stmt, 4);
    let timestamp_str = column_text(stmt, 5);
    let source = column_text(stmt, 6);
    let attributes_json = column_text_opt(stmt, 7);
    let created_at_str = column_text(stmt, 8);
    // For blob: use column_value + value_blob
    let signature = column_blob_via_value(stmt, 9);
    let signer_did = column_text_opt(stmt, 10);

    let subjects: Vec<String> =
        serde_json::from_str(&subjects_json).map_err(|e| format!("subjects: {}", e))?;
    let predicates: Vec<String> =
        serde_json::from_str(&predicates_json).map_err(|e| format!("predicates: {}", e))?;
    let contexts: Vec<String> =
        serde_json::from_str(&contexts_json).map_err(|e| format!("contexts: {}", e))?;
    let actors: Vec<String> =
        serde_json::from_str(&actors_json).map_err(|e| format!("actors: {}", e))?;

    let attributes: HashMap<String, serde_json::Value> = match attributes_json {
        Some(ref j) if !j.is_empty() => serde_json::from_str(j).unwrap_or_default(),
        _ => HashMap::new(),
    };

    let timestamp = parse_timestamp_to_ms(&timestamp_str);
    let created_at = parse_timestamp_to_ms(&created_at_str);

    Ok(Attestation {
        id,
        subjects,
        predicates,
        contexts,
        actors,
        timestamp,
        source,
        attributes,
        created_at,
        signature,
        signer_did,
    })
}

/// Parse RFC3339 timestamp string to Unix milliseconds.
fn parse_timestamp_to_ms(s: &str) -> i64 {
    let trimmed = s.trim();
    if trimmed.is_empty() {
        return 0;
    }

    // Try integer (Unix ms) first
    if let Ok(ms) = trimmed.parse::<i64>() {
        return ms;
    }

    // RFC3339-like: YYYY-MM-DDTHH:MM:SS[.fff][Z|+00:00]
    let (date_part, time_part) = if let Some(idx) = trimmed.find('T') {
        (&trimmed[..idx], &trimmed[idx + 1..])
    } else if let Some(idx) = trimmed.find(' ') {
        (&trimmed[..idx], &trimmed[idx + 1..])
    } else {
        return 0;
    };

    // Strip timezone suffix from time part
    let time_clean = time_part
        .trim_end_matches('Z')
        .split('+')
        .next()
        .unwrap_or("00:00:00");
    // Handle negative timezone offset (but not date separators)
    let time_clean = if time_clean.len() > 8 {
        time_clean.split('-').next().unwrap_or(time_clean)
    } else {
        time_clean
    };

    let date_parts: Vec<&str> = date_part.split('-').collect();
    if date_parts.len() != 3 {
        return 0;
    }

    let year: i64 = date_parts[0].parse().unwrap_or(0);
    let month: i64 = date_parts[1].parse().unwrap_or(1);
    let day: i64 = date_parts[2].parse().unwrap_or(1);

    let time_parts: Vec<&str> = time_clean.split(':').collect();
    let hour: i64 = time_parts.first().and_then(|s| s.parse().ok()).unwrap_or(0);
    let minute: i64 = time_parts.get(1).and_then(|s| s.parse().ok()).unwrap_or(0);
    let sec_str = time_parts.get(2).unwrap_or(&"0");
    let second: i64 = sec_str.split('.').next().unwrap_or("0").parse().unwrap_or(0);

    let days = days_from_civil(year, month, day);
    days * 86_400_000 + hour * 3_600_000 + minute * 60_000 + second * 1000
}

/// Days since Unix epoch. Algorithm from Howard Hinnant.
fn days_from_civil(year: i64, month: i64, day: i64) -> i64 {
    let y = if month <= 2 { year - 1 } else { year };
    let m = if month <= 2 { month + 9 } else { month - 3 };
    let era = if y >= 0 { y } else { y - 399 } / 400;
    let yoe = y - era * 400;
    let doy = (153 * m + 2) / 5 + day - 1;
    let doe = yoe * 365 + yoe / 4 - yoe / 100 + doy;
    era * 146097 + doe - 719468
}

// ============================================================================
// Column Extraction Helpers
// ============================================================================

unsafe fn column_text(stmt: *mut ext::sqlite3_stmt, col: i32) -> String {
    let ptr = ext::sqlite3ext_column_text(stmt, col);
    if ptr.is_null() {
        String::new()
    } else {
        CStr::from_ptr(ptr as *const c_char)
            .to_string_lossy()
            .into_owned()
    }
}

unsafe fn column_text_opt(stmt: *mut ext::sqlite3_stmt, col: i32) -> Option<String> {
    let ptr = ext::sqlite3ext_column_text(stmt, col);
    if ptr.is_null() {
        None
    } else {
        Some(
            CStr::from_ptr(ptr as *const c_char)
                .to_string_lossy()
                .into_owned(),
        )
    }
}

/// Get blob data via column_value + value_blob (since column_blob isn't in ext)
unsafe fn column_blob_via_value(stmt: *mut ext::sqlite3_stmt, col: i32) -> Option<Vec<u8>> {
    let value = ext::sqlite3ext_column_value(stmt, col);
    if value.is_null() {
        return None;
    }
    let ptr = ext::sqlite3ext_value_blob(value);
    if ptr.is_null() {
        return None;
    }
    let len = ext::sqlite3ext_value_bytes(value) as usize;
    if len == 0 {
        return None;
    }
    let slice = std::slice::from_raw_parts(ptr as *const u8, len);
    Some(slice.to_vec())
}

// ============================================================================
// Summary Builder
// ============================================================================

fn build_summary(attestations: &[Attestation]) -> AxSummary {
    let mut summary = AxSummary {
        total_attestations: attestations.len(),
        unique_subjects: HashMap::new(),
        unique_predicates: HashMap::new(),
        unique_contexts: HashMap::new(),
        unique_actors: HashMap::new(),
    };

    for att in attestations {
        for s in &att.subjects {
            *summary.unique_subjects.entry(s.clone()).or_insert(0) += 1;
        }
        for p in &att.predicates {
            *summary.unique_predicates.entry(p.clone()).or_insert(0) += 1;
        }
        for c in &att.contexts {
            *summary.unique_contexts.entry(c.clone()).or_insert(0) += 1;
        }
        for a in &att.actors {
            *summary.unique_actors.entry(a.clone()).or_insert(0) += 1;
        }
    }

    summary
}
