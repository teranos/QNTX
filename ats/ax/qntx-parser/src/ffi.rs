//! C-compatible FFI interface for AX Query Parser
//!
//! This module exposes the parser functionality through a C ABI,
//! enabling integration with Go via CGO.
//!
//! # Memory Ownership Rules
//!
//! - String arrays in results are owned by the caller and must be freed
//! - `parser_result_free()` must be called to deallocate results
//! - `parser_string_free()` frees individual strings
//!
//! # Thread Safety
//!
//! The parser is stateless and thread-safe. Multiple threads can parse
//! concurrently without synchronization.

use std::ffi::{CStr, CString};
use std::os::raw::c_char;
use std::ptr;
use std::slice;

use crate::ax::{DurationUnit, Lexer, ParseError, Parser, TemporalClause};

// Safety limits
const MAX_QUERY_LENGTH: usize = 10_000;

/// Temporal clause type enum for FFI
#[repr(C)]
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TemporalTypeC {
    /// No temporal clause
    None = 0,
    /// "since DATE"
    Since = 1,
    /// "until DATE"
    Until = 2,
    /// "on DATE"
    On = 3,
    /// "between DATE and DATE"
    Between = 4,
    /// "over DURATION"
    Over = 5,
}

/// Duration unit enum for FFI
#[repr(C)]
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum DurationUnitC {
    /// Unknown/unparsed unit
    Unknown = 0,
    /// Years
    Years = 1,
    /// Months
    Months = 2,
    /// Weeks
    Weeks = 3,
    /// Days
    Days = 4,
}

impl From<Option<DurationUnit>> for DurationUnitC {
    fn from(unit: Option<DurationUnit>) -> Self {
        match unit {
            Some(DurationUnit::Years) => DurationUnitC::Years,
            Some(DurationUnit::Months) => DurationUnitC::Months,
            Some(DurationUnit::Weeks) => DurationUnitC::Weeks,
            Some(DurationUnit::Days) => DurationUnitC::Days,
            None => DurationUnitC::Unknown,
        }
    }
}

/// C-compatible temporal clause
#[repr(C)]
pub struct TemporalClauseC {
    /// Type of temporal clause
    pub temporal_type: TemporalTypeC,
    /// Start date/time (for Since, Until, On, Between) - owned string
    pub start: *mut c_char,
    /// End date/time (for Between only) - owned string
    pub end: *mut c_char,
    /// Duration value (for Over only)
    pub duration_value: f64,
    /// Duration unit (for Over only)
    pub duration_unit: DurationUnitC,
    /// Raw duration string (for Over only) - owned string
    pub duration_raw: *mut c_char,
}

impl Default for TemporalClauseC {
    fn default() -> Self {
        Self {
            temporal_type: TemporalTypeC::None,
            start: ptr::null_mut(),
            end: ptr::null_mut(),
            duration_value: 0.0,
            duration_unit: DurationUnitC::Unknown,
            duration_raw: ptr::null_mut(),
        }
    }
}

/// C-compatible parsed AX query result
#[repr(C)]
pub struct AxQueryResultC {
    /// True if parsing succeeded
    pub success: bool,
    /// Error message if !success (owned)
    pub error_msg: *mut c_char,
    /// Error position in input (byte offset)
    pub error_position: usize,

    /// Subject strings (owned array of owned strings)
    pub subjects: *mut *mut c_char,
    /// Number of subjects
    pub subjects_len: usize,

    /// Predicate strings (owned array of owned strings)
    pub predicates: *mut *mut c_char,
    /// Number of predicates
    pub predicates_len: usize,

    /// Context strings (owned array of owned strings)
    pub contexts: *mut *mut c_char,
    /// Number of contexts
    pub contexts_len: usize,

    /// Actor strings (owned array of owned strings)
    pub actors: *mut *mut c_char,
    /// Number of actors
    pub actors_len: usize,

    /// Temporal clause
    pub temporal: TemporalClauseC,

    /// Action strings (owned array of owned strings)
    pub actions: *mut *mut c_char,
    /// Number of actions
    pub actions_len: usize,

    /// Parse time in microseconds
    pub parse_time_us: u64,
}

impl AxQueryResultC {
    fn error(msg: &str, position: usize) -> Self {
        Self {
            success: false,
            error_msg: CString::new(msg)
                .unwrap_or_else(|_| CString::new("parse error").unwrap())
                .into_raw(),
            error_position: position,
            subjects: ptr::null_mut(),
            subjects_len: 0,
            predicates: ptr::null_mut(),
            predicates_len: 0,
            contexts: ptr::null_mut(),
            contexts_len: 0,
            actors: ptr::null_mut(),
            actors_len: 0,
            temporal: TemporalClauseC::default(),
            actions: ptr::null_mut(),
            actions_len: 0,
            parse_time_us: 0,
        }
    }
}

// ============================================================================
// Main Parse Function
// ============================================================================

/// Parse an AX query string.
///
/// # Arguments
/// - `query`: Null-terminated UTF-8 query string
///
/// # Returns
/// AxQueryResultC with parsed components. Caller must call
/// `parser_result_free` to deallocate.
///
/// # Safety
/// - `query` must be a valid null-terminated string
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn parser_parse_query(query: *const c_char) -> AxQueryResultC {
    let start_time = std::time::Instant::now();

    // Validate query pointer
    if query.is_null() {
        return AxQueryResultC::error("null query pointer", 0);
    }

    // Convert query to Rust string
    let query_str = match unsafe { CStr::from_ptr(query) }.to_str() {
        Ok(s) => s,
        Err(_) => return AxQueryResultC::error("invalid UTF-8 in query", 0),
    };

    // Validate query length
    if query_str.len() > MAX_QUERY_LENGTH {
        return AxQueryResultC::error("query exceeds maximum length", 0);
    }

    // Parse the query
    let lexer = Lexer::new(query_str);
    let parser = Parser::new(lexer);

    let query_result = match parser.parse() {
        Ok(q) => q,
        Err(e) => {
            let position = match &e {
                ParseError::UnexpectedToken { position, .. } => *position,
                ParseError::MissingElement { position, .. } => *position,
                ParseError::MissingAnd { position } => *position,
                ParseError::EmptyQuery => 0,
            };
            return AxQueryResultC::error(&e.to_string(), position);
        }
    };

    let parse_time_us = start_time.elapsed().as_micros() as u64;

    // Convert subjects
    let (subjects, subjects_len) = string_slice_to_c_array(&query_result.subjects);

    // Convert predicates
    let (predicates, predicates_len) = string_slice_to_c_array(&query_result.predicates);

    // Convert contexts
    let (contexts, contexts_len) = string_slice_to_c_array(&query_result.contexts);

    // Convert actors
    let (actors, actors_len) = string_slice_to_c_array(&query_result.actors);

    // Convert actions
    let (actions, actions_len) = string_slice_to_c_array(&query_result.actions);

    // Convert temporal clause
    let temporal = match &query_result.temporal {
        None => TemporalClauseC::default(),
        Some(TemporalClause::Since(date)) => TemporalClauseC {
            temporal_type: TemporalTypeC::Since,
            start: str_to_c_string(date),
            end: ptr::null_mut(),
            duration_value: 0.0,
            duration_unit: DurationUnitC::Unknown,
            duration_raw: ptr::null_mut(),
        },
        Some(TemporalClause::Until(date)) => TemporalClauseC {
            temporal_type: TemporalTypeC::Until,
            start: str_to_c_string(date),
            end: ptr::null_mut(),
            duration_value: 0.0,
            duration_unit: DurationUnitC::Unknown,
            duration_raw: ptr::null_mut(),
        },
        Some(TemporalClause::On(date)) => TemporalClauseC {
            temporal_type: TemporalTypeC::On,
            start: str_to_c_string(date),
            end: ptr::null_mut(),
            duration_value: 0.0,
            duration_unit: DurationUnitC::Unknown,
            duration_raw: ptr::null_mut(),
        },
        Some(TemporalClause::Between(start, end)) => TemporalClauseC {
            temporal_type: TemporalTypeC::Between,
            start: str_to_c_string(start),
            end: str_to_c_string(end),
            duration_value: 0.0,
            duration_unit: DurationUnitC::Unknown,
            duration_raw: ptr::null_mut(),
        },
        Some(TemporalClause::Over(dur)) => TemporalClauseC {
            temporal_type: TemporalTypeC::Over,
            start: ptr::null_mut(),
            end: ptr::null_mut(),
            duration_value: dur.value.unwrap_or(0.0),
            duration_unit: dur.unit.into(),
            duration_raw: str_to_c_string(dur.raw),
        },
    };

    AxQueryResultC {
        success: true,
        error_msg: ptr::null_mut(),
        error_position: 0,
        subjects,
        subjects_len,
        predicates,
        predicates_len,
        contexts,
        contexts_len,
        actors,
        actors_len,
        temporal,
        actions,
        actions_len,
        parse_time_us,
    }
}

// ============================================================================
// Memory Management
// ============================================================================

/// Free an AxQueryResultC and all contained strings.
///
/// # Safety
/// - `result` must be from `parser_parse_query`
/// - `result` must not be used after this call
#[no_mangle]
pub extern "C" fn parser_result_free(result: AxQueryResultC) {
    // Free error message
    free_c_string(result.error_msg);

    // Free string arrays
    free_string_array(result.subjects, result.subjects_len);
    free_string_array(result.predicates, result.predicates_len);
    free_string_array(result.contexts, result.contexts_len);
    free_string_array(result.actors, result.actors_len);
    free_string_array(result.actions, result.actions_len);

    // Free temporal strings
    free_c_string(result.temporal.start);
    free_c_string(result.temporal.end);
    free_c_string(result.temporal.duration_raw);
}

/// Free a string returned by parser functions.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn parser_string_free(s: *mut c_char) {
    free_c_string(s);
}

// ============================================================================
// Helpers
// ============================================================================

/// Convert a string slice to a C string
fn str_to_c_string(s: &str) -> *mut c_char {
    CString::new(s)
        .unwrap_or_else(|_| CString::new("").unwrap())
        .into_raw()
}

/// Convert a slice of string references to a C array of C strings
fn string_slice_to_c_array(strings: &[&str]) -> (*mut *mut c_char, usize) {
    if strings.is_empty() {
        return (ptr::null_mut(), 0);
    }

    let mut c_strings: Vec<*mut c_char> = Vec::with_capacity(strings.len());
    for s in strings {
        c_strings.push(str_to_c_string(s));
    }

    let len = c_strings.len();
    let ptr = Box::into_raw(c_strings.into_boxed_slice()) as *mut *mut c_char;

    (ptr, len)
}

/// Free a C string if not null
fn free_c_string(s: *mut c_char) {
    if !s.is_null() {
        unsafe {
            let _ = CString::from_raw(s);
        }
    }
}

/// Free an array of C strings
fn free_string_array(arr: *mut *mut c_char, len: usize) {
    if arr.is_null() || len == 0 {
        return;
    }

    let strings = unsafe { slice::from_raw_parts_mut(arr, len) };
    for s in strings.iter() {
        if !s.is_null() {
            unsafe {
                let _ = CString::from_raw(*s);
            }
        }
    }

    // Free the array itself
    unsafe {
        let _ = Box::from_raw(std::ptr::slice_from_raw_parts_mut(arr, len));
    }
}

// ============================================================================
// Tests
// ============================================================================

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parse_simple_query() {
        let query = CString::new("ALICE is author").unwrap();
        let result = parser_parse_query(query.as_ptr());

        assert!(result.success);
        assert_eq!(result.subjects_len, 1);
        assert_eq!(result.predicates_len, 1);

        // Check subject value
        let subjects = unsafe { slice::from_raw_parts(result.subjects, result.subjects_len) };
        let subject = unsafe { CStr::from_ptr(subjects[0]) }.to_str().unwrap();
        assert_eq!(subject, "ALICE");

        // Check predicate value
        let predicates =
            unsafe { slice::from_raw_parts(result.predicates, result.predicates_len) };
        let predicate = unsafe { CStr::from_ptr(predicates[0]) }.to_str().unwrap();
        assert_eq!(predicate, "author");

        parser_result_free(result);
    }

    #[test]
    fn test_parse_full_query() {
        let query =
            CString::new("ALICE BOB is author_of of GitHub by CHARLIE since 2024-01-01 so notify")
                .unwrap();
        let result = parser_parse_query(query.as_ptr());

        assert!(result.success);
        assert_eq!(result.subjects_len, 2);
        assert_eq!(result.predicates_len, 1);
        assert_eq!(result.contexts_len, 1);
        assert_eq!(result.actors_len, 1);
        assert_eq!(result.actions_len, 1);
        assert_eq!(result.temporal.temporal_type, TemporalTypeC::Since);

        parser_result_free(result);
    }

    #[test]
    fn test_parse_temporal_over() {
        let query = CString::new("ALICE is experienced over 5y").unwrap();
        let result = parser_parse_query(query.as_ptr());

        assert!(result.success);
        assert_eq!(result.temporal.temporal_type, TemporalTypeC::Over);
        assert_eq!(result.temporal.duration_value, 5.0);
        assert_eq!(result.temporal.duration_unit, DurationUnitC::Years);

        parser_result_free(result);
    }

    #[test]
    fn test_parse_temporal_between() {
        let query = CString::new("ALICE is author between 2024-01-01 and 2024-12-31").unwrap();
        let result = parser_parse_query(query.as_ptr());

        assert!(result.success);
        assert_eq!(result.temporal.temporal_type, TemporalTypeC::Between);

        let start = unsafe { CStr::from_ptr(result.temporal.start) }
            .to_str()
            .unwrap();
        assert_eq!(start, "2024-01-01");

        let end = unsafe { CStr::from_ptr(result.temporal.end) }
            .to_str()
            .unwrap();
        assert_eq!(end, "2024-12-31");

        parser_result_free(result);
    }

    #[test]
    fn test_parse_null_query() {
        let result = parser_parse_query(ptr::null());
        assert!(!result.success);
        parser_result_free(result);
    }

    #[test]
    fn test_parse_error() {
        let query = CString::new("ALICE is").unwrap();
        let result = parser_parse_query(query.as_ptr());

        assert!(!result.success);
        assert!(!result.error_msg.is_null());

        parser_result_free(result);
    }

    #[test]
    fn test_empty_query() {
        let query = CString::new("").unwrap();
        let result = parser_parse_query(query.as_ptr());

        assert!(result.success);
        assert_eq!(result.subjects_len, 0);
        assert_eq!(result.predicates_len, 0);

        parser_result_free(result);
    }
}
