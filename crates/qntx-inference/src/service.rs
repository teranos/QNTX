//! gRPC service implementation for the inference plugin.

use crate::engine::{EngineConfig, InferenceEngine};
use qntx::plugin::proto::{
    domain_plugin_service_server::DomainPluginService, ConfigFieldSchema, ConfigSchemaResponse,
    Empty, HealthResponse, HttpRequest, HttpResponse, InitializeRequest, MetadataResponse,
    WebSocketMessage,
};
use serde::{Deserialize, Serialize};
use std::pin::Pin;
use std::sync::Arc;
use tokio_stream::Stream;
use tonic::{Request, Response, Status};
use tracing::{debug, error, info, warn};

/// Request body for the /embed endpoint.
#[derive(Debug, Deserialize)]
struct EmbedRequest {
    /// Text or texts to embed
    #[serde(default)]
    input: EmbedInput,
    /// Model to use (optional, for compatibility)
    #[serde(default)]
    model: Option<String>,
}

#[derive(Debug, Deserialize)]
#[serde(untagged)]
enum EmbedInput {
    Single(String),
    Batch(Vec<String>),
}

impl Default for EmbedInput {
    fn default() -> Self {
        EmbedInput::Single(String::new())
    }
}

/// Response body for the /embed endpoint.
#[derive(Debug, Serialize)]
struct EmbedResponse {
    embeddings: Vec<Vec<f32>>,
    model: String,
    dimensions: usize,
}

/// Error response body.
#[derive(Debug, Serialize)]
struct ErrorResponse {
    error: String,
}

/// The inference plugin gRPC service.
pub struct InferencePluginService {
    engine: Arc<InferenceEngine>,
}

impl InferencePluginService {
    pub fn new(engine: Arc<InferenceEngine>) -> Self {
        Self { engine }
    }

    fn handle_embed(&self, body: &[u8]) -> Result<Vec<u8>, Status> {
        let request: EmbedRequest = serde_json::from_slice(body)
            .map_err(|e| Status::invalid_argument(format!("invalid request body: {}", e)))?;

        let texts = match request.input {
            EmbedInput::Single(text) => vec![text],
            EmbedInput::Batch(texts) => texts,
        };

        if texts.is_empty() {
            return Err(Status::invalid_argument("no input texts provided"));
        }

        let embeddings = self
            .engine
            .embed(&texts)
            .map_err(|e| Status::internal(format!("embedding failed: {}", e)))?;

        let dimensions = embeddings.first().map(|e| e.len()).unwrap_or(0);
        let model_name = self
            .engine
            .config()
            .map(|c| c.model_path)
            .unwrap_or_else(|| "unknown".to_string());

        let response = EmbedResponse {
            embeddings,
            model: model_name,
            dimensions,
        };

        serde_json::to_vec(&response)
            .map_err(|e| Status::internal(format!("failed to serialize response: {}", e)))
    }

    fn handle_health_http(&self) -> Vec<u8> {
        #[derive(Serialize)]
        struct HealthStatus {
            healthy: bool,
            model_loaded: bool,
            message: String,
        }

        let model_loaded = self.engine.is_loaded();
        let status = HealthStatus {
            healthy: model_loaded,
            model_loaded,
            message: if model_loaded {
                "model loaded and ready".to_string()
            } else {
                "no model loaded".to_string()
            },
        };

        serde_json::to_vec(&status).unwrap_or_default()
    }
}

#[tonic::async_trait]
impl DomainPluginService for InferencePluginService {
    async fn metadata(&self, _request: Request<Empty>) -> Result<Response<MetadataResponse>, Status> {
        Ok(Response::new(MetadataResponse {
            name: "qntx-inference".to_string(),
            version: env!("CARGO_PKG_VERSION").to_string(),
            qntx_version: "0.1.0".to_string(),
            description: "Local inference plugin for embeddings and semantic search".to_string(),
            author: "QNTX Contributors".to_string(),
            license: "MIT".to_string(),
        }))
    }

    async fn initialize(
        &self,
        request: Request<InitializeRequest>,
    ) -> Result<Response<Empty>, Status> {
        let req = request.into_inner();
        info!("Initializing inference plugin");

        // Extract configuration
        let model_path = req
            .config
            .get("model_path")
            .cloned()
            .unwrap_or_default();
        let tokenizer_path = req
            .config
            .get("tokenizer_path")
            .cloned()
            .unwrap_or_default();
        let max_length: usize = req
            .config
            .get("max_length")
            .and_then(|s| s.parse().ok())
            .unwrap_or(512);
        let normalize: bool = req
            .config
            .get("normalize")
            .and_then(|s| s.parse().ok())
            .unwrap_or(true);
        let num_threads: usize = req
            .config
            .get("num_threads")
            .and_then(|s| s.parse().ok())
            .unwrap_or(0);

        // Load model if paths provided
        if !model_path.is_empty() && !tokenizer_path.is_empty() {
            let config = EngineConfig {
                model_path,
                tokenizer_path,
                max_length,
                normalize,
                num_threads,
            };

            self.engine
                .load(config)
                .map_err(|e| Status::internal(format!("failed to load model: {}", e)))?;
        } else {
            warn!("No model paths provided, plugin will start without a loaded model");
        }

        Ok(Response::new(Empty {}))
    }

    async fn shutdown(&self, _request: Request<Empty>) -> Result<Response<Empty>, Status> {
        info!("Shutting down inference plugin");
        self.engine.unload();
        Ok(Response::new(Empty {}))
    }

    async fn handle_http(
        &self,
        request: Request<HttpRequest>,
    ) -> Result<Response<HttpResponse>, Status> {
        let req = request.into_inner();
        debug!("HTTP {} {}", req.method, req.path);

        let (status_code, body) = match (req.method.as_str(), req.path.as_str()) {
            ("POST", "/embed") | ("POST", "/v1/embeddings") => {
                match self.handle_embed(&req.body) {
                    Ok(body) => (200, body),
                    Err(status) => {
                        let error = ErrorResponse {
                            error: status.message().to_string(),
                        };
                        let code = match status.code() {
                            tonic::Code::InvalidArgument => 400,
                            tonic::Code::NotFound => 404,
                            _ => 500,
                        };
                        (code, serde_json::to_vec(&error).unwrap_or_default())
                    }
                }
            }
            ("GET", "/health") => (200, self.handle_health_http()),
            _ => {
                let error = ErrorResponse {
                    error: format!("unknown endpoint: {} {}", req.method, req.path),
                };
                (404, serde_json::to_vec(&error).unwrap_or_default())
            }
        };

        Ok(Response::new(HttpResponse {
            status_code,
            headers: vec![qntx::plugin::proto::HttpHeader {
                name: "Content-Type".to_string(),
                values: vec!["application/json".to_string()],
            }],
            body,
        }))
    }

    type HandleWebSocketStream =
        Pin<Box<dyn Stream<Item = Result<WebSocketMessage, Status>> + Send>>;

    async fn handle_web_socket(
        &self,
        _request: Request<tonic::Streaming<WebSocketMessage>>,
    ) -> Result<Response<Self::HandleWebSocketStream>, Status> {
        // WebSocket not supported for inference plugin
        Err(Status::unimplemented(
            "WebSocket not supported by inference plugin",
        ))
    }

    async fn health(&self, _request: Request<Empty>) -> Result<Response<HealthResponse>, Status> {
        let model_loaded = self.engine.is_loaded();

        Ok(Response::new(HealthResponse {
            healthy: model_loaded,
            message: if model_loaded {
                "model loaded and ready".to_string()
            } else {
                "no model loaded".to_string()
            },
            details: Default::default(),
        }))
    }

    async fn config_schema(
        &self,
        _request: Request<Empty>,
    ) -> Result<Response<ConfigSchemaResponse>, Status> {
        let mut fields = std::collections::HashMap::new();

        fields.insert(
            "model_path".to_string(),
            ConfigFieldSchema {
                r#type: "string".to_string(),
                description: "Path to the ONNX model file".to_string(),
                default_value: String::new(),
                required: true,
                min_value: String::new(),
                max_value: String::new(),
                pattern: String::new(),
                element_type: String::new(),
            },
        );

        fields.insert(
            "tokenizer_path".to_string(),
            ConfigFieldSchema {
                r#type: "string".to_string(),
                description: "Path to the tokenizer.json file".to_string(),
                default_value: String::new(),
                required: true,
                min_value: String::new(),
                max_value: String::new(),
                pattern: String::new(),
                element_type: String::new(),
            },
        );

        fields.insert(
            "max_length".to_string(),
            ConfigFieldSchema {
                r#type: "number".to_string(),
                description: "Maximum sequence length for tokenization".to_string(),
                default_value: "512".to_string(),
                required: false,
                min_value: "1".to_string(),
                max_value: "8192".to_string(),
                pattern: String::new(),
                element_type: String::new(),
            },
        );

        fields.insert(
            "normalize".to_string(),
            ConfigFieldSchema {
                r#type: "boolean".to_string(),
                description: "Whether to L2-normalize output embeddings".to_string(),
                default_value: "true".to_string(),
                required: false,
                min_value: String::new(),
                max_value: String::new(),
                pattern: String::new(),
                element_type: String::new(),
            },
        );

        fields.insert(
            "num_threads".to_string(),
            ConfigFieldSchema {
                r#type: "number".to_string(),
                description: "Number of threads for inference (0 = auto)".to_string(),
                default_value: "0".to_string(),
                required: false,
                min_value: "0".to_string(),
                max_value: "64".to_string(),
                pattern: String::new(),
                element_type: String::new(),
            },
        );

        Ok(Response::new(ConfigSchemaResponse { fields }))
    }
}
