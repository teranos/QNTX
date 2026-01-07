//! gRPC service implementation for the Python plugin
//!
//! Implements the DomainPluginService interface for QNTX.

use crate::engine::{ExecutionConfig, ExecutionResult, PythonEngine};
use crate::proto::{
    domain_plugin_service_server::DomainPluginService, Empty, HealthResponse, HttpHeader,
    HttpRequest, HttpResponse, InitializeRequest, MetadataResponse, WebSocketMessage,
};
use parking_lot::RwLock;
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::pin::Pin;
use std::sync::Arc;
use tokio_stream::Stream;
use tonic::{Request, Response, Status, Streaming};
use tracing::{debug, error, info, warn};

/// Plugin configuration received during initialization
#[derive(Debug, Clone, Default)]
pub struct PluginConfig {
    /// ATSStore gRPC endpoint
    pub ats_store_endpoint: String,
    /// Queue service gRPC endpoint
    pub queue_endpoint: String,
    /// Auth token for service calls
    pub auth_token: String,
    /// Custom configuration values
    pub config: HashMap<String, String>,
}

/// State of the Python plugin
struct PluginState {
    /// Plugin configuration
    config: Option<PluginConfig>,
    /// Python engine
    engine: PythonEngine,
    /// Whether the plugin is initialized
    initialized: bool,
}

/// Python plugin gRPC service
pub struct PythonPluginService {
    state: Arc<RwLock<PluginState>>,
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
        Ok(Self {
            state: Arc::new(RwLock::new(PluginState {
                config: None,
                engine,
                initialized: false,
            })),
        })
    }

    /// Get Python version for health checks
    fn python_version(&self) -> String {
        let state = self.state.read();
        state.engine.python_version()
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

        let mut state = self.state.write();

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
        let mut state = self.state.write();
        state.initialized = false;
        state.config = None;
        Ok(Response::new(Empty {}))
    }

    /// Handle HTTP requests
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
            ("POST", "/execute") => self.handle_execute(body).await,
            ("POST", "/evaluate") => self.handle_evaluate(body).await,
            ("POST", "/execute-file") => self.handle_execute_file(body).await,

            // Package management
            ("POST", "/pip/install") => self.handle_pip_install(body).await,
            ("GET", "/pip/check") => self.handle_pip_check(body).await,

            // Info endpoints
            ("GET", "/version") => self.handle_version().await,
            ("GET", "/modules") => self.handle_modules(body).await,

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
        let state = self.state.read();
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
}

// HTTP endpoint handlers
impl PythonPluginService {
    /// Handle POST /execute - Execute Python code
    async fn handle_execute(&self, body: serde_json::Value) -> Result<HttpResponse, Status> {
        #[derive(Deserialize)]
        struct ExecuteRequest {
            code: String,
            #[serde(default)]
            timeout_secs: Option<u64>,
            #[serde(default)]
            capture_variables: Option<bool>,
            #[serde(default)]
            python_paths: Option<Vec<String>>,
        }

        let req: ExecuteRequest = serde_json::from_value(body)
            .map_err(|e| Status::invalid_argument(format!("Invalid request: {}", e)))?;

        if req.code.is_empty() {
            return Err(Status::invalid_argument("Missing 'code' field"));
        }

        let config = ExecutionConfig {
            timeout_secs: req.timeout_secs.unwrap_or(30),
            capture_variables: req.capture_variables.unwrap_or(false),
            python_paths: req.python_paths.unwrap_or_default(),
            ..Default::default()
        };

        let result = {
            let state = self.state.read();
            state.engine.execute(&req.code, &config)
        };

        self.execution_result_to_response(result)
    }

    /// Handle POST /evaluate - Evaluate a Python expression
    async fn handle_evaluate(&self, body: serde_json::Value) -> Result<HttpResponse, Status> {
        #[derive(Deserialize)]
        struct EvaluateRequest {
            expr: String,
        }

        let req: EvaluateRequest = serde_json::from_value(body)
            .map_err(|e| Status::invalid_argument(format!("Invalid request: {}", e)))?;

        if req.expr.is_empty() {
            return Err(Status::invalid_argument("Missing 'expr' field"));
        }

        let result = {
            let state = self.state.read();
            state.engine.evaluate(&req.expr)
        };

        self.execution_result_to_response(result)
    }

    /// Handle POST /execute-file - Execute a Python file
    async fn handle_execute_file(&self, body: serde_json::Value) -> Result<HttpResponse, Status> {
        #[derive(Deserialize)]
        struct ExecuteFileRequest {
            path: String,
            #[serde(default)]
            timeout_secs: Option<u64>,
            #[serde(default)]
            capture_variables: Option<bool>,
        }

        let req: ExecuteFileRequest = serde_json::from_value(body)
            .map_err(|e| Status::invalid_argument(format!("Invalid request: {}", e)))?;

        if req.path.is_empty() {
            return Err(Status::invalid_argument("Missing 'path' field"));
        }

        let config = ExecutionConfig {
            timeout_secs: req.timeout_secs.unwrap_or(30),
            capture_variables: req.capture_variables.unwrap_or(false),
            ..Default::default()
        };

        let result = {
            let state = self.state.read();
            state.engine.execute_file(&req.path, &config)
        };

        self.execution_result_to_response(result)
    }

    /// Handle POST /pip/install - Install a Python package
    async fn handle_pip_install(&self, body: serde_json::Value) -> Result<HttpResponse, Status> {
        #[derive(Deserialize)]
        struct PipInstallRequest {
            package: String,
        }

        let req: PipInstallRequest = serde_json::from_value(body)
            .map_err(|e| Status::invalid_argument(format!("Invalid request: {}", e)))?;

        if req.package.is_empty() {
            return Err(Status::invalid_argument("Missing 'package' field"));
        }

        let result = {
            let state = self.state.read();
            state.engine.pip_install(&req.package)
        };

        self.execution_result_to_response(result)
    }

    /// Handle GET /pip/check - Check if a module is available
    async fn handle_pip_check(&self, body: serde_json::Value) -> Result<HttpResponse, Status> {
        #[derive(Deserialize)]
        struct PipCheckRequest {
            module: String,
        }

        let req: PipCheckRequest = serde_json::from_value(body)
            .map_err(|e| Status::invalid_argument(format!("Invalid request: {}", e)))?;

        if req.module.is_empty() {
            return Err(Status::invalid_argument("Missing 'module' field"));
        }

        let available = {
            let state = self.state.read();
            state.engine.check_module(&req.module)
        };

        #[derive(Serialize)]
        struct PipCheckResponse {
            module: String,
            available: bool,
        }

        let response = PipCheckResponse {
            module: req.module,
            available,
        };

        self.json_response(200, &response)
    }

    /// Handle GET /version - Get Python version info
    async fn handle_version(&self) -> Result<HttpResponse, Status> {
        #[derive(Serialize)]
        struct VersionResponse {
            python_version: String,
            plugin_version: String,
        }

        let response = VersionResponse {
            python_version: self.python_version(),
            plugin_version: env!("CARGO_PKG_VERSION").to_string(),
        };

        self.json_response(200, &response)
    }

    /// Handle GET /modules - Check availability of common modules
    async fn handle_modules(&self, body: serde_json::Value) -> Result<HttpResponse, Status> {
        #[derive(Deserialize, Default)]
        struct ModulesRequest {
            #[serde(default)]
            modules: Option<Vec<String>>,
        }

        let req: ModulesRequest = serde_json::from_value(body).unwrap_or_default();

        // Default modules to check
        let modules_to_check: Vec<String> = req.modules.unwrap_or_else(|| {
            vec![
                "numpy".to_string(),
                "pandas".to_string(),
                "requests".to_string(),
                "json".to_string(),
                "os".to_string(),
                "sys".to_string(),
            ]
        });

        let state = self.state.read();
        let mut available = HashMap::new();

        for module in modules_to_check {
            available.insert(module.clone(), state.engine.check_module(&module));
        }

        #[derive(Serialize)]
        struct ModulesResponse {
            modules: HashMap<String, bool>,
        }

        let response = ModulesResponse { modules: available };

        self.json_response(200, &response)
    }

    /// Convert ExecutionResult to HttpResponse
    fn execution_result_to_response(
        &self,
        result: ExecutionResult,
    ) -> Result<HttpResponse, Status> {
        #[derive(Serialize)]
        struct ExecutionResponse {
            success: bool,
            stdout: String,
            stderr: String,
            result: Option<serde_json::Value>,
            error: Option<String>,
            duration_ms: u64,
            #[serde(skip_serializing_if = "HashMap::is_empty")]
            variables: HashMap<String, String>,
        }

        let response = ExecutionResponse {
            success: result.success,
            stdout: result.stdout,
            stderr: result.stderr,
            result: result.result,
            error: result.error,
            duration_ms: result.duration_ms,
            variables: result.variables,
        };

        let status_code = if result.success { 200 } else { 400 };
        self.json_response(status_code, &response)
    }

    /// Create a JSON HTTP response
    fn json_response<T: Serialize>(
        &self,
        status_code: i32,
        data: &T,
    ) -> Result<HttpResponse, Status> {
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
}

#[cfg(test)]
mod tests {
    use super::*;

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

        let result = service.handle_execute(body).await.unwrap();

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
