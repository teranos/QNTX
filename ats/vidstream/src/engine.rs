//! Video processing engine
//!
//! Core engine for frame-by-frame video processing. Designed for
//! ultra-low latency with minimal allocations in the hot path.

use std::time::Instant;

use parking_lot::RwLock;

use crate::types::{Detection, FrameFormat, ProcessingStats, VideoEngineConfig};

/// Thread-safe video processing engine
pub struct VideoEngine {
    config: VideoEngineConfig,
    labels: Vec<String>,
    /// Reusable buffers to minimize allocations
    state: RwLock<EngineState>,
}

struct EngineState {
    /// Preprocessed frame buffer (resized to model input)
    input_buffer: Vec<f32>,
    /// Raw detection buffer before NMS
    raw_detections: Vec<Detection>,
    /// Frame counter for tracking
    frame_count: u64,
}

impl VideoEngine {
    /// Create a new video engine with the given configuration.
    ///
    /// # Arguments
    /// * `config` - Engine configuration including model path and thresholds
    ///
    /// # Returns
    /// * `Ok(VideoEngine)` - Successfully initialized engine
    /// * `Err(String)` - Initialization failed (model not found, invalid config, etc.)
    pub fn new(config: VideoEngineConfig) -> Result<Self, String> {
        // Validate configuration
        if config.confidence_threshold < 0.0 || config.confidence_threshold > 1.0 {
            return Err("confidence_threshold must be between 0.0 and 1.0".to_string());
        }
        if config.nms_threshold < 0.0 || config.nms_threshold > 1.0 {
            return Err("nms_threshold must be between 0.0 and 1.0".to_string());
        }

        // Parse labels if provided
        let labels = if let Some(ref labels_str) = config.labels {
            parse_labels(labels_str)
        } else {
            default_coco_labels()
        };

        // Pre-allocate buffers for the expected input size
        let input_size = (config.input_width * config.input_height * 3) as usize;
        let input_buffer = vec![0.0f32; input_size];

        // TODO: When onnx feature is enabled, load the model here
        // For now, we provide a working stub that demonstrates the API

        Ok(Self {
            config,
            labels,
            state: RwLock::new(EngineState {
                input_buffer,
                raw_detections: Vec::with_capacity(100),
                frame_count: 0,
            }),
        })
    }

    /// Process a single frame and return detections.
    ///
    /// This is the hot path - designed for minimal latency.
    ///
    /// # Arguments
    /// * `frame_data` - Raw pixel data
    /// * `width` - Frame width in pixels
    /// * `height` - Frame height in pixels
    /// * `format` - Pixel format of input data
    /// * `timestamp_us` - Frame timestamp in microseconds (for tracking)
    ///
    /// # Returns
    /// Tuple of (detections, processing_stats)
    pub fn process_frame(
        &self,
        frame_data: &[u8],
        width: u32,
        height: u32,
        format: FrameFormat,
        timestamp_us: u64,
    ) -> (Vec<Detection>, ProcessingStats) {
        let total_start = Instant::now();
        let mut stats = ProcessingStats {
            frame_width: width,
            frame_height: height,
            ..Default::default()
        };

        // Validate input
        let expected_size = Self::expected_frame_size(width, height, format);
        if frame_data.len() < expected_size {
            // Return empty results for invalid input
            return (Vec::new(), stats);
        }

        // === PREPROCESSING ===
        let preprocess_start = Instant::now();

        let mut state = self.state.write();
        state.frame_count += 1;

        // Resize and normalize frame to model input
        self.preprocess_frame(frame_data, width, height, format, &mut state.input_buffer);

        stats.preprocess_us = preprocess_start.elapsed().as_micros() as u64;

        // === INFERENCE ===
        let inference_start = Instant::now();

        // Run model inference
        // Note: We pass the whole state to avoid split borrow issues
        Self::run_inference(&mut state);

        stats.inference_us = inference_start.elapsed().as_micros() as u64;
        stats.detections_raw = state.raw_detections.len() as u32;

        // === POST-PROCESSING ===
        let postprocess_start = Instant::now();

        // Apply NMS and threshold filtering
        let detections = self.postprocess_detections(
            &state.raw_detections,
            width,
            height,
            timestamp_us,
        );

        stats.postprocess_us = postprocess_start.elapsed().as_micros() as u64;
        stats.detections_final = detections.len() as u32;
        stats.total_us = total_start.elapsed().as_micros() as u64;

        (detections, stats)
    }

    /// Get the expected frame data size for a given format
    pub fn expected_frame_size(width: u32, height: u32, format: FrameFormat) -> usize {
        let pixels = (width * height) as usize;
        match format {
            FrameFormat::RGB8 | FrameFormat::BGR8 => pixels * 3,
            FrameFormat::RGBA8 => pixels * 4,
            FrameFormat::YUV420 => pixels + (pixels / 2), // Y + UV
            FrameFormat::Gray8 => pixels,
        }
    }

    /// Check if the engine is ready for inference
    pub fn is_ready(&self) -> bool {
        // TODO: Check if model is loaded when onnx feature is enabled
        true
    }

    /// Get the model input dimensions
    pub fn input_dimensions(&self) -> (u32, u32) {
        (self.config.input_width, self.config.input_height)
    }

    /// Get label for a class ID
    pub fn get_label(&self, class_id: u32) -> Option<&str> {
        self.labels.get(class_id as usize).map(|s| s.as_str())
    }

    // === Private Methods ===

    fn preprocess_frame(
        &self,
        frame_data: &[u8],
        width: u32,
        height: u32,
        format: FrameFormat,
        output: &mut [f32],
    ) {
        let (target_w, target_h) = (self.config.input_width, self.config.input_height);

        // Simple bilinear resize and normalize to [0, 1]
        // In production, this would use SIMD-optimized code

        let scale_x = width as f32 / target_w as f32;
        let scale_y = height as f32 / target_h as f32;

        let channels = match format {
            FrameFormat::RGB8 | FrameFormat::BGR8 => 3,
            FrameFormat::RGBA8 => 4,
            FrameFormat::Gray8 => 1,
            FrameFormat::YUV420 => 3, // Will be converted
        };

        for y in 0..target_h {
            for x in 0..target_w {
                let src_x = ((x as f32 + 0.5) * scale_x - 0.5).max(0.0).min((width - 1) as f32);
                let src_y = ((y as f32 + 0.5) * scale_y - 0.5).max(0.0).min((height - 1) as f32);

                let src_xi = src_x as u32;
                let src_yi = src_y as u32;

                let src_idx = ((src_yi * width + src_xi) * channels as u32) as usize;
                let dst_idx = ((y * target_w + x) * 3) as usize;

                if src_idx + 2 < frame_data.len() && dst_idx + 2 < output.len() {
                    // Normalize to [0, 1]
                    match format {
                        FrameFormat::BGR8 => {
                            // BGR to RGB
                            output[dst_idx] = frame_data[src_idx + 2] as f32 / 255.0;
                            output[dst_idx + 1] = frame_data[src_idx + 1] as f32 / 255.0;
                            output[dst_idx + 2] = frame_data[src_idx] as f32 / 255.0;
                        }
                        FrameFormat::RGB8 => {
                            output[dst_idx] = frame_data[src_idx] as f32 / 255.0;
                            output[dst_idx + 1] = frame_data[src_idx + 1] as f32 / 255.0;
                            output[dst_idx + 2] = frame_data[src_idx + 2] as f32 / 255.0;
                        }
                        FrameFormat::RGBA8 => {
                            output[dst_idx] = frame_data[src_idx] as f32 / 255.0;
                            output[dst_idx + 1] = frame_data[src_idx + 1] as f32 / 255.0;
                            output[dst_idx + 2] = frame_data[src_idx + 2] as f32 / 255.0;
                        }
                        FrameFormat::Gray8 => {
                            let v = frame_data[src_idx] as f32 / 255.0;
                            output[dst_idx] = v;
                            output[dst_idx + 1] = v;
                            output[dst_idx + 2] = v;
                        }
                        FrameFormat::YUV420 => {
                            // Simplified - just use Y channel as grayscale
                            let v = frame_data[src_idx] as f32 / 255.0;
                            output[dst_idx] = v;
                            output[dst_idx + 1] = v;
                            output[dst_idx + 2] = v;
                        }
                    }
                }
            }
        }
    }

    fn run_inference(state: &mut EngineState) {
        // Clear previous detections
        state.raw_detections.clear();

        // TODO: When onnx feature is enabled, run actual model inference
        //
        // For now, this is a stub that demonstrates the API structure.
        // In production, this would:
        // 1. Copy state.input_buffer to ONNX tensor
        // 2. Run session.run()
        // 3. Parse output tensors into state.raw_detections
        //
        // Example with ort crate:
        // ```rust
        // let outputs = session.run(ort::inputs![&state.input_buffer]?)?;
        // let output = outputs[0].extract_tensor::<f32>()?;
        // // Parse YOLO output format into state.raw_detections...
        // ```

        // Stub: return no detections
        // Access input_buffer to silence unused warning
        let _ = &state.input_buffer;
    }

    fn postprocess_detections(
        &self,
        raw_detections: &[Detection],
        frame_width: u32,
        frame_height: u32,
        _timestamp_us: u64,
    ) -> Vec<Detection> {
        // Filter by confidence threshold
        let mut filtered: Vec<Detection> = raw_detections
            .iter()
            .filter(|d| d.confidence >= self.config.confidence_threshold)
            .cloned()
            .collect();

        // Sort by confidence (descending)
        filtered.sort_by(|a, b| b.confidence.partial_cmp(&a.confidence).unwrap());

        // Apply Non-Maximum Suppression
        let mut keep = Vec::new();
        let mut suppressed = vec![false; filtered.len()];

        for i in 0..filtered.len() {
            if suppressed[i] {
                continue;
            }

            let mut det = filtered[i].clone();

            // Scale bbox to original frame size
            det.bbox.x *= frame_width as f32 / self.config.input_width as f32;
            det.bbox.y *= frame_height as f32 / self.config.input_height as f32;
            det.bbox.width *= frame_width as f32 / self.config.input_width as f32;
            det.bbox.height *= frame_height as f32 / self.config.input_height as f32;

            // Add label if we have it
            if det.label.is_empty() {
                if let Some(label) = self.get_label(det.class_id) {
                    det.label = label.to_string();
                }
            }

            keep.push(det);

            // Suppress overlapping detections of same class
            for j in (i + 1)..filtered.len() {
                if suppressed[j] {
                    continue;
                }
                if filtered[i].class_id == filtered[j].class_id {
                    let iou = filtered[i].bbox.iou(&filtered[j].bbox);
                    if iou > self.config.nms_threshold {
                        suppressed[j] = true;
                    }
                }
            }
        }

        keep
    }
}

/// Parse labels from string (newline-separated or JSON array)
fn parse_labels(labels_str: &str) -> Vec<String> {
    let trimmed = labels_str.trim();

    // Try JSON array first
    if trimmed.starts_with('[') {
        if let Ok(labels) = serde_json::from_str::<Vec<String>>(trimmed) {
            return labels;
        }
    }

    // Fall back to newline-separated
    trimmed.lines().map(|s| s.trim().to_string()).collect()
}

/// Default COCO class labels
fn default_coco_labels() -> Vec<String> {
    vec![
        "person", "bicycle", "car", "motorcycle", "airplane", "bus", "train", "truck",
        "boat", "traffic light", "fire hydrant", "stop sign", "parking meter", "bench",
        "bird", "cat", "dog", "horse", "sheep", "cow", "elephant", "bear", "zebra",
        "giraffe", "backpack", "umbrella", "handbag", "tie", "suitcase", "frisbee",
        "skis", "snowboard", "sports ball", "kite", "baseball bat", "baseball glove",
        "skateboard", "surfboard", "tennis racket", "bottle", "wine glass", "cup",
        "fork", "knife", "spoon", "bowl", "banana", "apple", "sandwich", "orange",
        "broccoli", "carrot", "hot dog", "pizza", "donut", "cake", "chair", "couch",
        "potted plant", "bed", "dining table", "toilet", "tv", "laptop", "mouse",
        "remote", "keyboard", "cell phone", "microwave", "oven", "toaster", "sink",
        "refrigerator", "book", "clock", "vase", "scissors", "teddy bear", "hair drier",
        "toothbrush",
    ]
    .into_iter()
    .map(String::from)
    .collect()
}

// Need serde_json for label parsing
use serde_json;

#[cfg(test)]
mod tests {
    use super::*;
    use crate::types::BoundingBox;

    #[test]
    fn test_engine_creation() {
        let config = VideoEngineConfig::default();
        let engine = VideoEngine::new(config).unwrap();
        assert!(engine.is_ready());
    }

    #[test]
    fn test_invalid_threshold() {
        let mut config = VideoEngineConfig::default();
        config.confidence_threshold = 1.5;
        assert!(VideoEngine::new(config).is_err());
    }

    #[test]
    fn test_bounding_box_iou() {
        let box1 = BoundingBox::new(0.0, 0.0, 100.0, 100.0);
        let box2 = BoundingBox::new(50.0, 50.0, 100.0, 100.0);

        let iou = box1.iou(&box2);
        // Intersection: 50x50 = 2500
        // Union: 100x100 + 100x100 - 2500 = 17500
        // IoU = 2500 / 17500 ≈ 0.143
        assert!((iou - 0.143).abs() < 0.01);
    }

    #[test]
    fn test_frame_size_calculation() {
        assert_eq!(VideoEngine::expected_frame_size(640, 480, FrameFormat::RGB8), 640 * 480 * 3);
        assert_eq!(VideoEngine::expected_frame_size(640, 480, FrameFormat::RGBA8), 640 * 480 * 4);
        assert_eq!(VideoEngine::expected_frame_size(640, 480, FrameFormat::Gray8), 640 * 480);
    }

    #[test]
    fn test_label_parsing() {
        let newline = "person\ncar\nbike";
        let labels = parse_labels(newline);
        assert_eq!(labels, vec!["person", "car", "bike"]);

        let json = r#"["person", "car", "bike"]"#;
        let labels = parse_labels(json);
        assert_eq!(labels, vec!["person", "car", "bike"]);
    }
}
