pub mod engine_simple;
pub mod types;

#[cfg(feature = "ffi")]
pub mod ffi;

pub use engine_simple::EmbeddingEngine;
pub use types::{EmbeddingResult, ModelInfo};

/// Initialize the ONNX runtime environment
pub fn init() -> anyhow::Result<()> {
    // In ort 2.0, init is handled automatically
    Ok(())
}
