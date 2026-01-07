//! gRPC service implementation for the Python plugin
//!
//! Implements the DomainPluginService interface for QNTX.

use crate::config::PluginConfig;
use crate::engine::PythonEngine;
use crate::handlers::{HandlerContext, PluginState};
use crate::proto::{
    domain_plugin_service_server::DomainPluginService, ConfigSchemaResponse, Empty,
    HealthResponse, HttpHeader, HttpRequest, HttpResponse, InitializeRequest, MetadataResponse,
    WebSocketMessage,
};
use parking_lot::RwLock;
use std::collections::HashMap;
use std::pin::Pin;
use std::sync::Arc;
use tokio_stream::Stream;
use tonic::{Request, Response, Status, Streaming};
use tracing::{debug, error, info, warn};

/// Python plugin gRPC service
pub struct PythonPluginService {
    handlers: HandlerContext,
}

impl PythonPluginService {
    /// Create a new Python plugin service
    pub fn new() -> Result<Self, Box<dyn std::error::Error>> {
        tracing::info!("Creating Python engine...");
        let engine = match PythonEngine::new() {
            Ok(e) => {
                tracing::info!("Python engine created successfully");
                e
            }
            Err(e) => {
                tracing::error!("Failed to create Python engine: {}", e);
                return Err(format!("Python engine creation failed: {}", e).into());
            }
        };

        tracing::info!("Initializing plugin state...");
        let state = Arc::new(RwLock::new(PluginState {
            config: None,
            engine,
            initialized: false,
            default_modules: crate::handlers::DEFAULT_MODULES
                .iter()
                .map(|s| s.to_string())
                .collect(),
        }));

        Ok(Self {
            handlers: HandlerContext::new(state),
        })
    }

    /// Get Python version for health checks
    fn python_version(&self) -> String {
        self.handlers.python_version()
    }
}

impl Default for PythonPluginService {
    fn default() -> Self {
        Self::new().expect("Failed to create PythonPluginService")
    }
}

#[tonic::async_trait]
impl DomainPluginService for PythonPluginService {
    /// Return plugin metadata
    async fn metadata(
        &self,
        _request: Request<Empty>,
    ) -> Result<Response<MetadataResponse>, Status> {
        debug!("Metadata request received");
        Ok(Response::new(MetadataResponse {
            name: "python".to_string(),
            version: env!("CARGO_PKG_VERSION").to_string(),
            qntx_version: ">=0.1.0".to_string(),
            description: "Python execution plugin - run Python code within QNTX".to_string(),
            author: "QNTX Contributors".to_string(),
            license: "MIT".to_string(),
        }))
    }

    /// Initialize the plugin with service endpoints
    async fn initialize(
        &self,
        request: Request<InitializeRequest>,
    ) -> Result<Response<Empty>, Status> {
        let req = request.into_inner();
        info!("Initializing Python plugin");
        info!("ATSStore endpoint: {}", req.ats_store_endpoint);
        info!("Queue endpoint: {}", req.queue_endpoint);

        let mut state = self.handlers.state.write();

        // Store configuration
        state.config = Some(PluginConfig {
            ats_store_endpoint: req.ats_store_endpoint,
            queue_endpoint: req.queue_endpoint,
            auth_token: req.auth_token,
            config: req.config,
        });

        // Initialize Python engine with custom paths if provided
        let python_paths: Vec<String> = state
            .config
            .as_ref()
            .and_then(|c| c.config.get("python_paths"))
            .map(|p| p.split(':').map(String::from).collect())
            .unwrap_or_default();

        // Override default modules if provided in config
        if let Some(modules_str) = state
            .config
            .as_ref()
            .and_then(|c| c.config.get("default_modules"))
        {
            state.default_modules = modules_str.split(',').map(|s| s.trim().to_string()).collect();
            info!("Using configured default modules: {:?}", state.default_modules);
        }

        if let Err(e) = state.engine.initialize(python_paths) {
            error!("Failed to initialize Python engine: {}", e);
            return Err(Status::internal(format!(
                "Failed to initialize Python engine: {}",
                e
            )));
        }

        state.initialized = true;
        info!(
            "Python plugin initialized successfully (Python {})",
            state.engine.python_version()
        );

        Ok(Response::new(Empty {}))
    }

    /// Shutdown the plugin
    async fn shutdown(&self, _request: Request<Empty>) -> Result<Response<Empty>, Status> {
        info!("Shutting down Python plugin");
        let mut state = self.handlers.state.write();
        state.initialized = false;
        state.config = None;
        Ok(Response::new(Empty {}))
    }

    /// Handle HTTP requests - routes to appropriate handler
    async fn handle_http(
        &self,
        request: Request<HttpRequest>,
    ) -> Result<Response<HttpResponse>, Status> {
        let req = request.into_inner();
        let path = &req.path;
        let method = &req.method;

        debug!("HTTP request: {} {}", method, path);

        // Parse request body
        let body: serde_json::Value = if req.body.is_empty() {
            serde_json::Value::Null
        } else {
            serde_json::from_slice(&req.body)
                .map_err(|e| Status::invalid_argument(format!("Invalid JSON body: {}", e)))?
        };

        // Route to handler
        let result = match (method.as_str(), path.as_str()) {
            // Python execution endpoints
            ("POST", "/execute") => self.handlers.handle_execute(body).await,
            ("POST", "/evaluate") => self.handlers.handle_evaluate(body).await,
            ("POST", "/execute-file") => self.handlers.handle_execute_file(body).await,

            // Package management
            ("POST", "/pip/install") => self.handlers.handle_pip_install(body).await,
            ("GET", "/pip/check") => self.handlers.handle_pip_check(body).await,

            // Info endpoints
            ("GET", "/version") => self.handlers.handle_version().await,
            ("GET", "/modules") => self.handlers.handle_modules(body).await,

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

    /// Handle WebSocket connections (not supported)
    type HandleWebSocketStream =
        Pin<Box<dyn Stream<Item = Result<WebSocketMessage, Status>> + Send>>;

    async fn handle_web_socket(
        &self,
        _request: Request<Streaming<WebSocketMessage>>,
    ) -> Result<Response<Self::HandleWebSocketStream>, Status> {
        warn!("WebSocket not supported by Python plugin");
        Err(Status::unimplemented(
            "WebSocket not supported by Python plugin",
        ))
    }

    /// Check plugin health
    async fn health(&self, _request: Request<Empty>) -> Result<Response<HealthResponse>, Status> {
        let state = self.handlers.state.read();
        let healthy = state.initialized;

        let mut details = HashMap::new();
        details.insert("python_version".to_string(), self.python_version());
        details.insert("initialized".to_string(), state.initialized.to_string());

        if let Some(config) = &state.config {
            if !config.ats_store_endpoint.is_empty() {
                details.insert("ats_store".to_string(), "configured".to_string());
            }
            if !config.queue_endpoint.is_empty() {
                details.insert("queue".to_string(), "configured".to_string());
            }
        }

        Ok(Response::new(HealthResponse {
            healthy,
            message: if healthy {
                "OK".to_string()
            } else {
                "Not initialized".to_string()
            },
            details,
        }))
    }

    /// Return configuration schema
    async fn config_schema(
        &self,
        _request: Request<Empty>,
    ) -> Result<Response<ConfigSchemaResponse>, Status> {
        debug!("ConfigSchema request received");
        Ok(Response::new(ConfigSchemaResponse {
            fields: crate::config::build_schema(),
        }))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde::Deserialize;

    #[tokio::test]
    async fn test_metadata() {
        let service = PythonPluginService::new().unwrap();
        let response = service.metadata(Request::new(Empty {})).await.unwrap();
        let meta = response.into_inner();
        assert_eq!(meta.name, "python");
        assert!(!meta.version.is_empty());
    }

    #[tokio::test]
    async fn test_health_before_init() {
        let service = PythonPluginService::new().unwrap();
        let response = service.health(Request::new(Empty {})).await.unwrap();
        let health = response.into_inner();
        assert!(!health.healthy);
    }

    #[tokio::test]
    async fn test_execute_endpoint() {
        let service = PythonPluginService::new().unwrap();

        let body = serde_json::json!({
            "code": "print('Hello from test')",
            "timeout_secs": 5
        });

        let result = service.handlers.handle_execute(body).await.unwrap();

        #[derive(Deserialize)]
        struct ExecutionResponse {
            success: bool,
            stdout: String,
            stderr: String,
        }

        let response: ExecutionResponse = serde_json::from_slice(&result.body).unwrap();
        assert!(response.success);
        assert_eq!(response.stdout, "Hello from test\n");
        assert_eq!(response.stderr, "");
    }
}
