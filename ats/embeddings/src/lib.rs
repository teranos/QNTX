pub mod engine;
pub mod engine_simple;
pub mod types;

#[cfg(feature = "ffi")]
pub mod ffi;

// Use the real engine now that ort 2.0 API is fixed
pub use engine::EmbeddingEngine;
pub use types::{EmbeddingResult, ModelInfo};

/// Initialize the ONNX runtime environment
pub fn init() -> anyhow::Result<()> {
    // In ort 2.0, init is handled automatically
    Ok(())
}
