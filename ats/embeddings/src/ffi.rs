use std::ffi::{CStr, CString};
use std::os::raw::{c_char, c_float, c_int};
use std::ptr;
use std::sync::Mutex;

use crate::{init, EmbeddingEngine};

// Global engine instance protected by mutex
static mut ENGINE: Option<Mutex<EmbeddingEngine>> = None;

/// Initialize the embedding engine with a model file
#[no_mangle]
pub extern "C" fn embedding_engine_init(model_path: *const c_char) -> c_int {
    if model_path.is_null() {
        return -1;
    }

    // Initialize ONNX Runtime
    if let Err(e) = init() {
        eprintln!("Failed to initialize ONNX Runtime: {}", e);
        return -1;
    }

    unsafe {
        let path = match CStr::from_ptr(model_path).to_str() {
            Ok(s) => s,
            Err(_) => return -1,
        };

        match EmbeddingEngine::new(path, "sentence-transformers".to_string()) {
            Ok(engine) => {
                ENGINE = Some(Mutex::new(engine));
                0
            }
            Err(e) => {
                eprintln!("Failed to create embedding engine: {}", e);
                -1
            }
        }
    }
}

/// Free the embedding engine
#[no_mangle]
pub extern "C" fn embedding_engine_free() {
    unsafe {
        ENGINE = None;
    }
}

/// Get model dimensions
#[no_mangle]
pub extern "C" fn embedding_engine_dimensions() -> c_int {
    unsafe {
        if let Some(ref engine_mutex) = ENGINE {
            if let Ok(engine) = engine_mutex.lock() {
                return engine.model_info().dimensions as c_int;
            }
        }
    }
    -1
}

/// Embed a single text and return the embedding vector
///
/// Returns the number of dimensions on success, -1 on error
/// The caller must provide a pre-allocated buffer of sufficient size
#[no_mangle]
pub extern "C" fn embedding_engine_embed(
    text: *const c_char,
    embedding_out: *mut c_float,
    dimensions: c_int,
) -> c_int {
    if text.is_null() || embedding_out.is_null() {
        return -1;
    }

    unsafe {
        let text_str = match CStr::from_ptr(text).to_str() {
            Ok(s) => s,
            Err(_) => return -1,
        };

        if let Some(ref engine_mutex) = ENGINE {
            if let Ok(engine) = engine_mutex.lock() {
                match engine.embed(text_str) {
                    Ok(result) => {
                        let actual_dims = result.embedding.len();
                        if actual_dims > dimensions as usize {
                            return -1; // Buffer too small
                        }

                        // Copy embedding to output buffer
                        for (i, &val) in result.embedding.iter().enumerate() {
                            *embedding_out.offset(i as isize) = val;
                        }

                        return actual_dims as c_int;
                    }
                    Err(e) => {
                        eprintln!("Embedding failed: {}", e);
                        return -1;
                    }
                }
            }
        }
    }
    -1
}

/// Get embedding as JSON string (for easier integration)
///
/// Returns a null-terminated C string that must be freed with embedding_free_string
#[no_mangle]
pub extern "C" fn embedding_engine_embed_json(text: *const c_char) -> *mut c_char {
    if text.is_null() {
        return ptr::null_mut();
    }

    unsafe {
        let text_str = match CStr::from_ptr(text).to_str() {
            Ok(s) => s,
            Err(_) => return ptr::null_mut(),
        };

        if let Some(ref engine_mutex) = ENGINE {
            if let Ok(engine) = engine_mutex.lock() {
                match engine.embed(text_str) {
                    Ok(result) => match serde_json::to_string(&result) {
                        Ok(json) => match CString::new(json) {
                            Ok(c_str) => return c_str.into_raw(),
                            Err(_) => return ptr::null_mut(),
                        },
                        Err(_) => return ptr::null_mut(),
                    },
                    Err(_) => return ptr::null_mut(),
                }
            }
        }
    }
    ptr::null_mut()
}

/// Free a string returned by the FFI
#[no_mangle]
pub extern "C" fn embedding_free_string(s: *mut c_char) {
    if !s.is_null() {
        unsafe {
            let _ = CString::from_raw(s);
        }
    }
}
