//! HTTP endpoint handlers for the Python plugin
//!
//! Implements handlers for /execute, /evaluate, /pip/*, /version, /modules endpoints.

use crate::engine::{ExecutionConfig, ExecutionResult, PythonEngine};
use crate::proto::{HttpHeader, HttpResponse};
use parking_lot::RwLock;
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::sync::Arc;
use tonic::Status;

use crate::config::PluginConfig;

/// Default Python modules to check for availability
pub(crate) const DEFAULT_MODULES: &[&str] = &["numpy", "pandas", "requests", "json", "os", "sys"];

/// Internal plugin state - shared with service module
pub(crate) struct PluginState {
    pub config: Option<PluginConfig>,
    pub engine: PythonEngine,
    pub initialized: bool,
    /// Default modules to check (can be overridden via plugin config)
    pub default_modules: Vec<String>,
}

/// Handler context providing access to plugin state
pub struct HandlerContext {
    pub(crate) state: Arc<RwLock<PluginState>>,
}

impl HandlerContext {
    pub fn new(state: Arc<RwLock<PluginState>>) -> Self {
        Self { state }
    }

    /// Get Python version for health checks
    pub fn python_version(&self) -> String {
        let state = self.state.read();
        state.engine.python_version()
    }

    /// Handle POST /execute - Execute Python code
    pub async fn handle_execute(&self, body: serde_json::Value) -> Result<HttpResponse, Status> {
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

        execution_result_to_response(result)
    }

    /// Handle POST /evaluate - Evaluate a Python expression
    pub async fn handle_evaluate(&self, body: serde_json::Value) -> Result<HttpResponse, Status> {
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

        execution_result_to_response(result)
    }

    /// Handle POST /execute-file - Execute a Python file
    pub async fn handle_execute_file(
        &self,
        body: serde_json::Value,
    ) -> Result<HttpResponse, Status> {
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

        execution_result_to_response(result)
    }

    /// Handle POST /pip/install - Install a Python package
    pub async fn handle_pip_install(
        &self,
        body: serde_json::Value,
    ) -> Result<HttpResponse, Status> {
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

        execution_result_to_response(result)
    }

    /// Handle GET /pip/check - Check if a module is available
    pub async fn handle_pip_check(&self, body: serde_json::Value) -> Result<HttpResponse, Status> {
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

        json_response(200, &response)
    }

    /// Handle GET /version - Get Python version info
    pub async fn handle_version(&self) -> Result<HttpResponse, Status> {
        #[derive(Serialize)]
        struct VersionResponse {
            python_version: String,
            plugin_version: String,
        }

        let response = VersionResponse {
            python_version: self.python_version(),
            plugin_version: env!("CARGO_PKG_VERSION").to_string(),
        };

        json_response(200, &response)
    }

    /// Handle GET /modules - Check availability of common modules
    pub async fn handle_modules(&self, body: serde_json::Value) -> Result<HttpResponse, Status> {
        #[derive(Deserialize, Default)]
        struct ModulesRequest {
            #[serde(default)]
            modules: Option<Vec<String>>,
        }

        let req: ModulesRequest = serde_json::from_value(body).unwrap_or_default();

        let state = self.state.read();

        // Use modules from request, fall back to configured default modules
        let modules_to_check: Vec<String> = req
            .modules
            .unwrap_or_else(|| state.default_modules.clone());

        let mut available = HashMap::new();

        for module in modules_to_check {
            available.insert(module.clone(), state.engine.check_module(&module));
        }

        #[derive(Serialize)]
        struct ModulesResponse {
            modules: HashMap<String, bool>,
        }

        let response = ModulesResponse { modules: available };

        json_response(200, &response)
    }
}

/// Convert ExecutionResult to HttpResponse
fn execution_result_to_response(result: ExecutionResult) -> Result<HttpResponse, Status> {
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
    json_response(status_code, &response)
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
