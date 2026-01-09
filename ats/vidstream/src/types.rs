//! Core types for video processing
//!
//! These types are used internally and converted to C-compatible
//! types in the FFI layer.

/// Supported frame formats for input
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[repr(C)]
pub enum FrameFormat {
    /// RGB with 8 bits per channel (24 bits per pixel)
    RGB8 = 0,
    /// RGBA with 8 bits per channel (32 bits per pixel)
    RGBA8 = 1,
    /// BGR with 8 bits per channel (OpenCV default)
    BGR8 = 2,
    /// YUV420 planar (common video format)
    YUV420 = 3,
    /// Grayscale 8-bit
    Gray8 = 4,
}

impl Default for FrameFormat {
    fn default() -> Self {
        Self::RGB8
    }
}

/// Bounding box for detected objects
#[derive(Debug, Clone, Copy, Default)]
pub struct BoundingBox {
    /// X coordinate of top-left corner (pixels)
    pub x: f32,
    /// Y coordinate of top-left corner (pixels)
    pub y: f32,
    /// Width of bounding box (pixels)
    pub width: f32,
    /// Height of bounding box (pixels)
    pub height: f32,
}

impl BoundingBox {
    pub fn new(x: f32, y: f32, width: f32, height: f32) -> Self {
        Self {
            x,
            y,
            width,
            height,
        }
    }

    /// Calculate intersection over union with another box
    pub fn iou(&self, other: &BoundingBox) -> f32 {
        let x1 = self.x.max(other.x);
        let y1 = self.y.max(other.y);
        let x2 = (self.x + self.width).min(other.x + other.width);
        let y2 = (self.y + self.height).min(other.y + other.height);

        if x2 <= x1 || y2 <= y1 {
            return 0.0;
        }

        let intersection = (x2 - x1) * (y2 - y1);
        let area_self = self.width * self.height;
        let area_other = other.width * other.height;
        let union = area_self + area_other - intersection;

        if union > 0.0 {
            intersection / union
        } else {
            0.0
        }
    }

    /// Calculate area
    pub fn area(&self) -> f32 {
        self.width * self.height
    }
}

/// A single detection result
#[derive(Debug, Clone)]
pub struct Detection {
    /// Class/label ID (model-specific)
    pub class_id: u32,
    /// Human-readable label (if available)
    pub label: String,
    /// Confidence score 0.0-1.0
    pub confidence: f32,
    /// Bounding box in pixel coordinates
    pub bbox: BoundingBox,
    /// Track ID for object tracking (0 if not tracked)
    pub track_id: u64,
}

impl Detection {
    pub fn new(
        class_id: u32,
        label: impl Into<String>,
        confidence: f32,
        bbox: BoundingBox,
    ) -> Self {
        Self {
            class_id,
            label: label.into(),
            confidence,
            bbox,
            track_id: 0,
        }
    }

    pub fn with_track_id(mut self, track_id: u64) -> Self {
        self.track_id = track_id;
        self
    }
}

/// Processing statistics for performance monitoring
#[derive(Debug, Clone, Copy, Default)]
pub struct ProcessingStats {
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

/// Configuration for video engine initialization
#[derive(Debug, Clone)]
pub struct VideoEngineConfig {
    /// Path to ONNX model file
    pub model_path: String,
    /// Confidence threshold for detections (0.0-1.0)
    pub confidence_threshold: f32,
    /// IoU threshold for NMS (0.0-1.0)
    pub nms_threshold: f32,
    /// Model input width (0 for auto-detect from model)
    pub input_width: u32,
    /// Model input height (0 for auto-detect from model)
    pub input_height: u32,
    /// Number of inference threads (0 for auto)
    pub num_threads: u32,
    /// Enable GPU inference if available
    pub use_gpu: bool,
    /// Class labels (newline-separated or JSON array)
    pub labels: Option<String>,
}

impl Default for VideoEngineConfig {
    fn default() -> Self {
        Self {
            model_path: String::new(),
            confidence_threshold: 0.5,
            nms_threshold: 0.45,
            input_width: 640,
            input_height: 640,
            num_threads: 0, // auto
            use_gpu: false,
            labels: None,
        }
    }
}
