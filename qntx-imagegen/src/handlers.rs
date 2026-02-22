//! HTTP endpoint handlers for the imagegen plugin

use crate::proto::{HttpHeader, HttpResponse};
use crate::service::PluginState;
use parking_lot::RwLock;
use serde::Serialize;
use std::sync::Arc;
use tonic::Status;

/// Handler context providing access to plugin state
pub struct HandlerContext {
    pub(crate) state: Arc<RwLock<PluginState>>,
}

impl HandlerContext {
    pub fn new(state: Arc<RwLock<PluginState>>) -> Self {
        Self { state }
    }

    /// POST /generate — synchronous image generation (blocks until done)
    pub async fn handle_generate(
        &self,
        body: serde_json::Value,
    ) -> Result<HttpResponse, Status> {
        #[derive(serde::Deserialize)]
        struct GenerateRequest {
            prompt: String,
            #[serde(default)]
            negative_prompt: Option<String>,
            #[serde(default)]
            steps: Option<u32>,
            #[serde(default)]
            guidance_scale: Option<f32>,
            #[serde(default)]
            seed: Option<u64>,
            #[serde(default)]
            width: Option<u32>,
            #[serde(default)]
            height: Option<u32>,
        }

        let req: GenerateRequest = serde_json::from_value(body)
            .map_err(|e| Status::invalid_argument(format!("Invalid request: {}", e)))?;

        if req.prompt.is_empty() {
            return Err(Status::invalid_argument("Missing 'prompt' field"));
        }

        let (imagegen_config, has_pipeline) = {
            let state = self.state.read();
            (state.imagegen_config.clone(), state.pipeline.is_some())
        };

        if !has_pipeline {
            return Err(Status::unavailable(format!(
                "Pipeline not loaded — model files missing from '{}'",
                imagegen_config.models_dir.display()
            )));
        }

        // Mark as generating
        {
            let mut state = self.state.write();
            state.generating = true;
        }

        let prompt = req.prompt.clone();
        let negative_prompt = req.negative_prompt.unwrap_or_default();
        let steps = req
            .steps
            .unwrap_or(imagegen_config.num_inference_steps);
        let guidance_scale = req
            .guidance_scale
            .unwrap_or(imagegen_config.guidance_scale);
        let seed = req.seed.unwrap_or(42);
        let width = req.width.unwrap_or(imagegen_config.image_width);
        let height = req.height.unwrap_or(imagegen_config.image_height);
        let output_dir = imagegen_config.output_dir.clone();

        let state_clone = self.state.clone();

        let result = tokio::task::spawn_blocking(move || {
            let start = std::time::Instant::now();

            let generate_result = {
                let state = state_clone.read();
                let pipeline = state.pipeline.as_ref().unwrap();
                pipeline.generate(
                    &prompt,
                    &negative_prompt,
                    steps,
                    guidance_scale,
                    seed,
                    width,
                    height,
                )
            };

            let duration_ms = start.elapsed().as_millis() as u64;

            match generate_result {
                Ok(image_data) => {
                    use sha2::{Digest, Sha256};
                    let mut hasher = Sha256::new();
                    hasher.update(&image_data);
                    let hash = format!("{:x}", hasher.finalize());

                    let filename = format!("{}.png", hash[..16].to_string());
                    let output_path = output_dir.join(&filename);
                    if let Err(e) = std::fs::write(&output_path, &image_data) {
                        return Err(format!(
                            "Failed to write image to '{}': {}",
                            output_path.display(),
                            e
                        ));
                    }

                    Ok(serde_json::json!({
                        "filename": filename,
                        "output_path": output_path.display().to_string(),
                        "sha256": hash,
                        "duration_ms": duration_ms,
                        "width": width,
                        "height": height,
                        "steps": steps,
                        "guidance_scale": guidance_scale,
                        "seed": seed,
                    }))
                }
                Err(e) => Err(e),
            }
        })
        .await
        .unwrap_or_else(|e| Err(format!("Generation task panicked: {:?}", e)));

        // Clear generating flag
        {
            let mut state = self.state.write();
            state.generating = false;
        }

        match result {
            Ok(result_json) => json_response(200, &result_json),
            Err(e) => Err(Status::internal(e)),
        }
    }

    /// GET /status — pipeline state
    pub async fn handle_status(&self) -> Result<HttpResponse, Status> {
        let state = self.state.read();

        #[derive(Serialize)]
        struct StatusResponse {
            initialized: bool,
            pipeline_loaded: bool,
            generating: bool,
            models_dir: String,
            output_dir: String,
        }

        let response = StatusResponse {
            initialized: state.initialized,
            pipeline_loaded: state.pipeline.is_some(),
            generating: state.generating,
            models_dir: state.imagegen_config.models_dir.display().to_string(),
            output_dir: state.imagegen_config.output_dir.display().to_string(),
        };

        json_response(200, &response)
    }

    /// POST /models/check — report model file presence
    pub async fn handle_models_check(&self) -> Result<HttpResponse, Status> {
        let state = self.state.read();
        let result = crate::models::check_models(&state.imagegen_config.models_dir);
        json_response(200, &result)
    }

    /// GET /image/{filename} — serve generated PNG
    pub async fn handle_serve_image(&self, filename: &str) -> Result<HttpResponse, Status> {
        // Sanitize filename to prevent path traversal
        if filename.contains("..") || filename.contains('/') || filename.contains('\\') {
            return Err(Status::invalid_argument("Invalid filename"));
        }

        let state = self.state.read();
        let image_path = state.imagegen_config.output_dir.join(filename);

        let data = std::fs::read(&image_path).map_err(|e| {
            Status::not_found(format!(
                "Image '{}' not found: {}",
                image_path.display(),
                e
            ))
        })?;

        Ok(HttpResponse {
            status_code: 200,
            headers: vec![
                HttpHeader {
                    name: "Content-Type".to_string(),
                    values: vec!["image/png".to_string()],
                },
                HttpHeader {
                    name: "Content-Length".to_string(),
                    values: vec![data.len().to_string()],
                },
            ],
            body: data,
        })
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
