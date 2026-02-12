//! Video processing engine
//!
//! Core engine for frame-by-frame video processing. Designed for
//! ultra-low latency with minimal allocations in the hot path.

use qntx_grpc::error::Error;
use std::time::Instant;

#[cfg(feature = "onnx")]
use parking_lot::Mutex;
use parking_lot::RwLock;

#[cfg(feature = "onnx")]
use crate::types::BoundingBox;
use crate::types::{Detection, FrameFormat, ProcessingStats, VideoEngineConfig};

#[cfg(feature = "onnx")]
use ort::{
    session::{builder::GraphOptimizationLevel, Session},
    value::Value,
};
#[cfg(feature = "onnx")]
use std::fs;

/// Thread-safe video processing engine
pub struct VideoEngine {
    config: VideoEngineConfig,
    labels: Vec<String>,
    /// ONNX inference session (when onnx feature is enabled)
    /// Wrapped in Mutex because Session::run() requires &mut self
    #[cfg(feature = "onnx")]
    session: Mutex<Session>,
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
    /// * `Err(Error)` - Initialization failed (model not found, invalid config, etc.)
    pub fn new(config: VideoEngineConfig) -> Result<Self, Error> {
        // Validate configuration
        if config.confidence_threshold < 0.0 || config.confidence_threshold > 1.0 {
            return Err(Error::internal(
                "confidence_threshold must be between 0.0 and 1.0",
            ));
        }
        if config.nms_threshold < 0.0 || config.nms_threshold > 1.0 {
            return Err(Error::internal("nms_threshold must be between 0.0 and 1.0"));
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

        // Load ONNX model when feature is enabled
        #[cfg(feature = "onnx")]
        let session = {
            if config.model_path.is_empty() {
                return Err(Error::internal(
                    "model_path is required when onnx feature is enabled",
                ));
            }

            // Load model file into memory
            // TODO: Update to commit_from_file when qntx-inference branch uses ort rc.11
            let model_bytes = fs::read(&config.model_path).map_err(|e| {
                Error::context(
                    format!("failed to read model file '{}'", config.model_path),
                    e,
                )
            })?;

            // Build session with model bytes
            Session::builder()
                .map_err(|e| Error::context("failed to create ONNX session builder", e))?
                .with_optimization_level(GraphOptimizationLevel::Level3)
                .map_err(|e| Error::context("failed to set ONNX optimization level", e))?
                .with_intra_threads(if config.num_threads > 0 {
                    config.num_threads as usize
                } else {
                    1
                })
                .map_err(|e| Error::context("failed to set ONNX thread count", e))?
                .commit_from_memory(&model_bytes)
                .map_err(|e| {
                    Error::context(
                        format!("failed to load ONNX model from '{}'", config.model_path),
                        e,
                    )
                })?
        };

        Ok(Self {
            config,
            labels,
            #[cfg(feature = "onnx")]
            session: Mutex::new(session),
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
        self.run_inference(&mut state);

        stats.inference_us = inference_start.elapsed().as_micros() as u64;
        stats.detections_raw = state.raw_detections.len() as u32;

        // === POST-PROCESSING ===
        let postprocess_start = Instant::now();

        // Apply NMS and threshold filtering
        let detections =
            self.postprocess_detections(&state.raw_detections, width, height, timestamp_us);

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
        #[cfg(feature = "onnx")]
        {
            // Model is loaded during construction, so always ready
            true
        }
        #[cfg(not(feature = "onnx"))]
        {
            // Without ONNX, we're in stub mode
            false
        }
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
                let src_x = ((x as f32 + 0.5) * scale_x - 0.5)
                    .max(0.0)
                    .min((width - 1) as f32);
                let src_y = ((y as f32 + 0.5) * scale_y - 0.5)
                    .max(0.0)
                    .min((height - 1) as f32);

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

    fn run_inference(&self, state: &mut EngineState) {
        // Clear previous detections
        state.raw_detections.clear();

        #[cfg(feature = "onnx")]
        {
            // Convert input buffer to NCHW format [1, 3, H, W]
            // input_buffer is currently in HWC format (height * width * 3)
            let h = self.config.input_height as usize;
            let w = self.config.input_width as usize;

            // Reshape from HWC to CHW (ONNX expects channels-first)
            let mut chw_buffer = vec![0.0f32; h * w * 3];
            for y in 0..h {
                for x in 0..w {
                    let hwc_idx = (y * w + x) * 3;
                    let hw_idx = y * w + x;
                    // R channel
                    chw_buffer[hw_idx] = state.input_buffer[hwc_idx];
                    // G channel
                    chw_buffer[h * w + hw_idx] = state.input_buffer[hwc_idx + 1];
                    // B channel
                    chw_buffer[2 * h * w + hw_idx] = state.input_buffer[hwc_idx + 2];
                }
            }

            // Create ONNX value directly from shape and data
            // Shape: [1, 3, H, W]
            let shape_vec = vec![1i64, 3, h as i64, w as i64];

            // Convert to ONNX Value
            // TODO: Simplify when ort rc.11 has better ndarray support
            let input_value = match Value::from_array((shape_vec.as_slice(), chw_buffer)) {
                Ok(v) => v,
                Err(_) => return,
            };

            // Run inference (lock session for mutable access)
            let mut session = self.session.lock();
            let outputs = match session.run(ort::inputs![input_value]) {
                Ok(outputs) => outputs,
                Err(_) => return, // Inference failed, return no detections
            };

            // Extract output tensor (YOLOv8 format: [1, 84, 8400])
            // 84 = 4 (bbox) + 80 (class scores)
            // 8400 = number of detection candidates
            let (shape, data_slice) = match outputs[0].try_extract_tensor::<f32>() {
                Ok(tensor) => tensor,
                Err(_) => return,
            };

            // Verify expected output shape [1, 84, 8400]
            if shape.len() != 3 || shape[0] != 1 {
                return;
            }

            let num_features = shape[1] as usize; // 84 = 4 bbox + 80 classes
            let num_detections = shape[2] as usize; // 8400 candidates
            let num_classes = num_features - 4; // 80 classes

            // Helper function to index into flat output slice
            // For shape [1, 84, 8400], flat index = feature * 8400 + detection
            let get_value = |feature: usize, detection: usize| -> f32 {
                data_slice[feature * num_detections + detection]
            };

            // Parse YOLOv8 output
            for i in 0..num_detections {
                // Extract bounding box (center_x, center_y, width, height)
                let cx = get_value(0, i);
                let cy = get_value(1, i);
                let w = get_value(2, i);
                let h = get_value(3, i);

                // Find best class and confidence
                let mut best_class = 0;
                let mut best_conf = 0.0f32;
                for c in 0..num_classes {
                    let conf = get_value(4 + c, i);
                    if conf > best_conf {
                        best_conf = conf;
                        best_class = c;
                    }
                }

                // Only keep detections above a pre-NMS threshold
                // (final threshold is applied in postprocessing)
                if best_conf > 0.25 {
                    let detection = Detection {
                        class_id: best_class as u32,
                        label: self
                            .get_label(best_class as u32)
                            .unwrap_or("unknown")
                            .to_string(),
                        confidence: best_conf,
                        bbox: BoundingBox {
                            x: cx - w / 2.0, // Convert center to top-left
                            y: cy - h / 2.0,
                            width: w,
                            height: h,
                        },
                        track_id: 0,
                    };
                    state.raw_detections.push(detection);
                }
            }
        }

        #[cfg(not(feature = "onnx"))]
        {
            // Stub mode: no detections
            // Access input_buffer to silence unused warning
            let _ = &state.input_buffer;
        }
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
        "person",
        "bicycle",
        "car",
        "motorcycle",
        "airplane",
        "bus",
        "train",
        "truck",
        "boat",
        "traffic light",
        "fire hydrant",
        "stop sign",
        "parking meter",
        "bench",
        "bird",
        "cat",
        "dog",
        "horse",
        "sheep",
        "cow",
        "elephant",
        "bear",
        "zebra",
        "giraffe",
        "backpack",
        "umbrella",
        "handbag",
        "tie",
        "suitcase",
        "frisbee",
        "skis",
        "snowboard",
        "sports ball",
        "kite",
        "baseball bat",
        "baseball glove",
        "skateboard",
        "surfboard",
        "tennis racket",
        "bottle",
        "wine glass",
        "cup",
        "fork",
        "knife",
        "spoon",
        "bowl",
        "banana",
        "apple",
        "sandwich",
        "orange",
        "broccoli",
        "carrot",
        "hot dog",
        "pizza",
        "donut",
        "cake",
        "chair",
        "couch",
        "potted plant",
        "bed",
        "dining table",
        "toilet",
        "tv",
        "laptop",
        "mouse",
        "remote",
        "keyboard",
        "cell phone",
        "microwave",
        "oven",
        "toaster",
        "sink",
        "refrigerator",
        "book",
        "clock",
        "vase",
        "scissors",
        "teddy bear",
        "hair drier",
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
    #[cfg(not(feature = "onnx"))]
    fn test_engine_creation() {
        // Only test without onnx feature (stub mode)
        let config = VideoEngineConfig::default();
        let engine = VideoEngine::new(config).unwrap();
        assert!(!engine.is_ready()); // Stub mode
    }

    #[test]
    fn test_invalid_threshold() {
        let config = VideoEngineConfig {
            confidence_threshold: 1.5,
            ..Default::default()
        };
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
        assert_eq!(
            VideoEngine::expected_frame_size(640, 480, FrameFormat::RGB8),
            640 * 480 * 3
        );
        assert_eq!(
            VideoEngine::expected_frame_size(640, 480, FrameFormat::RGBA8),
            640 * 480 * 4
        );
        assert_eq!(
            VideoEngine::expected_frame_size(640, 480, FrameFormat::Gray8),
            640 * 480
        );
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

    #[test]
    #[cfg(not(feature = "onnx"))]
    fn test_nms_filters_overlapping_detections() {
        // Test that NMS suppresses overlapping detections of the same class
        let config = VideoEngineConfig {
            confidence_threshold: 0.25,
            nms_threshold: 0.5,
            input_width: 640,
            input_height: 640,
            ..Default::default()
        };
        let engine = VideoEngine::new(config).unwrap();

        // Create overlapping detections of the same class (person = 0)
        let det1 = Detection {
            class_id: 0,
            label: "person".to_string(),
            confidence: 0.9,
            bbox: BoundingBox::new(100.0, 100.0, 200.0, 300.0),
            track_id: 0,
        };
        let det2 = Detection {
            class_id: 0,
            label: "person".to_string(),
            confidence: 0.7, // Lower confidence, should be suppressed
            bbox: BoundingBox::new(120.0, 110.0, 200.0, 300.0), // Overlaps with det1
            track_id: 0,
        };
        let det3 = Detection {
            class_id: 0,
            label: "person".to_string(),
            confidence: 0.85,
            bbox: BoundingBox::new(400.0, 100.0, 150.0, 250.0), // No overlap, should keep
            track_id: 0,
        };

        let raw_detections = vec![det1, det2, det3];
        let result = engine.postprocess_detections(&raw_detections, 1280, 720, 0);

        // Should keep 2 detections (det1 and det3), det2 should be suppressed
        assert_eq!(result.len(), 2);
        assert!((result[0].confidence - 0.9).abs() < 0.01); // Highest confidence kept
        assert!((result[1].confidence - 0.85).abs() < 0.01);
    }

    #[test]
    #[cfg(not(feature = "onnx"))]
    fn test_nms_keeps_different_classes() {
        // Test that NMS doesn't suppress detections of different classes
        let config = VideoEngineConfig {
            confidence_threshold: 0.25,
            nms_threshold: 0.5,
            input_width: 640,
            input_height: 640,
            ..Default::default()
        };
        let engine = VideoEngine::new(config).unwrap();

        // Create overlapping detections of DIFFERENT classes
        let det1 = Detection {
            class_id: 0, // person
            label: "person".to_string(),
            confidence: 0.9,
            bbox: BoundingBox::new(100.0, 100.0, 200.0, 300.0),
            track_id: 0,
        };
        let det2 = Detection {
            class_id: 2, // car
            label: "car".to_string(),
            confidence: 0.8,
            bbox: BoundingBox::new(110.0, 110.0, 200.0, 300.0), // Overlaps but different class
            track_id: 0,
        };

        let raw_detections = vec![det1, det2];
        let result = engine.postprocess_detections(&raw_detections, 1280, 720, 0);

        // Should keep both detections (different classes)
        assert_eq!(result.len(), 2);
    }

    #[test]
    #[cfg(not(feature = "onnx"))]
    fn test_confidence_threshold_filtering() {
        // Test that low confidence detections are filtered out
        let config = VideoEngineConfig {
            confidence_threshold: 0.6, // Higher threshold
            nms_threshold: 0.5,
            input_width: 640,
            input_height: 640,
            ..Default::default()
        };
        let engine = VideoEngine::new(config).unwrap();

        let det1 = Detection {
            class_id: 0,
            label: "person".to_string(),
            confidence: 0.9, // Above threshold, should keep
            bbox: BoundingBox::new(100.0, 100.0, 200.0, 300.0),
            track_id: 0,
        };
        let det2 = Detection {
            class_id: 0,
            label: "person".to_string(),
            confidence: 0.4, // Below threshold, should filter out
            bbox: BoundingBox::new(400.0, 100.0, 150.0, 250.0),
            track_id: 0,
        };

        let raw_detections = vec![det1, det2];
        let result = engine.postprocess_detections(&raw_detections, 1280, 720, 0);

        // Should keep only 1 detection
        assert_eq!(result.len(), 1);
        assert!((result[0].confidence - 0.9).abs() < 0.01);
    }

    #[test]
    #[cfg(not(feature = "onnx"))]
    fn test_bbox_scaling() {
        // Test that bounding boxes are scaled from model input to frame size
        let config = VideoEngineConfig {
            confidence_threshold: 0.25,
            nms_threshold: 0.5,
            input_width: 640, // Model input
            input_height: 640,
            ..Default::default()
        };
        let engine = VideoEngine::new(config).unwrap();

        let det = Detection {
            class_id: 0,
            label: "person".to_string(),
            confidence: 0.9,
            bbox: BoundingBox::new(320.0, 320.0, 100.0, 100.0), // Center of 640x640
            track_id: 0,
        };

        let raw_detections = vec![det];
        let result = engine.postprocess_detections(&raw_detections, 1280, 1280, 0); // 2x scale

        // Bbox should be scaled by 2x (1280/640)
        assert_eq!(result.len(), 1);
        assert!((result[0].bbox.x - 640.0).abs() < 0.1);
        assert!((result[0].bbox.y - 640.0).abs() < 0.1);
        assert!((result[0].bbox.width - 200.0).abs() < 0.1);
        assert!((result[0].bbox.height - 200.0).abs() < 0.1);
    }
}
