//! C-compatible FFI interface for EmbeddingEngine
//!
//! Follows the same pointer-based ownership pattern as qntx-sqlite and vidstream:
//! - `embedding_engine_init()` allocates on Rust heap, caller owns pointer
//! - `embedding_engine_free()` must be called to deallocate
//! - All operations take the engine pointer as first argument
//! - No global state — Go side manages lifetime and synchronization

use std::os::raw::{c_char, c_float, c_int};
use std::ptr;

use qntx_ffi_common::{cstr_to_str, cstring_new_or_empty};

use crate::EmbeddingEngine;

/// Initialize the embedding engine with a model file.
///
/// Returns a pointer to the engine on success, or NULL on error.
/// Caller owns the pointer and must call `embedding_engine_free` to deallocate.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn embedding_engine_init(model_path: *const c_char) -> *mut EmbeddingEngine {
    let path = match unsafe { cstr_to_str(model_path) } {
        Ok(s) => s,
        Err(_) => return ptr::null_mut(),
    };

    // Initialize ORT environment
    crate::engine::EmbeddingEngine::init_ort();

    match EmbeddingEngine::new(path, "sentence-transformers".to_string()) {
        Ok(engine) => Box::into_raw(Box::new(engine)),
        Err(e) => {
            eprintln!("Failed to create embedding engine: {}", e);
            ptr::null_mut()
        }
    }
}

qntx_ffi_common::define_engine_free!(embedding_engine_free, EmbeddingEngine);

/// Get model dimensions.
///
/// Returns dimensions on success, -1 on error.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn embedding_engine_dimensions(engine: *const EmbeddingEngine) -> c_int {
    if engine.is_null() {
        return -1;
    }
    let engine = unsafe { &*engine };
    engine.model_info().dimensions as c_int
}

/// Embed a single text and write the embedding vector to a pre-allocated buffer.
///
/// Returns the number of dimensions written on success, -1 on error.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn embedding_engine_embed(
    engine: *mut EmbeddingEngine,
    text: *const c_char,
    embedding_out: *mut c_float,
    dimensions: c_int,
) -> c_int {
    if engine.is_null() || text.is_null() || embedding_out.is_null() {
        return -1;
    }

    let text_str = match unsafe { cstr_to_str(text) } {
        Ok(s) => s,
        Err(_) => return -1,
    };

    let engine = unsafe { &mut *engine };

    match engine.embed(text_str) {
        Ok(result) => {
            let actual_dims = result.embedding.len();
            if actual_dims > dimensions as usize {
                return -1; // Buffer too small
            }

            for (i, &val) in result.embedding.iter().enumerate() {
                unsafe {
                    *embedding_out.add(i) = val;
                }
            }

            actual_dims as c_int
        }
        Err(e) => {
            eprintln!("Embedding failed: {}", e);
            -1
        }
    }
}

/// Embed a text and return JSON result string.
///
/// Returns a null-terminated C string that must be freed with `embedding_free_string`,
/// or NULL on error.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn embedding_engine_embed_json(
    engine: *mut EmbeddingEngine,
    text: *const c_char,
) -> *mut c_char {
    if engine.is_null() || text.is_null() {
        return ptr::null_mut();
    }

    let text_str = match unsafe { cstr_to_str(text) } {
        Ok(s) => s,
        Err(_) => return ptr::null_mut(),
    };

    let engine = unsafe { &mut *engine };

    match engine.embed(text_str) {
        Ok(result) => match serde_json::to_string(&result) {
            Ok(json) => cstring_new_or_empty(&json),
            Err(_) => ptr::null_mut(),
        },
        Err(_) => ptr::null_mut(),
    }
}

qntx_ffi_common::define_string_free!(embedding_free_string);
