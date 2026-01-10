//! Video processing commands for Tauri desktop app
//!
//! Provides Tauri commands to interact with the vidstream inference engine.
//! Desktop-only - requires ONNX Runtime support and nokhwa for native camera access.
//! The entire module is conditionally compiled in main.rs.

use nokhwa::pixel_format::RgbFormat;
use nokhwa::utils::{
    ApiBackend, CameraFormat, CameraIndex, FrameFormat as NokhwaFrameFormat, RequestedFormat,
    RequestedFormatType, Resolution,
};
use nokhwa::{query, Camera};
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

/// Shared camera state (thread-safe)
pub struct CameraState {
    camera: Mutex<Option<Camera>>,
}

impl CameraState {
    pub fn new() -> Self {
        Self {
            camera: Mutex::new(None),
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
    println!(
        "[vidstream] Config: confidence={}, nms={}, input={}x{}",
        config.confidence_threshold, config.nms_threshold, config.input_width, config.input_height
    );

    let engine_config = VideoEngineConfig {
        model_path: config.model_path.clone(),
        confidence_threshold: config.confidence_threshold,
        nms_threshold: config.nms_threshold,
        input_width: config.input_width,
        input_height: config.input_height,
        num_threads: 0, // auto
        use_gpu: false,
        labels: None,
    };

    println!("[vidstream] Creating VideoEngine...");
    let engine = match VideoEngine::new(engine_config) {
        Ok(e) => {
            println!("[vidstream] VideoEngine created successfully");
            e
        }
        Err(e) => {
            println!("[vidstream] ERROR creating VideoEngine: {}", e);
            return Err(format!("Failed to create engine: {}", e));
        }
    };

    let (width, height) = engine.input_dimensions();
    let ready = engine.is_ready();

    println!(
        "[vidstream] Engine dimensions: {}x{}, ready={}",
        width, height, ready
    );

    *state.engine.lock().unwrap() = Some(engine);

    println!("[vidstream] Engine stored in state, initialization complete");

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

// === Camera Commands ===

#[derive(Debug, Serialize)]
pub struct CameraDevice {
    pub index: usize,
    pub name: String,
    pub description: String,
}

#[derive(Debug, Serialize)]
pub struct CameraFrame {
    pub data: Vec<u8>,
    pub width: u32,
    pub height: u32,
    pub format: String,
    pub timestamp_us: u64,
}

/// List available cameras
#[tauri::command]
pub fn vidstream_list_cameras() -> Result<Vec<CameraDevice>, String> {
    println!("[vidstream] Listing available cameras");

    // Use platform-specific backend (AVFoundation on macOS, etc.)
    #[cfg(target_os = "macos")]
    let backend = ApiBackend::AVFoundation;
    #[cfg(target_os = "windows")]
    let backend = ApiBackend::MediaFoundation;
    #[cfg(target_os = "linux")]
    let backend = ApiBackend::Video4Linux;

    let cameras = query(backend).map_err(|e| format!("Failed to query cameras: {}", e))?;

    let devices: Vec<CameraDevice> = cameras
        .into_iter()
        .enumerate()
        .map(|(i, info)| CameraDevice {
            index: i,
            name: info.human_name().to_string(),
            description: info.description().to_string(),
        })
        .collect();

    println!("[vidstream] Found {} cameras", devices.len());
    Ok(devices)
}

/// Start camera capture
#[tauri::command]
pub fn vidstream_start_camera(
    camera_index: usize,
    width: u32,
    height: u32,
    state: State<CameraState>,
) -> Result<(), String> {
    println!(
        "[vidstream] Starting camera {} with resolution {}x{}",
        camera_index, width, height
    );

    // Create camera format with RGB format (most compatible)
    let requested_format = RequestedFormat::new::<RgbFormat>(RequestedFormatType::Exact(
        CameraFormat::new(Resolution::new(width, height), NokhwaFrameFormat::MJPEG, 30),
    ));

    let mut camera = Camera::new(CameraIndex::Index(camera_index as u32), requested_format)
        .map_err(|e| format!("Failed to open camera: {}", e))?;

    // Open the camera stream
    camera
        .open_stream()
        .map_err(|e| format!("Failed to start stream: {}", e))?;

    println!("[vidstream] Camera opened successfully");
    *state.camera.lock().unwrap() = Some(camera);

    Ok(())
}

/// Stop camera capture
#[tauri::command]
pub fn vidstream_stop_camera(state: State<CameraState>) -> Result<(), String> {
    println!("[vidstream] Stopping camera");
    let mut camera_lock = state.camera.lock().unwrap();
    *camera_lock = None;
    println!("[vidstream] Camera stopped");
    Ok(())
}

/// Get next camera frame
#[tauri::command]
pub fn vidstream_get_frame(state: State<CameraState>) -> Result<CameraFrame, String> {
    let mut camera_lock = state.camera.lock().unwrap();
    let camera = camera_lock.as_mut().ok_or("Camera not started")?;

    let frame = camera
        .frame()
        .map_err(|e| format!("Failed to get frame: {}", e))?;

    let timestamp_us = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap()
        .as_micros() as u64;

    // Decode frame to RGB
    let decoded = frame
        .decode_image::<RgbFormat>()
        .map_err(|e| format!("Failed to decode frame: {}", e))?;

    Ok(CameraFrame {
        data: decoded.into_raw(),
        width: frame.resolution().width(),
        height: frame.resolution().height(),
        format: "rgb8".to_string(),
        timestamp_us,
    })
}
