//! QNTX WASM bridge
//!
//! Exposes qntx-core functions through a WASM-compatible ABI for use with
//! wazero (pure Go WebAssembly runtime). No WASI imports needed — all
//! functions are pure computation with shared memory string passing.
//!
//! # Memory Protocol
//!
//! Strings cross the WASM boundary as (ptr, len) pairs in linear memory.
//! The host allocates via [`wasm_alloc`], writes bytes, calls the function,
//! reads the result, then frees via [`wasm_free`].
//!
//! Return values pack pointer and length into a single u64:
//! `(ptr << 32) | len`

use qntx_core::parser::Parser;

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
