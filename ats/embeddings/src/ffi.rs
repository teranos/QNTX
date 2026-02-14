//! C-compatible FFI interface for EmbeddingEngine
//!
//! Follows the same pointer-based ownership pattern as qntx-sqlite and vidstream:
//! - `embedding_engine_init()` allocates on Rust heap, caller owns pointer
//! - `embedding_engine_free()` must be called to deallocate
//! - All operations take the engine pointer as first argument
//! - No global state — Go side manages lifetime and synchronization

use std::os::raw::{c_char, c_float, c_int};
use std::ptr;

use qntx_ffi_common::{cstr_to_str, cstring_new_or_empty, vec_into_raw, FfiResult};

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

// --- HDBSCAN clustering FFI ---

/// Result of HDBSCAN clustering, returned by `embedding_cluster_hdbscan`.
/// Caller must free `labels` with `embedding_free_int_array`,
/// `probabilities` with `embedding_free_float_array`,
/// and `error_msg` (if non-null) with `embedding_free_string`.
#[repr(C)]
pub struct ClusterResultC {
    pub success: c_int,
    pub error_msg: *mut c_char,
    pub labels: *mut c_int,
    pub probabilities: *mut c_float,
    pub count: c_int,
    pub n_clusters: c_int,
}

impl qntx_ffi_common::FfiResult for ClusterResultC {
    const ERROR_FALLBACK: &'static str = "HDBSCAN clustering failed";

    fn error_fields(error_msg: *mut c_char) -> Self {
        ClusterResultC {
            success: 0,
            error_msg,
            labels: ptr::null_mut(),
            probabilities: ptr::null_mut(),
            count: 0,
            n_clusters: 0,
        }
    }
}

/// Run HDBSCAN clustering on a flat array of float32 embeddings.
///
/// `data`: flat array of n_points * dimensions floats
/// `n_points`: number of embedding vectors
/// `dimensions`: dimensionality of each vector
/// `min_cluster_size`: minimum cluster size for HDBSCAN
///
/// Returns ClusterResultC. On success, `labels` and `probabilities` are heap-allocated
/// arrays of length `count` that must be freed by the caller.
#[no_mangle]
pub extern "C" fn embedding_cluster_hdbscan(
    data: *const c_float,
    n_points: c_int,
    dimensions: c_int,
    min_cluster_size: c_int,
) -> ClusterResultC {
    if data.is_null() || n_points <= 0 || dimensions <= 0 || min_cluster_size <= 0 {
        return ClusterResultC::error("invalid arguments: null data or non-positive parameters");
    }

    let n = n_points as usize;
    let dims = dimensions as usize;
    let total = n * dims;

    let slice = unsafe { std::slice::from_raw_parts(data, total) };

    match crate::cluster::cluster_embeddings(slice, n, dims, min_cluster_size as usize) {
        Ok(result) => {
            let count = result.labels.len() as c_int;
            let n_clusters = result.n_clusters as c_int;

            // Convert labels from i32 to c_int (same on all targets, but explicit)
            let labels_vec: Vec<c_int> = result.labels.into_iter().map(|l| l as c_int).collect();
            let (labels_ptr, _) = vec_into_raw(labels_vec);
            let (probs_ptr, _) = vec_into_raw(result.probabilities);

            ClusterResultC {
                success: 1,
                error_msg: ptr::null_mut(),
                labels: labels_ptr,
                probabilities: probs_ptr,
                count,
                n_clusters,
            }
        }
        Err(e) => ClusterResultC::error(&e),
    }
}

/// Free an int array returned by clustering FFI.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn embedding_free_int_array(arr: *mut c_int, len: c_int) {
    if !arr.is_null() && len > 0 {
        unsafe { qntx_ffi_common::free_boxed_slice(arr, len as usize) };
    }
}

/// Free a float array returned by clustering FFI.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn embedding_free_float_array(arr: *mut c_float, len: c_int) {
    if !arr.is_null() && len > 0 {
        unsafe { qntx_ffi_common::free_boxed_slice(arr, len as usize) };
    }
}
