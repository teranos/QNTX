//! QNTX WASM bridge
//!
//! Provides two WASM targets:
//!
//! ## Default (wazero/Go)
//! Exposes qntx-core functions through a raw memory ABI for use with
//! wazero (pure Go WebAssembly runtime). No WASI imports needed — all
//! functions are pure computation with shared memory string passing.
//!
//! Strings cross the WASM boundary as (ptr, len) pairs in linear memory.
//! The host allocates via [`wasm_alloc`], writes bytes, calls the function,
//! reads the result, then frees via [`wasm_free`].
//!
//! Return values pack pointer and length into a single u64:
//! `(ptr << 32) | len`
//!
//! ## Browser (feature = "browser")
//! Uses wasm-bindgen for seamless JavaScript interop and qntx-indexeddb
//! for browser-based persistent storage. Provides async functions for
//! parsing queries and storing/retrieving attestations.
//!
//! ## Proto Migration Note (ADR-006)
//!
//! This crate includes qntx-proto for potential future use of proto types.
//! WASM modules can use proto-generated types without any gRPC dependencies,
//! demonstrating clean separation of concerns:
//! - qntx-proto: Just types (5 dependencies)
//! - qntx-grpc: Types + gRPC infrastructure (50+ dependencies)

// Browser-specific module (wasm-bindgen + IndexedDB)
#[cfg(feature = "browser")]
pub mod browser;

// Re-export browser functions at crate root for convenience
#[cfg(feature = "browser")]
pub use browser::*;

// ============================================================================
// Wazero/Go target (raw memory ABI) - only when browser feature is disabled
// ============================================================================

#[cfg(not(feature = "browser"))]
use qntx_core::parser::Parser;

#[cfg(not(feature = "browser"))]
use qntx_core::fuzzy::{FuzzyEngine, VocabularyType};

#[cfg(not(feature = "browser"))]
mod wazero {
    use super::*;
    use std::cell::RefCell;

    // Global fuzzy engine. WASM is single-threaded so RefCell is safe.
    thread_local! {
        static FUZZY_ENGINE: RefCell<FuzzyEngine> = RefCell::new(FuzzyEngine::new());
    }

    // ============================================================================
    // Memory management
    // ============================================================================

    /// Allocate `size` bytes in WASM linear memory. Returns a pointer.
    /// The host must call `wasm_free` to release.
    #[no_mangle]
    pub extern "C" fn wasm_alloc(size: u32) -> u32 {
        let layout = match std::alloc::Layout::from_size_align(size as usize, 1) {
            Ok(l) => l,
            Err(_) => return 0,
        };
        if layout.size() == 0 {
            return 0;
        }
        let ptr = unsafe { std::alloc::alloc(layout) };
        if ptr.is_null() {
            return 0;
        }
        ptr as u32
    }

    /// Free a buffer previously allocated by `wasm_alloc` or returned by an
    /// export function.
    #[no_mangle]
    pub extern "C" fn wasm_free(ptr: u32, size: u32) {
        if ptr == 0 || size == 0 {
            return;
        }
        let layout = match std::alloc::Layout::from_size_align(size as usize, 1) {
            Ok(l) => l,
            Err(_) => return,
        };
        unsafe {
            std::alloc::dealloc(ptr as *mut u8, layout);
        }
    }

    // ============================================================================
    // Helpers
    // ============================================================================

    /// Read a UTF-8 string from WASM linear memory at (ptr, len).
    unsafe fn read_str(ptr: u32, len: u32) -> &'static str {
        let slice = std::slice::from_raw_parts(ptr as *const u8, len as usize);
        std::str::from_utf8_unchecked(slice)
    }

    /// Write a string into newly allocated WASM memory and return packed u64.
    /// The caller (host) is responsible for freeing via `wasm_free`.
    fn write_result(s: &str) -> u64 {
        let bytes = s.as_bytes();
        let len = bytes.len() as u32;
        let ptr = wasm_alloc(len);
        if ptr == 0 {
            return 0;
        }
        unsafe {
            std::ptr::copy_nonoverlapping(bytes.as_ptr(), ptr as *mut u8, len as usize);
        }
        ((ptr as u64) << 32) | (len as u64)
    }

    /// Write an error JSON response.
    fn write_error(msg: &str) -> u64 {
        let json = format!(r#"{{"error":"{}"}}"#, msg.replace('"', "\\\""));
        write_result(&json)
    }

    // ============================================================================
    // Version info
    // ============================================================================

    /// Get the qntx-core version. Returns a packed u64 (ptr << 32 | len) pointing
    /// to a string containing the version (e.g., "0.1.0").
    #[no_mangle]
    pub extern "C" fn qntx_core_version() -> u64 {
        write_result(env!("CARGO_PKG_VERSION"))
    }

    // ============================================================================
    // Parser
    // ============================================================================

    /// Parse an AX query string. Takes (ptr, len) pointing to a UTF-8 query
    /// string in WASM memory. Returns a packed u64 (ptr << 32 | len) pointing
    /// to a JSON-serialized AxQuery result.
    ///
    /// On success: `{"subjects":["ALICE"],"predicates":["author"],...}`
    /// On error: `{"error":"description"}`
    #[no_mangle]
    pub extern "C" fn parse_ax_query(ptr: u32, len: u32) -> u64 {
        let input = unsafe { read_str(ptr, len) };

        match Parser::parse(input) {
            Ok(query) => {
                // NOTE: User dissatisfaction - we're adding post-parse validation to match Go's
                // arbitrary error behavior. The parser accepts "over 5q" but Go rejects it.
                // A proper design would validate during parsing, not as a separate step.
                // This is a hack to achieve bug-for-bug compatibility with the Go parser.
                if let Some(qntx_core::parser::TemporalClause::Over(ref dur)) = query.temporal {
                    if dur.value.is_some() && dur.unit.is_none() {
                        // Has a number but invalid unit (like "5q")
                        return write_error(&format!("missing unit in '{}'", dur.raw));
                    }
                }

                match serde_json::to_string(&query) {
                    Ok(json) => write_result(&json),
                    Err(e) => write_error(&format!("serialization failed: {}", e)),
                }
            }
            Err(e) => write_error(&format!("{}", e)),
        }
    }
    // ============================================================================
    // Fuzzy matching
    // ============================================================================

    /// Rebuild the fuzzy index with new vocabulary.
    /// Input: JSON `{"predicates":["a","b"],"contexts":["c","d"]}`
    /// Output: JSON `{"predicate_count":N,"context_count":N,"hash":"..."}`
    #[no_mangle]
    pub extern "C" fn fuzzy_rebuild_index(ptr: u32, len: u32) -> u64 {
        let input = unsafe { read_str(ptr, len) };

        let v: serde_json::Value = match serde_json::from_str(input) {
            Ok(v) => v,
            Err(e) => return write_error(&format!("invalid rebuild input: {}", e)),
        };

        let predicates: Vec<String> = v
            .get("predicates")
            .and_then(|a| a.as_array())
            .map(|arr| {
                arr.iter()
                    .filter_map(|v| v.as_str().map(String::from))
                    .collect()
            })
            .unwrap_or_default();

        let contexts: Vec<String> = v
            .get("contexts")
            .and_then(|a| a.as_array())
            .map(|arr| {
                arr.iter()
                    .filter_map(|v| v.as_str().map(String::from))
                    .collect()
            })
            .unwrap_or_default();

        FUZZY_ENGINE.with(|engine| {
            let mut engine = engine.borrow_mut();
            let (pred_count, ctx_count, hash) = engine.rebuild_index(predicates, contexts);

            let result = format!(
                r#"{{"predicate_count":{},"context_count":{},"hash":"{}"}}"#,
                pred_count, ctx_count, hash
            );
            write_result(&result)
        })
    }

    /// Search the fuzzy index.
    /// Input: JSON `{"query":"...","vocab_type":"predicates"|"contexts","limit":N,"min_score":F}`
    /// Output: JSON `{"matches":[{"value":"...","score":F,"strategy":"..."}]}`
    #[no_mangle]
    pub extern "C" fn fuzzy_search(ptr: u32, len: u32) -> u64 {
        let input = unsafe { read_str(ptr, len) };

        let v: serde_json::Value = match serde_json::from_str(input) {
            Ok(v) => v,
            Err(e) => return write_error(&format!("invalid search input: {}", e)),
        };

        let query = match v.get("query").and_then(|q| q.as_str()) {
            Some(q) => q,
            None => return write_error("missing or invalid 'query' field"),
        };

        let vocab_type = match v.get("vocab_type").and_then(|vt| vt.as_str()) {
            Some(vt) => vt,
            None => return write_error("missing or invalid 'vocab_type' field"),
        };

        let limit = v
            .get("limit")
            .and_then(|l| l.as_u64())
            .unwrap_or(20) as usize;
        let min_score = v
            .get("min_score")
            .and_then(|s| s.as_f64())
            .unwrap_or(0.6);

        let vocab = match vocab_type {
            "predicates" => VocabularyType::Predicates,
            "contexts" => VocabularyType::Contexts,
            other => {
                return write_error(&format!(
                    "invalid vocab_type '{}': expected 'predicates' or 'contexts'",
                    other
                ))
            }
        };

        FUZZY_ENGINE.with(|engine| {
            let engine = engine.borrow();
            let matches = engine.find_matches(query, vocab, limit, min_score);

            match serde_json::to_string(&matches) {
                Ok(json) => {
                    let result = format!(r#"{{"matches":{}}}"#, json);
                    write_result(&result)
                }
                Err(e) => write_error(&format!("serialization failed: {}", e)),
            }
        })
    }

    /// Check if the fuzzy engine has vocabulary indexed.
    /// Returns 1 if ready, 0 if not.
    #[no_mangle]
    pub extern "C" fn fuzzy_is_ready() -> u32 {
        FUZZY_ENGINE.with(|engine| if engine.borrow().is_ready() { 1 } else { 0 })
    }

    /// Get the fuzzy index hash. Returns packed u64 string.
    #[no_mangle]
    pub extern "C" fn fuzzy_get_hash() -> u64 {
        FUZZY_ENGINE.with(|engine| {
            let engine = engine.borrow();
            let hash = engine.get_index_hash();
            if hash.is_empty() {
                write_result("")
            } else {
                write_result(hash)
            }
        })
    }
} // end mod wazero

// Re-export wazero functions at crate root for backward compatibility
#[cfg(not(feature = "browser"))]
pub use wazero::*;
