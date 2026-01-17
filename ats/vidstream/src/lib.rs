//! QNTX Video Processing Library (CGO)
//!
//! Real-time video frame processing for attestation generation.
//! Designed for ultra-low latency integration with Go via CGO.
//!
//! ## Architecture
//!
//! ```text
//! ┌─────────────┐     ┌──────────────┐     ┌─────────────────┐
//! │ Video Frame │────▶│ VideoEngine  │────▶│ DetectionResult │
//! │ (raw bytes) │     │ (Rust/ONNX)  │     │ (C-compatible)  │
//! └─────────────┘     └──────────────┘     └─────────────────┘
//! ```
//!
//! ## Usage from Go via CGO
//!
//! ```go
//! engine, err := videoengine.NewVideoEngine(modelPath)
//! if err != nil {
//!     log.Fatal(err)
//! }
//! defer engine.Close()
//!
//! result := engine.ProcessFrame(frameData, width, height, timestamp)
//! for _, detection := range result.Detections {
//!     // Create attestation from detection
//! }
//! ```
//!
//! ## Memory Ownership
//!
//! - `video_engine_new()` allocates on Rust heap, caller owns pointer
//! - `video_engine_free()` must be called to deallocate
//! - Detection results are owned by caller and must be freed
//! - Use `video_result_free()` to deallocate results

pub mod engine;
pub mod ffi;
pub mod types;

// Re-export main types
pub use engine::VideoEngine;
pub use types::{BoundingBox, Detection, FrameFormat, ProcessingStats};

// Re-export FFI types for C consumers
pub use ffi::{BoundingBoxC, DetectionC, ProcessingStatsC, VideoEngineConfigC, VideoResultC};
