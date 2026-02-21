//! gRPC service implementation for the Python plugin
//!
//! Implements the DomainPluginService interface for QNTX.
//!
//! TODO: uv-based package management
//! Add HTTP endpoints for installing Python packages via `uv`:
//! - POST /uv/install - Install package using `uv pip install <package>`
//! - GET /uv/check - Check if module is available
//!
//! Implementation considerations:
//! - Option A: New module qntx-python/src/uv.rs that calls uv CLI via std::process::Command
//! - Option B: Add handlers to service.rs HTTP routing
//! - Option C: Separate qntx-uv plugin for cleaner separation
//! - Option D: Go-side wrapper in plugin/python
//!
//! Decision deferred - need to evaluate which approach best fits QNTX architecture.

use crate::atsstore;
use crate::config::PluginConfig;
use crate::engine::PythonEngine;
use crate::handlers::{HandlerContext, PluginState};
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

/// Default timeout for Python job execution (5 minutes)
const DEFAULT_TIMEOUT_SECS: u64 = 300;

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
            ats_client: atsstore::new_shared_client(),
            discovered_handlers: HashMap::new(),
        }));

        Ok(Self {
            handlers: HandlerContext::new(state),
        })
    }

    /// Get Python version for health checks
    fn python_version(&self) -> String {
        self.handlers.python_version()
    }

    /// Discover handler scripts from ATS store
    /// Returns a HashMap of handler_name -> Python code
    async fn discover_handlers_from_config(
        &self,
        config: Option<PluginConfig>,
    ) -> HashMap<String, String> {
        use crate::proto::{
            ats_store_service_client::AtsStoreServiceClient, AttestationFilter,
            GetAttestationsRequest,
        };
        use tonic::transport::Channel;

        // Check if we have config with ATS store endpoint
        let config = match config {
            Some(cfg) if !cfg.ats_store_endpoint.is_empty() => cfg,
            _ => {
                info!("No ATS store endpoint configured, skipping handler discovery");
                return HashMap::new();
            }
        };

        info!("Discovering Python handlers from ATS store");

        let endpoint = config.ats_store_endpoint.clone();
        let auth_token = config.auth_token.clone();

        // Query ATS store for handler attestations
        // Filter: predicate="handler" AND context="python"
        let filter = AttestationFilter {
            subjects: vec![],
            predicates: vec!["handler".to_string()],
            contexts: vec!["python".to_string()],
            actors: vec![],
            time_start: 0,
            time_end: 0,
            limit: Some(100), // Limit to 100 handlers
        };

        let request = GetAttestationsRequest {
            auth_token,
            filter: Some(filter),
        };

        // Connect to ATS store and query
        let result: Result<HashMap<String, String>, String> =
            tokio::task::spawn_blocking(move || {
                let rt = tokio::runtime::Builder::new_current_thread()
                    .enable_all()
                    .build()
                    .map_err(|e| format!("failed to create runtime: {}", e))?;

                rt.block_on(async {
                    // Ensure endpoint has http:// scheme
                    let endpoint_uri =
                        if endpoint.starts_with("http://") || endpoint.starts_with("https://") {
                            endpoint.clone()
                        } else {
                            format!("http://{}", endpoint)
                        };

                    let channel = Channel::from_shared(endpoint_uri)
                        .map_err(|e| format!("invalid endpoint: {}", e))?
                        .connect()
                        .await
                        .map_err(|e| format!("connection failed: {}", e))?;

                    let mut client = AtsStoreServiceClient::new(channel);
                    let response = client
                        .get_attestations(request)
                        .await
                        .map_err(|e| format!("gRPC error: {}", e))?
                        .into_inner();

                    if !response.success {
                        return Err(format!("Query failed: {}", response.error));
                    }

                    // Extract handler names and code from attestations
                    let mut handlers = HashMap::new();
                    for attestation in response.attestations {
                        if let Some(handler_name) = attestation.subjects.first() {
                            // Extract Python code from attributes Struct
                            if let Some(ref attrs_struct) = attestation.attributes {
                                let attrs =
                                    qntx_proto::serde_struct::struct_to_json_map(attrs_struct);
                                if let Some(serde_json::Value::String(code)) = attrs.get("code") {
                                    handlers.insert(handler_name.clone(), code.clone());
                                } else {
                                    warn!(
                                        "Handler {} attributes missing 'code' field, skipping",
                                        handler_name
                                    );
                                }
                            } else {
                                warn!("Handler {} has no attributes, skipping", handler_name);
                            }
                        }
                    }

                    Ok(handlers)
                })
            })
            .await
            .unwrap_or_else(|e| Err(format!("task panicked: {:?}", e)));

        match result {
            Ok(handlers) => {
                info!(
                    "Discovered {} handler(s) from ATS store: {:?}",
                    handlers.len(),
                    handlers.keys().collect::<Vec<_>>()
                );
                handlers
            }
            Err(e) => {
                warn!("Failed to discover handlers from ATS store: {}", e);
                HashMap::new()
            }
        }
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
    ) -> Result<Response<InitializeResponse>, Status> {
        let req = request.into_inner();
        info!("Initializing Python plugin");
        info!("ATSStore endpoint: {}", req.ats_store_endpoint);
        info!("Queue endpoint: {}", req.queue_endpoint);

        // Clone config for later use after dropping lock
        let state_config = {
            let mut state = self.handlers.state.write();

            // Store configuration
            state.config = Some(PluginConfig {
                ats_store_endpoint: req.ats_store_endpoint.clone(),
                queue_endpoint: req.queue_endpoint,
                auth_token: req.auth_token.clone(),
                config: req.config,
            });

            // Initialize ATSStore client if endpoint is provided
            if !req.ats_store_endpoint.is_empty() {
                info!("Initializing ATSStore client for Python attestation support");
                atsstore::init_shared_client(
                    &state.ats_client,
                    atsstore::AtsStoreConfig {
                        endpoint: req.ats_store_endpoint,
                        auth_token: req.auth_token,
                    },
                );
            }

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
                state.default_modules = modules_str
                    .split(',')
                    .map(|s| s.trim().to_string())
                    .collect();
                info!(
                    "Using configured default modules: {:?}",
                    state.default_modules
                );
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

            // Clone config before dropping lock
            state.config.clone()
        }; // Lock automatically dropped here

        // Discover handler scripts from ATS store
        let discovered_handlers = self.discover_handlers_from_config(state_config).await;

        // Store discovered handlers in plugin state
        {
            let mut state = self.handlers.state.write();
            state.discovered_handlers = discovered_handlers.clone();
        }

        // Announce async handler capabilities
        // Start with built-in handlers
        let mut handler_names = vec!["python.script".to_string()];

        // Add discovered handlers with python. prefix (sorted for determinism)
        let mut sorted_handlers: Vec<_> = discovered_handlers.keys().collect();
        sorted_handlers.sort();
        for handler_name in sorted_handlers {
            handler_names.push(format!("python.{}", handler_name));
        }

        info!("Announcing async handlers: {:?}", handler_names);

        Ok(Response::new(InitializeResponse { handler_names }))
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

        // Add binary build time
        if let Ok(exe_path) = std::env::current_exe() {
            if let Ok(metadata) = std::fs::metadata(&exe_path) {
                if let Ok(modified) = metadata.modified() {
                    if let Ok(duration) = modified.duration_since(std::time::UNIX_EPOCH) {
                        details.insert("binary_built".to_string(), duration.as_secs().to_string());
                    }
                }
            }
        }

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

    /// Execute an async job
    /// Routes to appropriate handler based on handler_name
    async fn execute_job(
        &self,
        request: Request<ExecuteJobRequest>,
    ) -> Result<Response<ExecuteJobResponse>, Status> {
        let req = request.into_inner();

        debug!(
            "ExecuteJob request: job_id={}, handler={}",
            req.job_id, req.handler_name
        );

        // Clone handler name to avoid borrow issues
        let handler_name = req.handler_name.clone();

        // Route to handler based on handler_name
        if handler_name == "python.script" {
            self.execute_python_script_job(req).await
        } else if let Some(stripped) = handler_name.strip_prefix("python.") {
            // Strip python. prefix to get handler name
            let handler_key = stripped.to_string();
            self.execute_discovered_handler_job(req, &handler_key).await
        } else {
            Err(Status::not_found(format!(
                "Unknown handler: {}",
                handler_name
            )))
        }
    }
}

// Helper methods for PythonPluginService
impl PythonPluginService {
    /// Execute a python.script job
    async fn execute_python_script_job(
        &self,
        req: ExecuteJobRequest,
    ) -> Result<Response<ExecuteJobResponse>, Status> {
        use crate::engine::ExecutionConfig;

        // Parse payload as JSON containing script_code
        #[derive(serde::Deserialize)]
        struct PythonScriptPayload {
            content: String,
        }

        let payload: PythonScriptPayload = serde_json::from_slice(&req.payload)
            .map_err(|e| Status::invalid_argument(format!("Invalid payload JSON: {}", e)))?;

        if payload.content.is_empty() {
            return Err(Status::invalid_argument("Missing content in payload"));
        }

        // Execute the Python script
        let config = ExecutionConfig {
            timeout_secs: if req.timeout_secs > 0 {
                req.timeout_secs as u64
            } else {
                DEFAULT_TIMEOUT_SECS
            },
            capture_variables: false,
            python_paths: vec![],
            ..Default::default()
        };

        let result = {
            let state = self.handlers.state.read();
            state.engine.execute_with_ats(
                &payload.content,
                &config,
                Some(state.ats_client.clone()),
                None,
            )
        };

        // Convert execution result to ExecuteJobResponse
        if result.success {
            // Serialize result as JSON for the result field
            let result_json = serde_json::json!({
                "stdout": result.stdout,
                "stderr": result.stderr,
                "duration_ms": result.duration_ms,
                "result": result.result,
            });

            let result_bytes = serde_json::to_vec(&result_json)
                .map_err(|e| Status::internal(format!("Failed to serialize result: {}", e)))?;

            Ok(Response::new(ExecuteJobResponse {
                success: true,
                error: String::new(),
                result: result_bytes,
                progress_current: 0,
                progress_total: 0,
                cost_actual: 0.0,
            }))
        } else {
            // Execution failed
            let error_msg = result.error.unwrap_or_else(|| "Unknown error".to_string());

            Ok(Response::new(ExecuteJobResponse {
                success: false,
                error: error_msg,
                result: vec![],
                progress_current: 0,
                progress_total: 0,
                cost_actual: 0.0,
            }))
        }
    }

    /// Execute a dynamically discovered handler job
    async fn execute_discovered_handler_job(
        &self,
        req: ExecuteJobRequest,
        handler_key: &str,
    ) -> Result<Response<ExecuteJobResponse>, Status> {
        use crate::engine::ExecutionConfig;

        // Retrieve handler code from plugin state
        let script_code = {
            let state = self.handlers.state.read();
            state.discovered_handlers.get(handler_key).cloned()
        };

        let script_code = script_code.ok_or_else(|| {
            Status::not_found(format!(
                "Handler {} not found in discovered handlers",
                handler_key
            ))
        })?;

        // Execute the Python script
        let config = ExecutionConfig {
            timeout_secs: if req.timeout_secs > 0 {
                req.timeout_secs as u64
            } else {
                DEFAULT_TIMEOUT_SECS
            },
            capture_variables: false,
            python_paths: vec![],
            ..Default::default()
        };

        let result = {
            let state = self.handlers.state.read();
            state.engine.execute_with_ats(
                &script_code,
                &config,
                Some(state.ats_client.clone()),
                None,
            )
        };

        // Convert execution result to ExecuteJobResponse
        if result.success {
            // Serialize result as JSON for the result field
            let result_json = serde_json::json!({
                "stdout": result.stdout,
                "stderr": result.stderr,
                "duration_ms": result.duration_ms,
                "result": result.result,
            });

            let result_bytes = serde_json::to_vec(&result_json)
                .map_err(|e| Status::internal(format!("Failed to serialize result: {}", e)))?;

            Ok(Response::new(ExecuteJobResponse {
                success: true,
                error: String::new(),
                result: result_bytes,
                progress_current: 0,
                progress_total: 0,
                cost_actual: 0.0,
            }))
        } else {
            // Execution failed
            let error_msg = result.error.unwrap_or_else(|| "Unknown error".to_string());

            Ok(Response::new(ExecuteJobResponse {
                success: false,
                error: error_msg,
                result: vec![],
                progress_current: 0,
                progress_total: 0,
                cost_actual: 0.0,
            }))
        }
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
            "content": "print('Hello from test')",
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

    #[tokio::test]
    async fn test_attest_function_available() {
        let service = PythonPluginService::new().unwrap();

        // Test that the attest function exists in the Python namespace
        // It will error when called since ATSStore is not initialized,
        // but it should be defined and callable.
        let body = serde_json::json!({
            "content": "result = callable(attest)\nprint('attest is callable:', result)",
            "timeout_secs": 5
        });

        let result = service.handlers.handle_execute(body).await.unwrap();

        #[derive(Deserialize)]
        struct ExecutionResponse {
            success: bool,
            stdout: String,
            error: Option<String>,
        }

        let response: ExecutionResponse = serde_json::from_slice(&result.body).unwrap();
        assert!(
            response.success,
            "Expected success, got error: {:?}",
            response.error
        );
        assert!(response.stdout.contains("attest is callable: True"));
    }

    #[tokio::test]
    async fn test_attest_without_atsstore_errors() {
        let service = PythonPluginService::new().unwrap();

        // When ATSStore is not initialized, calling attest should fail gracefully
        let body = serde_json::json!({
            "content": r#"
try:
    attest(['subject'], ['predicate'], ['context'])
    print('ERROR: should have raised')
except RuntimeError as e:
    print('Got expected error:', str(e))
"#,
            "timeout_secs": 5
        });

        let result = service.handlers.handle_execute(body).await.unwrap();

        #[derive(Deserialize)]
        struct ExecutionResponse {
            success: bool,
            stdout: String,
        }

        let response: ExecutionResponse = serde_json::from_slice(&result.body).unwrap();
        assert!(response.success);
        assert!(response.stdout.contains("Got expected error"));
    }
}
