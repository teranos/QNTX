//! HTTP endpoint handlers for the VidStream plugin
//!
//! Implements handlers for /init, /frame, /status, and the glyph module.

use crate::proto::{HttpHeader, HttpResponse};
use parking_lot::RwLock;
use qntx_vidstream::types::{FrameFormat, VideoEngineConfig};
use qntx_vidstream::VideoEngine;
use serde::{Deserialize, Serialize};
use std::sync::Arc;
use tonic::Status;
use tracing::{debug, info};

/// Embedded glyph module JS — served at GET /vidstream-glyph-module.js
const GLYPH_MODULE_JS: &str = include_str!("../web/vidstream-glyph-module.js");

/// Plugin state shared across handlers
pub(crate) struct PluginState {
    pub engine: Option<VideoEngine>,
    pub default_model_path: Option<String>,
}

/// Handler context providing access to plugin state
pub struct HandlerContext {
    pub(crate) state: Arc<RwLock<PluginState>>,
}

impl HandlerContext {
    pub fn new() -> Self {
        Self {
            state: Arc::new(RwLock::new(PluginState {
                engine: None,
                default_model_path: None,
            })),
        }
    }

    /// POST /init — Initialize the ONNX engine with a model
    pub async fn handle_init(&self, body: &[u8]) -> Result<HttpResponse, Status> {
        #[derive(Deserialize)]
        struct InitRequest {
            model_path: String,
            #[serde(default = "default_confidence")]
            confidence_threshold: f32,
            #[serde(default = "default_nms")]
            nms_threshold: f32,
        }

        fn default_confidence() -> f32 {
            0.5
        }
        fn default_nms() -> f32 {
            0.45
        }

        let req: InitRequest = serde_json::from_slice(body)
            .map_err(|e| Status::invalid_argument(format!("Invalid JSON body: {}", e)))?;

        if req.model_path.is_empty() {
            return Err(Status::invalid_argument("model_path is required"));
        }

        info!(
            "Initializing ONNX engine: model={}, confidence={}, nms={}",
            req.model_path, req.confidence_threshold, req.nms_threshold
        );

        let config = VideoEngineConfig {
            model_path: req.model_path.clone(),
            confidence_threshold: req.confidence_threshold,
            nms_threshold: req.nms_threshold,
            ..Default::default()
        };

        let engine = VideoEngine::new(config).map_err(|e| {
            Status::internal(format!(
                "Failed to initialize ONNX engine with model '{}': {}",
                req.model_path, e
            ))
        })?;

        let (width, height) = engine.input_dimensions();
        let ready = engine.is_ready();

        {
            let mut state = self.state.write();
            state.engine = Some(engine);
        }

        #[derive(Serialize)]
        struct InitResponse {
            width: u32,
            height: u32,
            ready: bool,
        }

        json_response(
            200,
            &InitResponse {
                width,
                height,
                ready,
            },
        )
    }

    /// POST /frame — Process a video frame and return detections
    pub async fn handle_frame(&self, body: &[u8]) -> Result<HttpResponse, Status> {
        #[derive(Deserialize)]
        struct FrameRequest {
            frame_data: Vec<u8>,
            width: u32,
            height: u32,
            #[serde(default = "default_format")]
            format: String,
        }

        fn default_format() -> String {
            "rgba8".to_string()
        }

        let req: FrameRequest = serde_json::from_slice(body)
            .map_err(|e| Status::invalid_argument(format!("Invalid JSON body: {}", e)))?;

        let format = parse_frame_format(&req.format)?;

        let state = self.state.read();
        let engine = state.engine.as_ref().ok_or_else(|| {
            Status::unavailable("ONNX engine not initialized — call POST /init first")
        })?;

        let (detections, stats) =
            engine.process_frame(&req.frame_data, req.width, req.height, format, 0);

        debug!(
            "Frame {}x{}: {} detections in {}us",
            req.width, req.height, stats.detections_final, stats.total_us
        );

        // Serialize with PascalCase field names to match the JS glyph module expectations
        // (originally from Go's default JSON serialization)
        let detection_responses: Vec<DetectionResponse> = detections
            .into_iter()
            .map(|d| DetectionResponse {
                label: d.label,
                confidence: d.confidence,
                bbox: BBoxResponse {
                    x: d.bbox.x,
                    y: d.bbox.y,
                    width: d.bbox.width,
                    height: d.bbox.height,
                },
            })
            .collect();

        #[derive(Serialize)]
        struct FrameResponse {
            detections: Vec<DetectionResponse>,
            stats: StatsResponse,
        }

        json_response(
            200,
            &FrameResponse {
                detections: detection_responses,
                stats: StatsResponse {
                    preprocess_us: stats.preprocess_us,
                    inference_us: stats.inference_us,
                    postprocess_us: stats.postprocess_us,
                    total_us: stats.total_us,
                    frame_width: stats.frame_width,
                    frame_height: stats.frame_height,
                    detections_raw: stats.detections_raw,
                    detections_final: stats.detections_final,
                },
            },
        )
    }

    /// GET /status — Return engine status
    pub async fn handle_status(&self) -> Result<HttpResponse, Status> {
        let state = self.state.read();

        #[derive(Serialize)]
        struct StatusResponse {
            engine_ready: bool,
            model_input: Option<String>,
            plugin_version: String,
        }

        let (engine_ready, model_input) = match state.engine.as_ref() {
            Some(engine) => {
                let (w, h) = engine.input_dimensions();
                (engine.is_ready(), Some(format!("{}x{}", w, h)))
            }
            None => (false, None),
        };

        json_response(
            200,
            &StatusResponse {
                engine_ready,
                model_input,
                plugin_version: env!("CARGO_PKG_VERSION").to_string(),
            },
        )
    }

    /// GET /vidstream-glyph-module.js — Serve the embedded glyph module
    pub async fn handle_glyph_module(&self) -> Result<HttpResponse, Status> {
        Ok(HttpResponse {
            status_code: 200,
            headers: vec![
                HttpHeader {
                    name: "Content-Type".to_string(),
                    values: vec!["application/javascript".to_string()],
                },
                HttpHeader {
                    name: "Cache-Control".to_string(),
                    values: vec!["public, max-age=3600".to_string()],
                },
            ],
            body: GLYPH_MODULE_JS.as_bytes().to_vec(),
        })
    }
}

/// Detection response with PascalCase fields (matching Go JSON convention used by JS glyph module)
#[derive(Serialize)]
#[serde(rename_all = "PascalCase")]
struct DetectionResponse {
    label: String,
    confidence: f32,
    #[serde(rename = "BBox")]
    bbox: BBoxResponse,
}

/// Bounding box with PascalCase fields
#[derive(Serialize)]
#[serde(rename_all = "PascalCase")]
struct BBoxResponse {
    x: f32,
    y: f32,
    width: f32,
    height: f32,
}

/// Processing stats with snake_case fields (matching JS glyph module expectations)
#[derive(Serialize)]
struct StatsResponse {
    preprocess_us: u64,
    inference_us: u64,
    postprocess_us: u64,
    total_us: u64,
    frame_width: u32,
    frame_height: u32,
    detections_raw: u32,
    detections_final: u32,
}

/// Parse frame format string to FrameFormat enum
fn parse_frame_format(format: &str) -> Result<FrameFormat, Status> {
    match format {
        "rgb8" => Ok(FrameFormat::RGB8),
        "rgba8" => Ok(FrameFormat::RGBA8),
        "bgr8" => Ok(FrameFormat::BGR8),
        "yuv420" => Ok(FrameFormat::YUV420),
        "gray8" => Ok(FrameFormat::Gray8),
        _ => Err(Status::invalid_argument(format!(
            "Unknown frame format '{}' — supported: rgb8, rgba8, bgr8, yuv420, gray8",
            format
        ))),
    }
}

/// Create a JSON HTTP response
fn json_response<T: Serialize>(status_code: i32, data: &T) -> Result<HttpResponse, Status> {
    let body = serde_json::to_vec(data)
        .map_err(|e| Status::internal(format!("Failed to serialize response: {}", e)))?;

    Ok(HttpResponse {
        status_code,
        headers: vec![HttpHeader {
            name: "Content-Type".to_string(),
            values: vec!["application/json".to_string()],
        }],
        body,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn test_status_before_init() {
        let ctx = HandlerContext::new();
        let response = ctx.handle_status().await.unwrap();
        assert_eq!(response.status_code, 200);

        let body: serde_json::Value = serde_json::from_slice(&response.body).unwrap();
        assert_eq!(body["engine_ready"], false);
        assert_eq!(body["model_input"], serde_json::Value::Null);
    }

    #[tokio::test]
    async fn test_init_missing_model_path() {
        let ctx = HandlerContext::new();
        let body = serde_json::to_vec(&serde_json::json!({
            "model_path": "",
        }))
        .unwrap();
        let result = ctx.handle_init(&body).await;
        assert!(result.is_err());
    }

    #[tokio::test]
    async fn test_frame_without_engine() {
        let ctx = HandlerContext::new();
        let frame_data: Vec<u8> = vec![0; 100];
        let body = serde_json::to_vec(&serde_json::json!({
            "frame_data": frame_data,
            "width": 10,
            "height": 10,
            "format": "rgba8",
        }))
        .unwrap();
        let result = ctx.handle_frame(&body).await;
        assert!(result.is_err());
    }

    #[tokio::test]
    async fn test_glyph_module_served() {
        let ctx = HandlerContext::new();
        let response = ctx.handle_glyph_module().await.unwrap();
        assert_eq!(response.status_code, 200);
        assert!(response.headers.iter().any(|h| {
            h.name == "Content-Type"
                && h.values.iter().any(|v| v == "application/javascript")
        }));
        let body_str = String::from_utf8(response.body).unwrap();
        assert!(body_str.contains("render"));
    }

    #[test]
    fn test_parse_frame_format() {
        assert!(matches!(parse_frame_format("rgb8"), Ok(FrameFormat::RGB8)));
        assert!(matches!(
            parse_frame_format("rgba8"),
            Ok(FrameFormat::RGBA8)
        ));
        assert!(matches!(parse_frame_format("bgr8"), Ok(FrameFormat::BGR8)));
        assert!(parse_frame_format("invalid").is_err());
    }
}
