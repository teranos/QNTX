//! gRPC service implementation for the VidStream plugin
//!
//! Implements DomainPluginService: metadata, HTTP routing, glyph registration.

use crate::handlers::HandlerContext;
use crate::proto::{
    domain_plugin_service_server::DomainPluginService, ConfigFieldSchema, ConfigSchemaResponse,
    Empty, ExecuteJobRequest, ExecuteJobResponse, GlyphDef, GlyphDefResponse, HealthResponse,
    HttpHeader, HttpRequest, HttpResponse, InitializeRequest, InitializeResponse,
    MetadataResponse, WebSocketMessage,
};
use std::collections::HashMap;
use std::pin::Pin;
use tokio_stream::Stream;
use tonic::{Request, Response, Status, Streaming};
use tracing::{debug, info, warn};

/// VidStream plugin gRPC service
pub struct VidStreamPluginService {
    handlers: HandlerContext,
}

impl VidStreamPluginService {
    pub fn new() -> Self {
        Self {
            handlers: HandlerContext::new(),
        }
    }
}

impl Default for VidStreamPluginService {
    fn default() -> Self {
        Self::new()
    }
}

#[tonic::async_trait]
impl DomainPluginService for VidStreamPluginService {
    async fn metadata(
        &self,
        _request: Request<Empty>,
    ) -> Result<Response<MetadataResponse>, Status> {
        debug!("Metadata request received");
        Ok(Response::new(MetadataResponse {
            name: "vidstream".to_string(),
            version: env!("CARGO_PKG_VERSION").to_string(),
            qntx_version: ">=0.1.0".to_string(),
            description: "Real-time video inference via ONNX Runtime".to_string(),
            author: "QNTX Contributors".to_string(),
            license: "MIT".to_string(),
        }))
    }

    async fn initialize(
        &self,
        request: Request<InitializeRequest>,
    ) -> Result<Response<InitializeResponse>, Status> {
        let req = request.into_inner();
        debug!(
            "Initializing VidStream plugin (ATS: {}, Queue: {})",
            req.ats_store_endpoint, req.queue_endpoint
        );

        // Apply config defaults if provided
        if let Some(model_path) = req.config.get("model_path") {
            let mut state = self.handlers.state.write();
            state.default_model_path = Some(model_path.clone());
        }

        info!(
            "VidStream plugin v{} initialized",
            env!("CARGO_PKG_VERSION")
        );

        Ok(Response::new(InitializeResponse {
            handler_names: vec![],
            schedules: vec![],
        }))
    }

    async fn shutdown(&self, _request: Request<Empty>) -> Result<Response<Empty>, Status> {
        info!("Shutting down VidStream plugin");
        let mut state = self.handlers.state.write();
        state.engine = None;
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

        let result = match (method.as_str(), path.as_str()) {
            ("POST", "/init") => self.handlers.handle_init(&req.body).await,
            ("POST", "/frame") => self.handlers.handle_frame(&req.body).await,
            ("GET", "/status") => self.handlers.handle_status().await,
            ("GET", "/vidstream-glyph-module.js") => self.handlers.handle_glyph_module().await,
            _ => Err(Status::not_found(format!(
                "Unknown endpoint: {} {}",
                method, path
            ))),
        };

        match result {
            Ok(response) => Ok(Response::new(response)),
            Err(status) => {
                let error_body = serde_json::json!({ "error": status.message() });
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
        warn!("WebSocket not supported by VidStream plugin");
        Err(Status::unimplemented(
            "WebSocket not supported — use HTTP endpoints via pluginFetch",
        ))
    }

    async fn health(&self, _request: Request<Empty>) -> Result<Response<HealthResponse>, Status> {
        let state = self.handlers.state.read();
        let engine_ready = state.engine.as_ref().is_some_and(|e| e.is_ready());

        let mut details = HashMap::new();
        details.insert("plugin_version".to_string(), env!("CARGO_PKG_VERSION").to_string());
        details.insert("engine_ready".to_string(), engine_ready.to_string());

        if let Some(ref engine) = state.engine {
            let (w, h) = engine.input_dimensions();
            details.insert("model_input".to_string(), format!("{}x{}", w, h));
        }

        Ok(Response::new(HealthResponse {
            healthy: true,
            message: if engine_ready {
                "ONNX engine ready".to_string()
            } else {
                "Engine not initialized — call POST /init".to_string()
            },
            details,
        }))
    }

    async fn config_schema(
        &self,
        _request: Request<Empty>,
    ) -> Result<Response<ConfigSchemaResponse>, Status> {
        let mut fields = HashMap::new();

        fields.insert(
            "model_path".to_string(),
            ConfigFieldSchema {
                r#type: "string".to_string(),
                description: "Default ONNX model path".to_string(),
                default_value: "ats/vidstream/models/yolo11n.onnx".to_string(),
                required: false,
                min_value: String::new(),
                max_value: String::new(),
                pattern: String::new(),
                element_type: String::new(),
            },
        );

        fields.insert(
            "confidence_threshold".to_string(),
            ConfigFieldSchema {
                r#type: "number".to_string(),
                description: "Detection confidence threshold (0.0-1.0)".to_string(),
                default_value: "0.5".to_string(),
                required: false,
                min_value: "0.0".to_string(),
                max_value: "1.0".to_string(),
                pattern: String::new(),
                element_type: String::new(),
            },
        );

        Ok(Response::new(ConfigSchemaResponse { fields }))
    }

    async fn register_glyphs(
        &self,
        _request: Request<Empty>,
    ) -> Result<Response<GlyphDefResponse>, Status> {
        Ok(Response::new(GlyphDefResponse {
            glyphs: vec![GlyphDef {
                symbol: "\u{2B80}".to_string(), // ⮀
                title: "VidStream".to_string(),
                label: "vidstream".to_string(),
                content_path: String::new(),
                css_path: String::new(),
                module_path: "/vidstream-glyph-module.js".to_string(),
                default_width: 680,
                default_height: 620,
            }],
        }))
    }

    async fn execute_job(
        &self,
        _request: Request<ExecuteJobRequest>,
    ) -> Result<Response<ExecuteJobResponse>, Status> {
        Err(Status::unimplemented(
            "VidStream plugin does not support async jobs",
        ))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn test_metadata() {
        let service = VidStreamPluginService::new();
        let response = service.metadata(Request::new(Empty {})).await.unwrap();
        let meta = response.into_inner();
        assert_eq!(meta.name, "vidstream");
        assert!(!meta.version.is_empty());
    }

    #[tokio::test]
    async fn test_health_before_init() {
        let service = VidStreamPluginService::new();
        let response = service.health(Request::new(Empty {})).await.unwrap();
        let health = response.into_inner();
        assert!(health.healthy);
        assert!(health.message.contains("not initialized"));
    }

    #[tokio::test]
    async fn test_register_glyphs() {
        let service = VidStreamPluginService::new();
        let response = service
            .register_glyphs(Request::new(Empty {}))
            .await
            .unwrap();
        let glyphs = response.into_inner();
        assert_eq!(glyphs.glyphs.len(), 1);
        assert_eq!(glyphs.glyphs[0].label, "vidstream");
        assert_eq!(
            glyphs.glyphs[0].module_path,
            "/vidstream-glyph-module.js"
        );
    }
}
