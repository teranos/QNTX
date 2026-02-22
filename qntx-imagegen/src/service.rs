//! gRPC service implementation for the imagegen plugin
//!
//! Implements the DomainPluginService interface for QNTX.

use crate::atsstore::{self, AtsStoreConfig};
use crate::config::{ImagegenConfig, PluginConfig};
use crate::handlers::HandlerContext;
use crate::pipeline::DiffusionPipeline;
use crate::proto::{
    domain_plugin_service_server::DomainPluginService, ConfigSchemaResponse, Empty,
    ExecuteJobRequest, ExecuteJobResponse, HealthResponse, HttpHeader, HttpRequest, HttpResponse,
    InitializeRequest, InitializeResponse, MetadataResponse, WebSocketMessage,
};
use parking_lot::RwLock;
use std::collections::HashMap;
use std::pin::Pin;
use std::sync::Arc;
use tokio_stream::Stream;
use tonic::{Request, Response, Status, Streaming};
use tracing::{debug, error, info, warn};

/// Shared plugin state
pub(crate) struct PluginState {
    pub config: Option<PluginConfig>,
    pub imagegen_config: ImagegenConfig,
    pub pipeline: Option<DiffusionPipeline>,
    pub initialized: bool,
    pub ats_client: atsstore::SharedAtsStoreClient,
    /// True while a generation is in progress
    pub generating: bool,
}

/// Imagegen plugin gRPC service
pub struct ImagegenPluginService {
    pub(crate) handlers: HandlerContext,
}

impl ImagegenPluginService {
    pub fn new() -> Self {
        let state = Arc::new(RwLock::new(PluginState {
            config: None,
            imagegen_config: ImagegenConfig::default(),
            pipeline: None,
            initialized: false,
            ats_client: atsstore::new_shared_client(),
            generating: false,
        }));

        Self {
            handlers: HandlerContext::new(state),
        }
    }
}

impl Default for ImagegenPluginService {
    fn default() -> Self {
        Self::new()
    }
}

#[tonic::async_trait]
impl DomainPluginService for ImagegenPluginService {
    async fn metadata(
        &self,
        _request: Request<Empty>,
    ) -> Result<Response<MetadataResponse>, Status> {
        debug!("Metadata request received");
        Ok(Response::new(MetadataResponse {
            name: "imagegen".to_string(),
            version: env!("CARGO_PKG_VERSION").to_string(),
            qntx_version: ">=0.1.0".to_string(),
            description: "Stable Diffusion image generation via ONNX Runtime".to_string(),
            author: "QNTX Contributors".to_string(),
            license: "MIT".to_string(),
        }))
    }

    async fn initialize(
        &self,
        request: Request<InitializeRequest>,
    ) -> Result<Response<InitializeResponse>, Status> {
        let req = request.into_inner();
        info!("Initializing imagegen plugin");
        info!("ATSStore endpoint: {}", req.ats_store_endpoint);

        let imagegen_config = ImagegenConfig::from_config_map(&req.config);
        info!("Models dir: {}", imagegen_config.models_dir.display());
        info!("Output dir: {}", imagegen_config.output_dir.display());

        // Ensure output directory exists
        if let Err(e) = std::fs::create_dir_all(&imagegen_config.output_dir) {
            error!(
                "Failed to create output directory '{}': {}",
                imagegen_config.output_dir.display(),
                e
            );
            return Err(Status::internal(format!(
                "Failed to create output directory '{}': {}",
                imagegen_config.output_dir.display(),
                e
            )));
        }

        // Check model files
        let model_check = crate::models::check_models(&imagegen_config.models_dir);
        if !model_check.all_present {
            let missing: Vec<_> = model_check
                .files
                .iter()
                .filter(|f| !f.present)
                .map(|f| f.path.as_str())
                .collect();
            warn!(
                "Missing model files in '{}': {:?} — pipeline will not load until models are present",
                imagegen_config.models_dir.display(),
                missing
            );
        }

        // Try to load the pipeline
        let pipeline = if model_check.all_present {
            match DiffusionPipeline::load(&imagegen_config) {
                Ok(p) => {
                    info!("Diffusion pipeline loaded successfully");
                    Some(p)
                }
                Err(e) => {
                    error!("Failed to load diffusion pipeline: {}", e);
                    None
                }
            }
        } else {
            None
        };

        {
            let mut state = self.handlers.state.write();

            state.config = Some(PluginConfig {
                ats_store_endpoint: req.ats_store_endpoint.clone(),
                queue_endpoint: req.queue_endpoint,
                auth_token: req.auth_token.clone(),
                config: req.config,
            });

            // Initialize ATSStore client if endpoint is provided
            if !req.ats_store_endpoint.is_empty() {
                info!("Initializing ATSStore client for attestation support");
                atsstore::init_shared_client(
                    &state.ats_client,
                    AtsStoreConfig {
                        endpoint: req.ats_store_endpoint,
                        auth_token: req.auth_token,
                    },
                );
            }

            state.imagegen_config = imagegen_config;
            state.pipeline = pipeline;
            state.initialized = true;
        }

        let handler_names = vec!["imagegen.generate".to_string()];
        info!("Announcing async handlers: {:?}", handler_names);

        Ok(Response::new(InitializeResponse { handler_names }))
    }

    async fn shutdown(&self, _request: Request<Empty>) -> Result<Response<Empty>, Status> {
        info!("Shutting down imagegen plugin");
        let mut state = self.handlers.state.write();
        state.initialized = false;
        state.pipeline = None;
        state.config = None;
        Ok(Response::new(Empty {}))
    }

    async fn handle_http(
        &self,
        request: Request<HttpRequest>,
    ) -> Result<Response<HttpResponse>, Status> {
        let req = request.into_inner();
        let path = &req.path;
        let method = &req.method;

        debug!("HTTP request: {} {}", method, path);

        let body: serde_json::Value = if req.body.is_empty() {
            serde_json::Value::Null
        } else {
            serde_json::from_slice(&req.body)
                .map_err(|e| Status::invalid_argument(format!("Invalid JSON body: {}", e)))?
        };

        let result = match (method.as_str(), path.as_str()) {
            ("POST", "/generate") => self.handlers.handle_generate(body).await,
            ("GET", "/status") => self.handlers.handle_status().await,
            ("POST", "/models/check") => self.handlers.handle_models_check().await,
            ("GET", path) if path.starts_with("/image/") => {
                let filename = &path["/image/".len()..];
                self.handlers.handle_serve_image(filename).await
            }
            _ => Err(Status::not_found(format!(
                "Unknown endpoint: {} {}",
                method, path
            ))),
        };

        match result {
            Ok(response) => Ok(Response::new(response)),
            Err(status) => {
                let error_body = serde_json::json!({
                    "error": status.message()
                });
                Ok(Response::new(HttpResponse {
                    status_code: match status.code() {
                        tonic::Code::NotFound => 404,
                        tonic::Code::InvalidArgument => 400,
                        tonic::Code::Internal => 500,
                        tonic::Code::Unavailable => 503,
                        _ => 500,
                    },
                    headers: vec![HttpHeader {
                        name: "Content-Type".to_string(),
                        values: vec!["application/json".to_string()],
                    }],
                    body: serde_json::to_vec(&error_body).unwrap_or_default(),
                }))
            }
        }
    }

    type HandleWebSocketStream =
        Pin<Box<dyn Stream<Item = Result<WebSocketMessage, Status>> + Send>>;

    async fn handle_web_socket(
        &self,
        _request: Request<Streaming<WebSocketMessage>>,
    ) -> Result<Response<Self::HandleWebSocketStream>, Status> {
        warn!("WebSocket not supported by imagegen plugin");
        Err(Status::unimplemented(
            "WebSocket not supported by imagegen plugin",
        ))
    }

    async fn health(&self, _request: Request<Empty>) -> Result<Response<HealthResponse>, Status> {
        let state = self.handlers.state.read();
        let healthy = state.initialized;

        let mut details = HashMap::new();
        details.insert("initialized".to_string(), state.initialized.to_string());
        details.insert(
            "pipeline_loaded".to_string(),
            state.pipeline.is_some().to_string(),
        );
        details.insert("generating".to_string(), state.generating.to_string());
        details.insert(
            "models_dir".to_string(),
            state.imagegen_config.models_dir.display().to_string(),
        );

        if let Ok(exe_path) = std::env::current_exe() {
            if let Ok(metadata) = std::fs::metadata(&exe_path) {
                if let Ok(modified) = metadata.modified() {
                    if let Ok(duration) = modified.duration_since(std::time::UNIX_EPOCH) {
                        details.insert("binary_built".to_string(), duration.as_secs().to_string());
                    }
                }
            }
        }

        Ok(Response::new(HealthResponse {
            healthy,
            message: if healthy {
                if state.pipeline.is_some() {
                    "OK — pipeline loaded".to_string()
                } else {
                    "OK — pipeline not loaded (models missing)".to_string()
                }
            } else {
                "Not initialized".to_string()
            },
            details,
        }))
    }

    async fn config_schema(
        &self,
        _request: Request<Empty>,
    ) -> Result<Response<ConfigSchemaResponse>, Status> {
        debug!("ConfigSchema request received");
        Ok(Response::new(ConfigSchemaResponse {
            fields: crate::config::build_schema(),
        }))
    }

    async fn execute_job(
        &self,
        request: Request<ExecuteJobRequest>,
    ) -> Result<Response<ExecuteJobResponse>, Status> {
        let req = request.into_inner();

        debug!(
            "ExecuteJob request: job_id={}, handler={}",
            req.job_id, req.handler_name
        );

        if req.handler_name != "imagegen.generate" {
            return Err(Status::not_found(format!(
                "Unknown handler: {}",
                req.handler_name
            )));
        }

        self.execute_generate_job(req).await
    }
}

impl ImagegenPluginService {
    async fn execute_generate_job(
        &self,
        req: ExecuteJobRequest,
    ) -> Result<Response<ExecuteJobResponse>, Status> {
        #[derive(serde::Deserialize)]
        struct GeneratePayload {
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

        let payload: GeneratePayload = serde_json::from_slice(&req.payload)
            .map_err(|e| Status::invalid_argument(format!("Invalid payload JSON: {}", e)))?;

        if payload.prompt.is_empty() {
            return Err(Status::invalid_argument("Missing prompt in payload"));
        }

        // Grab config and pipeline availability
        let (imagegen_config, has_pipeline) = {
            let state = self.handlers.state.read();
            (state.imagegen_config.clone(), state.pipeline.is_some())
        };

        if !has_pipeline {
            return Ok(Response::new(ExecuteJobResponse {
                success: false,
                error: format!(
                    "Pipeline not loaded — model files missing from '{}'",
                    imagegen_config.models_dir.display()
                ),
                result: vec![],
                progress_current: 0,
                progress_total: 0,
                cost_actual: 0.0,
            }));
        }

        // Mark as generating
        {
            let mut state = self.handlers.state.write();
            state.generating = true;
        }

        let prompt = payload.prompt.clone();
        let prompt_for_attest = prompt.clone();
        let negative_prompt = payload.negative_prompt.unwrap_or_default();
        let steps = payload
            .steps
            .unwrap_or(imagegen_config.num_inference_steps);
        let guidance_scale = payload
            .guidance_scale
            .unwrap_or(imagegen_config.guidance_scale);
        let seed = payload.seed.unwrap_or(42);
        let width = payload.width.unwrap_or(imagegen_config.image_width);
        let height = payload.height.unwrap_or(imagegen_config.image_height);
        let output_dir = imagegen_config.output_dir.clone();
        let job_id = req.job_id.clone();

        let state_clone = self.handlers.state.clone();

        // Run inference in a blocking task to keep gRPC responsive
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
                    // Compute SHA256
                    use sha2::{Digest, Sha256};
                    let mut hasher = Sha256::new();
                    hasher.update(&image_data);
                    let hash = format!("{:x}", hasher.finalize());

                    // Save to output directory
                    let filename = format!("{}.png", job_id);
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
            let mut state = self.handlers.state.write();
            state.generating = false;
        }

        match result {
            Ok(result_json) => {
                // Create attestation if ATSStore is available
                let sha256 = result_json["sha256"].as_str().unwrap_or("").to_string();
                let output_path = result_json["output_path"]
                    .as_str()
                    .unwrap_or("")
                    .to_string();
                let duration_ms = result_json["duration_ms"].as_u64().unwrap_or(0);

                {
                    let state = self.handlers.state.read();
                    atsstore::create_attestation(
                        &state.ats_client,
                        &sha256,
                        &prompt_for_attest,
                        steps,
                        seed,
                        duration_ms,
                        &output_path,
                    );
                }

                let result_bytes = serde_json::to_vec(&result_json).unwrap_or_default();

                Ok(Response::new(ExecuteJobResponse {
                    success: true,
                    error: String::new(),
                    result: result_bytes,
                    progress_current: steps as i32,
                    progress_total: steps as i32,
                    cost_actual: 0.0,
                }))
            }
            Err(e) => Ok(Response::new(ExecuteJobResponse {
                success: false,
                error: e,
                result: vec![],
                progress_current: 0,
                progress_total: 0,
                cost_actual: 0.0,
            })),
        }
    }
}

