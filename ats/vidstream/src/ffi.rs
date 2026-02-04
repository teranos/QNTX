//! C-compatible FFI interface for VideoEngine
//!
//! This module exposes the VideoEngine functionality through a C ABI,
//! enabling integration with Go via CGO or any other language with C FFI.
//!
//! # Memory Ownership Rules
//!
//! - `video_engine_new()` allocates on Rust heap, caller owns pointer
//! - `video_engine_free()` must be called to deallocate
//! - `VideoResultC` and its strings are owned by caller after return
//! - `video_result_free()` must be called to deallocate results
//!
//! # Thread Safety
//!
//! The VideoEngine is internally thread-safe via RwLock. Multiple threads
//! can call `video_engine_process_frame` concurrently on the same engine.
//!
//! # Safety
//!
//! All public FFI functions handle null pointer checks internally.
//! The caller is responsible for passing valid pointers as documented.

use std::os::raw::c_char;
use std::ptr;
use std::slice;

use qntx_ffi_common::{
    cstr_to_str, cstring_new_or_empty, free_boxed_slice, free_cstring, vec_into_raw, FfiResult,
};

use crate::engine::VideoEngine;
use crate::types::{FrameFormat, VideoEngineConfig};

// Safety limits to prevent DoS attacks
const MAX_FRAME_SIZE: usize = 100_000_000; // 100MB max frame
const MAX_MODEL_PATH_LEN: usize = 4096;
const MAX_LABELS_LEN: usize = 1_000_000; // 1MB max labels

/// C-compatible bounding box
#[repr(C)]
#[derive(Debug, Clone, Copy)]
pub struct BoundingBoxC {
    pub x: f32,
    pub y: f32,
    pub width: f32,
    pub height: f32,
}

/// C-compatible detection result
#[repr(C)]
pub struct DetectionC {
    /// Class/label ID
    pub class_id: u32,
    /// Human-readable label (owned, must be freed)
    pub label: *mut c_char,
    /// Confidence score 0.0-1.0
    pub confidence: f32,
    /// Bounding box
    pub bbox: BoundingBoxC,
    /// Track ID (0 if not tracked)
    pub track_id: u64,
}

/// C-compatible processing statistics
#[repr(C)]
#[derive(Debug, Clone, Copy, Default)]
pub struct ProcessingStatsC {
    /// Frame preprocessing time (microseconds)
    pub preprocess_us: u64,
    /// Model inference time (microseconds)
    pub inference_us: u64,
    /// Post-processing/NMS time (microseconds)
    pub postprocess_us: u64,
    /// Total processing time (microseconds)
    pub total_us: u64,
    /// Frame width processed
    pub frame_width: u32,
    /// Frame height processed
    pub frame_height: u32,
    /// Number of detections before NMS
    pub detections_raw: u32,
    /// Number of detections after NMS
    pub detections_final: u32,
}

/// C-compatible result wrapper for process_frame
#[repr(C)]
pub struct VideoResultC {
    /// True if operation succeeded
    pub success: bool,
    /// Error message if success is false (owned, must be freed)
    pub error_msg: *mut c_char,
    /// Array of detections (owned, must be freed with video_result_free)
    pub detections: *mut DetectionC,
    /// Number of detections in array
    pub detections_len: usize,
    /// Processing statistics
    pub stats: ProcessingStatsC,
}

/// C-compatible engine configuration
#[repr(C)]
pub struct VideoEngineConfigC {
    /// Path to ONNX model file (null-terminated)
    pub model_path: *const c_char,
    /// Confidence threshold for detections (0.0-1.0)
    pub confidence_threshold: f32,
    /// IoU threshold for NMS (0.0-1.0)
    pub nms_threshold: f32,
    /// Model input width (0 for auto-detect)
    pub input_width: u32,
    /// Model input height (0 for auto-detect)
    pub input_height: u32,
    /// Number of inference threads (0 for auto)
    pub num_threads: u32,
    /// Enable GPU inference if available
    pub use_gpu: bool,
    /// Class labels (null-terminated, optional)
    pub labels: *const c_char,
}

impl VideoResultC {
    fn success(detections: Vec<DetectionC>, stats: ProcessingStatsC) -> Self {
        let (detections_ptr, detections_len) = vec_into_raw(detections);

        Self {
            success: true,
            error_msg: ptr::null_mut(),
            detections: detections_ptr,
            detections_len,
            stats,
        }
    }
}

impl FfiResult for VideoResultC {
    const ERROR_FALLBACK: &'static str = "unknown error";

    fn error_fields(error_msg: *mut c_char) -> Self {
        Self {
            success: false,
            error_msg,
            detections: ptr::null_mut(),
            detections_len: 0,
            stats: ProcessingStatsC::default(),
        }
    }
}

// ============================================================================
// Engine Lifecycle
// ============================================================================

/// Create a new VideoEngine instance with default configuration.
///
/// # Returns
/// Pointer to VideoEngine, or NULL on allocation failure.
/// Caller owns the pointer and must call `video_engine_free` to deallocate.
///
/// # Safety
/// The returned pointer is valid until `video_engine_free` is called.
#[no_mangle]
pub extern "C" fn video_engine_new() -> *mut VideoEngine {
    match VideoEngine::new(VideoEngineConfig::default()) {
        Ok(engine) => Box::into_raw(Box::new(engine)),
        Err(e) => {
            eprintln!("[vidstream] Failed to create engine: {}", e);
            ptr::null_mut()
        }
    }
}

/// Create a new VideoEngine instance with custom configuration.
///
/// # Arguments
/// - `config`: Engine configuration
///
/// # Returns
/// Pointer to VideoEngine, or NULL on failure.
/// Caller owns the pointer and must call `video_engine_free` to deallocate.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn video_engine_new_with_config(
    config: *const VideoEngineConfigC,
) -> *mut VideoEngine {
    if config.is_null() {
        return ptr::null_mut();
    }

    let config = unsafe { &*config };

    // Convert C config to Rust config
    let model_path = match unsafe { cstr_to_str(config.model_path) } {
        Ok(s) if s.len() <= MAX_MODEL_PATH_LEN => s.to_string(),
        Ok(_) => return ptr::null_mut(), // too long
        Err(_) => String::new(),         // null pointer = empty
    };

    let labels = match unsafe { cstr_to_str(config.labels) } {
        Ok(s) if s.len() <= MAX_LABELS_LEN => Some(s.to_string()),
        Ok(_) => return ptr::null_mut(), // too long
        Err(_) => None,                  // null pointer = no labels
    };

    let rust_config = VideoEngineConfig {
        model_path,
        confidence_threshold: config.confidence_threshold,
        nms_threshold: config.nms_threshold,
        input_width: config.input_width,
        input_height: config.input_height,
        num_threads: config.num_threads,
        use_gpu: config.use_gpu,
        labels,
    };

    match VideoEngine::new(rust_config) {
        Ok(engine) => Box::into_raw(Box::new(engine)),
        Err(e) => {
            eprintln!("[vidstream] Failed to create engine: {}", e);
            ptr::null_mut()
        }
    }
}

qntx_ffi_common::define_engine_free!(video_engine_free, VideoEngine);

// ============================================================================
// Frame Processing
// ============================================================================

/// Process a single video frame and return detections.
///
/// # Arguments
/// - `engine`: Valid VideoEngine pointer
/// - `frame_data`: Pointer to raw pixel data
/// - `frame_len`: Length of frame data in bytes
/// - `width`: Frame width in pixels
/// - `height`: Frame height in pixels
/// - `format`: Pixel format (0=RGB8, 1=RGBA8, 2=BGR8, 3=YUV420, 4=Gray8)
/// - `timestamp_us`: Frame timestamp in microseconds
///
/// # Returns
/// VideoResultC with detections. Caller must call `video_result_free`.
///
/// # Safety
/// - `engine` must be valid
/// - `frame_data` must point to at least `frame_len` bytes
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn video_engine_process_frame(
    engine: *const VideoEngine,
    frame_data: *const u8,
    frame_len: usize,
    width: u32,
    height: u32,
    format: i32,
    timestamp_us: u64,
) -> VideoResultC {
    // Validate engine pointer
    if engine.is_null() {
        return VideoResultC::error("null engine pointer");
    }
    if frame_data.is_null() {
        return VideoResultC::error("null frame data pointer");
    }
    if frame_len > MAX_FRAME_SIZE {
        return VideoResultC::error("frame size exceeds maximum");
    }
    if width == 0 || height == 0 {
        return VideoResultC::error("invalid frame dimensions");
    }

    let engine = unsafe { &*engine };

    // Convert format
    let frame_format = match format {
        0 => FrameFormat::RGB8,
        1 => FrameFormat::RGBA8,
        2 => FrameFormat::BGR8,
        3 => FrameFormat::YUV420,
        4 => FrameFormat::Gray8,
        _ => return VideoResultC::error("invalid frame format"),
    };

    // Validate frame size
    let expected_size = VideoEngine::expected_frame_size(width, height, frame_format);
    if frame_len < expected_size {
        return VideoResultC::error("frame data too small for specified dimensions");
    }

    // Create slice from frame data
    let frame_slice = unsafe { slice::from_raw_parts(frame_data, frame_len) };

    // Process frame
    let (detections, stats) =
        engine.process_frame(frame_slice, width, height, frame_format, timestamp_us);

    // Convert to C types
    let c_detections: Vec<DetectionC> = detections
        .into_iter()
        .map(|d| DetectionC {
            class_id: d.class_id,
            label: cstring_new_or_empty(&d.label),
            confidence: d.confidence,
            bbox: BoundingBoxC {
                x: d.bbox.x,
                y: d.bbox.y,
                width: d.bbox.width,
                height: d.bbox.height,
            },
            track_id: d.track_id,
        })
        .collect();

    let c_stats = ProcessingStatsC {
        preprocess_us: stats.preprocess_us,
        inference_us: stats.inference_us,
        postprocess_us: stats.postprocess_us,
        total_us: stats.total_us,
        frame_width: stats.frame_width,
        frame_height: stats.frame_height,
        detections_raw: stats.detections_raw,
        detections_final: stats.detections_final,
    };

    VideoResultC::success(c_detections, c_stats)
}

/// Free a VideoResultC and all contained data.
///
/// # Safety
/// - `result` must be from `video_engine_process_frame`
/// - `result` must not be used after this call
#[no_mangle]
pub extern "C" fn video_result_free(result: VideoResultC) {
    unsafe {
        free_cstring(result.error_msg);

        // Free detections array and labels
        if !result.detections.is_null() && result.detections_len > 0 {
            let detections_slice =
                slice::from_raw_parts_mut(result.detections, result.detections_len);

            for d in detections_slice.iter() {
                free_cstring(d.label);
            }

            free_boxed_slice(result.detections, result.detections_len);
        }
    }
}

// ============================================================================
// Utilities
// ============================================================================

/// Check if the engine is ready for inference.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn video_engine_is_ready(engine: *const VideoEngine) -> bool {
    if engine.is_null() {
        return false;
    }
    let engine = unsafe { &*engine };
    engine.is_ready()
}

/// Get the model input dimensions.
///
/// # Arguments
/// - `engine`: Valid engine pointer
/// - `width`: Pointer to receive width (output)
/// - `height`: Pointer to receive height (output)
///
/// # Returns
/// True if successful, false if engine is null.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn video_engine_get_input_dimensions(
    engine: *const VideoEngine,
    width: *mut u32,
    height: *mut u32,
) -> bool {
    if engine.is_null() || width.is_null() || height.is_null() {
        return false;
    }

    let engine = unsafe { &*engine };
    let (w, h) = engine.input_dimensions();

    unsafe {
        *width = w;
        *height = h;
    }

    true
}

/// Get the expected frame size for given dimensions and format.
///
/// # Arguments
/// - `width`: Frame width
/// - `height`: Frame height
/// - `format`: Pixel format (0=RGB8, 1=RGBA8, 2=BGR8, 3=YUV420, 4=Gray8)
///
/// # Returns
/// Expected frame size in bytes, or 0 for invalid format.
#[no_mangle]
pub extern "C" fn video_expected_frame_size(width: u32, height: u32, format: i32) -> usize {
    let frame_format = match format {
        0 => FrameFormat::RGB8,
        1 => FrameFormat::RGBA8,
        2 => FrameFormat::BGR8,
        3 => FrameFormat::YUV420,
        4 => FrameFormat::Gray8,
        _ => return 0,
    };

    VideoEngine::expected_frame_size(width, height, frame_format)
}

qntx_ffi_common::define_string_free!(video_string_free);

qntx_ffi_common::define_version_fn!(video_engine_version);

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    #[cfg(not(feature = "onnx"))]
    fn test_engine_lifecycle() {
        // Only test without onnx feature (no model file required)
        let engine = video_engine_new();
        assert!(!engine.is_null());
        video_engine_free(engine);
    }

    #[test]
    fn test_null_engine_handling() {
        let result = video_engine_process_frame(ptr::null(), ptr::null(), 0, 640, 480, 0, 0);
        assert!(!result.success);
        video_result_free(result);
    }

    #[test]
    #[cfg(not(feature = "onnx"))]
    fn test_frame_processing() {
        // Only test without onnx feature (no model file required)
        let engine = video_engine_new();
        assert!(!engine.is_null());

        // Create test frame (RGB8)
        let width = 640u32;
        let height = 480u32;
        let frame_size = (width * height * 3) as usize;
        let frame_data = vec![128u8; frame_size];

        let result = video_engine_process_frame(
            engine,
            frame_data.as_ptr(),
            frame_data.len(),
            width,
            height,
            0, // RGB8
            0,
        );

        assert!(result.success);
        assert_eq!(result.stats.frame_width, width);
        assert_eq!(result.stats.frame_height, height);

        video_result_free(result);
        video_engine_free(engine);
    }

    #[test]
    fn test_expected_frame_size() {
        assert_eq!(video_expected_frame_size(640, 480, 0), 640 * 480 * 3); // RGB8
        assert_eq!(video_expected_frame_size(640, 480, 1), 640 * 480 * 4); // RGBA8
        assert_eq!(video_expected_frame_size(640, 480, 4), 640 * 480); // Gray8
        assert_eq!(video_expected_frame_size(640, 480, 99), 0); // Invalid
    }
}
