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

// Shared identity logic (used by both wazero and browser targets)
mod identity;

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
mod wazero {
    use super::*;

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

    /// Format an error as JSON string.
    fn error_json(msg: &str) -> String {
        format!(r#"{{"error":"{}"}}"#, msg.replace('"', "\\\""))
    }

    /// Write an error JSON response to WASM memory.
    fn write_error(msg: &str) -> u64 {
        write_result(&error_json(msg))
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
    // Classification
    // ============================================================================

    /// Inner logic for classify_claims — testable without WASM memory ABI.
    fn classify_claims_impl(input: &str) -> String {
        qntx_core::classify_claims(input)
    }

    /// Classify claim conflicts. Takes (ptr, len) pointing to a JSON string:
    /// ```json
    /// {
    ///   "claim_groups": [{"key": "...", "claims": [...]}],
    ///   "config": {"verification_window_ms": 60000, ...},
    ///   "now_ms": 1234567890
    /// }
    /// ```
    ///
    /// Returns packed u64 pointing to JSON:
    /// ```json
    /// {
    ///   "conflicts": [...],
    ///   "auto_resolved": N,
    ///   "review_required": N,
    ///   "total_analyzed": N
    /// }
    /// ```
    #[no_mangle]
    pub extern "C" fn classify_claims(ptr: u32, len: u32) -> u64 {
        let input = unsafe { read_str(ptr, len) };
        write_result(&classify_claims_impl(input))
    }

    // ============================================================================
    // Cartesian expansion
    // ============================================================================

    /// Inner logic for expand_cartesian_claims — testable without WASM memory ABI.
    fn expand_cartesian_claims_impl(input: &str) -> String {
        qntx_core::expand_claims_json(input)
    }

    /// Inner logic for group_claims — testable without WASM memory ABI.
    fn group_claims_impl(input: &str) -> String {
        qntx_core::group_claims_json(input)
    }

    /// Inner logic for dedup_source_ids — testable without WASM memory ABI.
    fn dedup_source_ids_impl(input: &str) -> String {
        qntx_core::dedup_source_ids_json(input)
    }

    /// Expand compact attestations into individual claims via cartesian product.
    /// Takes (ptr, len) pointing to a JSON string:
    /// ```json
    /// {
    ///   "attestations": [{
    ///     "id": "...", "subjects": [...], "predicates": [...],
    ///     "contexts": [...], "actors": [...], "timestamp_ms": N
    ///   }]
    /// }
    /// ```
    ///
    /// Returns packed u64 pointing to JSON:
    /// ```json
    /// {
    ///   "claims": [{"subject":"...","predicate":"...","context":"...","actor":"...","timestamp_ms":N,"source_id":"..."}],
    ///   "total": N
    /// }
    /// ```
    #[no_mangle]
    pub extern "C" fn expand_cartesian_claims(ptr: u32, len: u32) -> u64 {
        let input = unsafe { read_str(ptr, len) };
        write_result(&expand_cartesian_claims_impl(input))
    }

    /// Group individual claims by (subject, predicate, context) key.
    /// Takes (ptr, len) pointing to a JSON string:
    /// `{"claims": [...]}`
    ///
    /// Returns packed u64 pointing to JSON:
    /// `{"groups": [{"key": "...", "claims": [...]}], "total_groups": N}`
    #[no_mangle]
    pub extern "C" fn group_claims(ptr: u32, len: u32) -> u64 {
        let input = unsafe { read_str(ptr, len) };
        write_result(&group_claims_impl(input))
    }

    /// Deduplicate claims to unique source attestation IDs, preserving order.
    /// Takes (ptr, len) pointing to a JSON string:
    /// `{"claims": [...]}`
    ///
    /// Returns packed u64 pointing to JSON:
    /// `{"ids": ["..."], "total": N}`
    #[no_mangle]
    pub extern "C" fn dedup_source_ids(ptr: u32, len: u32) -> u64 {
        let input = unsafe { read_str(ptr, len) };
        write_result(&dedup_source_ids_impl(input))
    }

    // ============================================================================
    // Identity (qntx-id)
    // ============================================================================

    /// Generate a content-addressed ASUID from SPC components and a content hash.
    /// Input: `{"prefix":"AS","subject":"Sarah","predicate":"author","context":"GitHub","content_hash":"<64-char hex>"}`
    /// Returns packed u64 pointing to `{"full":"AS-SARAH-AUTHOR-GITHUB-7K4M3B9X","short":"AS-SARAH-AUTHOR-GITHUB-7K4M"}`
    /// or `{"error":"..."}` on invalid input.
    #[no_mangle]
    pub extern "C" fn generate_asuid(ptr: u32, len: u32) -> u64 {
        let input = unsafe { read_str(ptr, len) };
        write_result(&crate::identity::generate_asuid_impl(input))
    }

    /// Generate a random ID using the QNTX alphabet.
    /// Input: `{"length":8,"random_bytes":[161,178,...]}`
    /// Returns packed u64 pointing to `{"id":"A3B7X9K2"}` or `{"error":"..."}`.
    #[no_mangle]
    pub extern "C" fn generate_random_id(ptr: u32, len: u32) -> u64 {
        let input = unsafe { read_str(ptr, len) };
        write_result(&crate::identity::generate_random_id_impl(input))
    }

    /// Clean a seed string for ID generation (normalize, uppercase, collapse repeats).
    /// Input: raw UTF-8 string
    /// Returns packed u64 pointing to the cleaned string.
    #[no_mangle]
    pub extern "C" fn id_clean_seed(ptr: u32, len: u32) -> u64 {
        let input = unsafe { read_str(ptr, len) };
        write_result(&qntx_id::clean_seed(input))
    }

    /// Normalize input for ID lookup (uppercase, map 0→O/1→I, strip invalid).
    /// Input: raw UTF-8 string
    /// Returns packed u64 pointing to the normalized string.
    #[no_mangle]
    pub extern "C" fn id_normalize_for_lookup(ptr: u32, len: u32) -> u64 {
        let input = unsafe { read_str(ptr, len) };
        write_result(&qntx_id::normalize_for_lookup(input))
    }

    // ============================================================================
    // Tests
    // ============================================================================

    #[cfg(test)]
    mod tests {
        use super::*;

        #[test]
        fn classify_claims_evolution() {
            let now = 1_000_000_000_i64;
            let input = serde_json::json!({
                "claim_groups": [{
                    "key": "ALICE|role|GitHub",
                    "claims": [
                        {"subject": "ALICE", "predicate": "is_junior", "context": "GitHub", "actor": "human:alice", "timestamp_ms": now - 200_000, "source_id": "as-1"},
                        {"subject": "ALICE", "predicate": "is_senior", "context": "GitHub", "actor": "human:alice", "timestamp_ms": now - 1000, "source_id": "as-2"}
                    ]
                }],
                "config": {"verification_window_ms": 60000, "evolution_window_ms": 86400000, "obsolescence_window_ms": 31536000000_i64},
                "now_ms": now
            });
            let result = classify_claims_impl(&input.to_string());
            let parsed: serde_json::Value = serde_json::from_str(&result).unwrap();
            assert!(parsed["error"].is_null(), "unexpected error: {}", result);
            assert_eq!(parsed["total_analyzed"], 1);
            assert_eq!(parsed["conflicts"][0]["conflict_type"], "Evolution");
        }

        #[test]
        fn classify_claims_invalid() {
            let result = classify_claims_impl("not json");
            let parsed: serde_json::Value = serde_json::from_str(&result).unwrap();
            assert!(parsed["error"]
                .as_str()
                .unwrap()
                .contains("invalid classify input"));
        }

        #[test]
        fn expand_cartesian_basic() {
            let input = serde_json::json!({
                "attestations": [{
                    "id": "SW001",
                    "subjects": ["LUKE", "LEIA"],
                    "predicates": ["operates_in"],
                    "contexts": ["REBELLION"],
                    "actors": ["imperial-records"],
                    "timestamp_ms": 1000
                }]
            });
            let result = expand_cartesian_claims_impl(&input.to_string());
            let parsed: serde_json::Value = serde_json::from_str(&result).unwrap();
            assert!(parsed["error"].is_null(), "unexpected error: {}", result);
            assert_eq!(parsed["total"], 2);
            let claims = parsed["claims"].as_array().unwrap();
            assert_eq!(claims[0]["subject"], "LUKE");
            assert_eq!(claims[1]["subject"], "LEIA");
        }

        #[test]
        fn expand_cartesian_invalid() {
            let result = expand_cartesian_claims_impl("not json");
            let parsed: serde_json::Value = serde_json::from_str(&result).unwrap();
            assert!(parsed["error"]
                .as_str()
                .unwrap()
                .contains("invalid expand input"));
        }

        #[test]
        fn group_claims_basic() {
            // Palace records and rebel intelligence both confirm Han is frozen;
            // Lando's guard disguise is a separate key.
            let input = serde_json::json!({
                "claims": [
                    {"subject": "HAN", "predicate": "frozen_in", "context": "JABBAS-PALACE", "actor": "palace-records", "timestamp_ms": 1, "source_id": "jabba-01"},
                    {"subject": "HAN", "predicate": "frozen_in", "context": "JABBAS-PALACE", "actor": "rebel-intelligence", "timestamp_ms": 2, "source_id": "rescue-01"},
                    {"subject": "LANDO", "predicate": "disguised_as", "context": "GUARD", "actor": "rebel-intelligence", "timestamp_ms": 3, "source_id": "rescue-02"}
                ]
            });
            let result = group_claims_impl(&input.to_string());
            let parsed: serde_json::Value = serde_json::from_str(&result).unwrap();
            assert!(parsed["error"].is_null(), "unexpected error: {}", result);
            assert_eq!(parsed["total_groups"], 2);
        }

        #[test]
        fn dedup_source_ids_basic() {
            // Luke's rescue plan covers both the droid delivery and Lando's infiltration;
            // Leia's carbonite heist is a second source.
            let input = serde_json::json!({
                "claims": [
                    {"subject": "R2D2", "predicate": "delivered_to", "context": "JABBAS-PALACE", "actor": "rebel-intelligence", "timestamp_ms": 1, "source_id": "rescue-plan"},
                    {"subject": "LANDO", "predicate": "infiltrated", "context": "JABBAS-PALACE", "actor": "rebel-intelligence", "timestamp_ms": 2, "source_id": "rescue-plan"},
                    {"subject": "LEIA", "predicate": "unfroze", "context": "HAN", "actor": "palace-surveillance", "timestamp_ms": 3, "source_id": "carbonite-heist"}
                ]
            });
            let result = dedup_source_ids_impl(&input.to_string());
            let parsed: serde_json::Value = serde_json::from_str(&result).unwrap();
            assert!(parsed["error"].is_null(), "unexpected error: {}", result);
            assert_eq!(parsed["total"], 2);
            assert_eq!(
                parsed["ids"].as_array().unwrap(),
                &["rescue-plan", "carbonite-heist"]
            );
        }

    }
} // end mod wazero

// Re-export wazero functions at crate root for backward compatibility
#[cfg(not(feature = "browser"))]
pub use wazero::*;
