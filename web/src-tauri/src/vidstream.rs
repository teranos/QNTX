//! Video processing commands for Tauri desktop app
//!
//! Provides Tauri commands to interact with the vidstream inference engine.
//! Desktop-only - requires ONNX Runtime support.

use qntx_vidstream::types::VideoEngineConfig;
use qntx_vidstream::{FrameFormat, VideoEngine};
use serde::{Deserialize, Serialize};
use std::sync::Mutex;
use tauri::State;

/// Shared video engine state (thread-safe)
pub struct VideoEngineState {
    engine: Mutex<Option<VideoEngine>>,
}

impl VideoEngineState {
    pub fn new() -> Self {
        Self {
            engine: Mutex::new(None),
        }
    }
}

/// Configuration for initializing the video engine (from frontend)
#[derive(Debug, Deserialize)]
pub struct InitConfig {
    pub model_path: String,
    #[serde(default = "default_confidence")]
    pub confidence_threshold: f32,
    #[serde(default = "default_nms")]
    pub nms_threshold: f32,
    #[serde(default = "default_input_size")]
    pub input_width: u32,
    #[serde(default = "default_input_size")]
    pub input_height: u32,
}

fn default_confidence() -> f32 {
    0.5
}
fn default_nms() -> f32 {
    0.45
}
fn default_input_size() -> u32 {
    640
}

/// Detection result (serializable for Tauri)
#[derive(Debug, Serialize)]
pub struct DetectionResult {
    pub class_id: u32,
    pub label: String,
    pub confidence: f32,
    pub bbox: BBox,
    pub track_id: u64,
}

#[derive(Debug, Serialize)]
pub struct BBox {
    pub x: f32,
    pub y: f32,
    pub width: f32,
    pub height: f32,
}

/// Processing result with detections and stats
#[derive(Debug, Serialize)]
pub struct ProcessResult {
    pub detections: Vec<DetectionResult>,
    pub stats: Stats,
}

#[derive(Debug, Serialize)]
pub struct Stats {
    pub preprocess_us: u64,
    pub inference_us: u64,
    pub postprocess_us: u64,
    pub total_us: u64,
    pub detections_raw: u32,
    pub detections_final: u32,
}

/// Engine info (dimensions, ready status)
#[derive(Debug, Serialize)]
pub struct EngineInfo {
    pub ready: bool,
    pub input_width: u32,
    pub input_height: u32,
}

/// Initialize the video engine with a model
#[tauri::command]
pub fn vidstream_init(
    config: InitConfig,
    state: State<VideoEngineState>,
) -> Result<EngineInfo, String> {
    println!(
        "[vidstream] Initializing engine with model: {}",
        config.model_path
    );

    let engine_config = VideoEngineConfig {
        model_path: config.model_path,
        confidence_threshold: config.confidence_threshold,
        nms_threshold: config.nms_threshold,
        input_width: config.input_width,
        input_height: config.input_height,
        num_threads: 0, // auto
        use_gpu: false,
        labels: None,
    };

    let engine = VideoEngine::new(engine_config)?;
    let (width, height) = engine.input_dimensions();
    let ready = engine.is_ready();

    *state.engine.lock().unwrap() = Some(engine);

    println!(
        "[vidstream] Engine initialized: {}x{}, ready={}",
        width, height, ready
    );

    Ok(EngineInfo {
        ready,
        input_width: width,
        input_height: height,
    })
}

/// Process a video frame
#[tauri::command]
pub fn vidstream_process_frame(
    frame_data: Vec<u8>,
    width: u32,
    height: u32,
    format: String,
    timestamp_us: u64,
    state: State<VideoEngineState>,
) -> Result<ProcessResult, String> {
    let mut engine_lock = state.engine.lock().unwrap();
    let engine = engine_lock.as_mut().ok_or("Engine not initialized")?;

    // Parse format
    let frame_format = match format.as_str() {
        "rgb8" => FrameFormat::RGB8,
        "rgba8" => FrameFormat::RGBA8,
        "bgr8" => FrameFormat::BGR8,
        "yuv420" => FrameFormat::YUV420,
        "gray8" => FrameFormat::Gray8,
        _ => return Err(format!("Unknown format: {}", format)),
    };

    let (detections, stats) =
        engine.process_frame(&frame_data, width, height, frame_format, timestamp_us);

    Ok(ProcessResult {
        detections: detections
            .into_iter()
            .map(|d| DetectionResult {
                class_id: d.class_id,
                label: d.label,
                confidence: d.confidence,
                bbox: BBox {
                    x: d.bbox.x,
                    y: d.bbox.y,
                    width: d.bbox.width,
                    height: d.bbox.height,
                },
                track_id: d.track_id,
            })
            .collect(),
        stats: Stats {
            preprocess_us: stats.preprocess_us,
            inference_us: stats.inference_us,
            postprocess_us: stats.postprocess_us,
            total_us: stats.total_us,
            detections_raw: stats.detections_raw,
            detections_final: stats.detections_final,
        },
    })
}

/// Get current engine info
#[tauri::command]
pub fn vidstream_get_info(state: State<VideoEngineState>) -> Result<EngineInfo, String> {
    let engine_lock = state.engine.lock().unwrap();
    let engine = engine_lock.as_ref().ok_or("Engine not initialized")?;

    let (width, height) = engine.input_dimensions();
    let ready = engine.is_ready();

    Ok(EngineInfo {
        ready,
        input_width: width,
        input_height: height,
    })
}
