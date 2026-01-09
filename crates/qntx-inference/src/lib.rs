//! QNTX Inference Plugin Library
//!
//! Provides local embedding generation using ONNX models.

pub mod engine;
pub mod service;

pub use engine::{create_engine, EngineConfig, EngineError, InferenceEngine};
pub use service::InferencePluginService;
