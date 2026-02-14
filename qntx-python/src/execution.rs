//! Python code execution - execute, evaluate, and file execution
//!
//! Provides execution capabilities for the PythonEngine.

use crate::atsstore::{self, SharedAtsStoreClient};
use crate::engine::{ExecutionConfig, ExecutionResult, PythonEngine};
use pyo3::prelude::*;
use pyo3::types::{PyDict, PyList};
use qntx_grpc::error::Error;
use std::collections::HashMap;
use std::ffi::CString;

/// Maximum length for captured variable values before truncation
const MAX_VARIABLE_LENGTH: usize = 1000;

/// Suffix appended to truncated variable values
const TRUNCATION_SUFFIX: &str = "...";

impl PythonEngine {
    /// Execute Python code and return the result
    pub fn execute(&self, code: &str, config: &ExecutionConfig) -> ExecutionResult {
        self.execute_with_ats(code, config, None, None)
    }

    /// Execute Python code with optional ATSStore client for attestation support.
    /// When an ATSStore client is provided, the `attest()` function becomes available
    /// in the Python execution context. When `upstream_attestation` is provided, it is
    /// injected as a Python dict global named `upstream` (or `None` when absent).
    pub fn execute_with_ats(
        &self,
        code: &str,
        config: &ExecutionConfig,
        ats_client: Option<SharedAtsStoreClient>,
        upstream_attestation: Option<&serde_json::Value>,
    ) -> ExecutionResult {
        let start = std::time::Instant::now();

        // Set up ATSStore client for this execution if provided
        if let Some(ref client) = ats_client {
            atsstore::set_current_client(client.clone());
        }

        let result = self.execute_inner(code, config, ats_client.is_some(), upstream_attestation);

        // Clean up ATSStore client
        if ats_client.is_some() {
            atsstore::clear_current_client();
        }

        let duration_ms = start.elapsed().as_millis() as u64;

        match result {
            Ok(mut res) => {
                res.duration_ms = duration_ms;
                res
            }
            Err(e) => ExecutionResult {
                success: false,
                stdout: String::new(),
                stderr: String::new(),
                result: None,
                error: Some(e.to_string()),
                duration_ms,
                variables: HashMap::new(),
            },
        }
    }

    fn execute_inner(
        &self,
        code: &str,
        config: &ExecutionConfig,
        inject_attest: bool,
        upstream_attestation: Option<&serde_json::Value>,
    ) -> Result<ExecutionResult, Error> {
        // Create CString for the code
        let code_cstr = CString::new(code)
            .map_err(|e| Error::context("invalid code string contains null bytes", e))?;

        Python::with_gil(|py| {
            // Set up output capture
            let io = py
                .import("io")
                .map_err(|e| Error::context("failed to import io module", e))?;
            let sys = py
                .import("sys")
                .map_err(|e| Error::context("failed to import sys module", e))?;

            // Create StringIO objects for capturing output
            let stdout_capture = io
                .call_method0("StringIO")
                .map_err(|e| Error::context("failed to create stdout StringIO object", e))?;
            let stderr_capture = io
                .call_method0("StringIO")
                .map_err(|e| Error::context("failed to create stderr StringIO object", e))?;

            // Save original stdout/stderr
            let original_stdout = sys
                .getattr("stdout")
                .map_err(|e| Error::context("failed to get original sys.stdout", e))?;
            let original_stderr = sys
                .getattr("stderr")
                .map_err(|e| Error::context("failed to get original sys.stderr", e))?;

            // Redirect stdout/stderr
            sys.setattr("stdout", &stdout_capture)
                .map_err(|e| Error::context("failed to redirect sys.stdout to StringIO", e))?;
            sys.setattr("stderr", &stderr_capture)
                .map_err(|e| Error::context("failed to redirect sys.stderr to StringIO", e))?;

            // Create execution globals
            let globals = PyDict::new(py);

            // Add builtins
            let builtins = py
                .import("builtins")
                .map_err(|e| Error::context("failed to import builtins module", e))?;
            globals.set_item("__builtins__", builtins).map_err(|e| {
                Error::context("failed to set __builtins__ in execution globals", e)
            })?;

            // Add custom paths if specified
            for path in &config.python_paths {
                let path_list: Bound<'_, PyList> = sys
                    .getattr("path")
                    .map_err(|e| Error::context("failed to get sys.path", e))?
                    .extract()
                    .map_err(|e| Error::context("failed to extract sys.path as list", e))?;
                let _ = path_list.insert(0, path);
            }

            // Inject attest() and query() functions if ATSStore client is available
            if inject_attest {
                atsstore::inject_ats_functions(py, &globals)
                    .map_err(|e| Error::context("failed to inject attestation functions", e))?;
            }

            // Inject upstream attestation as Python dict (or None)
            match upstream_attestation {
                Some(attestation) => {
                    let json_module = py
                        .import("json")
                        .map_err(|e| Error::context("failed to import json module", e))?;
                    let json_str = serde_json::to_string(attestation).map_err(|e| {
                        Error::context("failed to serialize upstream attestation", e)
                    })?;
                    let upstream = json_module
                        .call_method1("loads", (json_str,))
                        .map_err(|e| {
                            Error::context("failed to parse upstream attestation as Python dict", e)
                        })?;
                    globals
                        .set_item("upstream", upstream)
                        .map_err(|e| Error::context("failed to set upstream global", e))?;
                }
                None => {
                    globals
                        .set_item("upstream", py.None())
                        .map_err(|e| Error::context("failed to set upstream = None", e))?;
                }
            }

            // Execute the code using py.run
            let exec_result = py.run(code_cstr.as_c_str(), Some(&globals), None);

            // Restore stdout/stderr
            let _ = sys.setattr("stdout", original_stdout);
            let _ = sys.setattr("stderr", original_stderr);

            // Get captured output
            let stdout: String = stdout_capture
                .call_method0("getvalue")
                .and_then(|v| v.extract())
                .unwrap_or_default();
            let stderr: String = stderr_capture
                .call_method0("getvalue")
                .and_then(|v| v.extract())
                .unwrap_or_default();

            // Handle execution result
            match exec_result {
                Ok(_) => {
                    // Try to get the last expression result if there's a _result variable
                    let result_value = globals
                        .get_item("_result")
                        .ok()
                        .flatten()
                        .and_then(|v| python_to_json(py, &v).ok());

                    // Capture variables if requested
                    let variables = if config.capture_variables {
                        capture_variables(&globals)
                    } else {
                        HashMap::new()
                    };

                    Ok(ExecutionResult {
                        success: true,
                        stdout,
                        stderr,
                        result: result_value,
                        error: None,
                        duration_ms: 0,
                        variables,
                    })
                }
                Err(e) => {
                    // Capture full traceback for better debugging
                    let error_msg = format_python_error(py, &e);
                    Ok(ExecutionResult {
                        success: false,
                        stdout,
                        stderr,
                        result: None,
                        error: Some(error_msg),
                        duration_ms: 0,
                        variables: HashMap::new(),
                    })
                }
            }
        })
    }

    /// Execute a Python file
    ///
    /// TODO(sec): Consider path validation to restrict execution to allowed directories.
    /// Currently reads arbitrary filesystem paths which may be a security concern
    /// depending on deployment context.
    pub fn execute_file(&self, path: &str, config: &ExecutionConfig) -> ExecutionResult {
        self.execute_file_with_ats(path, config, None)
    }

    /// Execute a Python file with optional ATSStore client for attestation support.
    pub fn execute_file_with_ats(
        &self,
        path: &str,
        config: &ExecutionConfig,
        ats_client: Option<SharedAtsStoreClient>,
    ) -> ExecutionResult {
        // TODO(sec): Validate path is within allowed directories if config.allow_fs is false
        match std::fs::read_to_string(path) {
            Ok(code) => self.execute_with_ats(&code, config, ats_client, None),
            Err(e) => ExecutionResult {
                success: false,
                stdout: String::new(),
                stderr: String::new(),
                result: None,
                error: Some(format!("Failed to read file {}: {}", path, e)),
                duration_ms: 0,
                variables: HashMap::new(),
            },
        }
    }

    /// Evaluate a Python expression and return its value
    pub fn evaluate(&self, expr: &str) -> ExecutionResult {
        self.evaluate_with_ats(expr, None)
    }

    /// Evaluate a Python expression with optional ATSStore client.
    pub fn evaluate_with_ats(
        &self,
        expr: &str,
        ats_client: Option<SharedAtsStoreClient>,
    ) -> ExecutionResult {
        // Wrap expression to capture result
        let code = format!("_result = ({})", expr);
        self.execute_with_ats(&code, &ExecutionConfig::default(), ats_client, None)
    }

    /// Install a package using pip (if allowed)
    ///
    /// TODO(sec): Validate package name against PEP 508 pattern before execution.
    /// Current escaping only handles quotes - malicious package names could potentially
    /// cause issues. Consider: `^[a-zA-Z0-9]([a-zA-Z0-9._-]*[a-zA-Z0-9])?$`
    pub fn pip_install(&self, package: &str) -> ExecutionResult {
        // TODO(sec): Add package name validation
        let code = format!(
            r#"
import subprocess
import sys
result = subprocess.run(
    [sys.executable, "-m", "pip", "install", "{}"],
    capture_output=True,
    text=True
)
print(result.stdout)
if result.stderr:
    import sys
    print(result.stderr, file=sys.stderr)
_result = result.returncode == 0
"#,
            package.replace('"', r#"\""#)
        );
        self.execute(&code, &ExecutionConfig::default())
    }
}

/// Format a Python error with full traceback for better debugging
fn format_python_error(py: Python<'_>, err: &PyErr) -> String {
    // Try to get the full traceback using Python's traceback module
    if let Some(tb) = err.traceback(py) {
        if let Ok(traceback_mod) = py.import("traceback") {
            if let Ok(lines) = traceback_mod
                .call_method1("format_exception", (err.get_type(py), err.value(py), tb))
            {
                if let Ok(iter) = lines.try_iter() {
                    let formatted: Vec<String> = iter
                        .filter_map(|line| line.ok())
                        .filter_map(|line| line.extract::<String>().ok())
                        .collect();
                    if !formatted.is_empty() {
                        return formatted.join("");
                    }
                }
            }
        }
    }
    // Fallback to simple error message
    format!("{}", err)
}

/// Convert a Python object to JSON
fn python_to_json(py: Python<'_>, obj: &Bound<'_, PyAny>) -> Result<serde_json::Value, Error> {
    // Try to use json.dumps for serialization
    let json_module = py
        .import("json")
        .map_err(|e| Error::context("failed to import json module", e))?;

    match json_module.call_method1("dumps", (obj,)) {
        Ok(json_str) => {
            let s: String = json_str
                .extract()
                .map_err(|e| Error::context("failed to extract JSON string from Python", e))?;
            serde_json::from_str(&s)
                .map_err(|e| Error::context("failed to parse Python JSON output", e))
        }
        Err(_) => {
            // Fallback to string representation
            let repr: String = obj
                .repr()
                .and_then(|r| r.extract())
                .unwrap_or_else(|_| "<unknown>".to_string());
            Ok(serde_json::Value::String(repr))
        }
    }
}

/// Capture variables from execution scope
fn capture_variables(globals: &Bound<'_, PyDict>) -> HashMap<String, String> {
    let mut vars = HashMap::new();

    for (key, value) in globals.iter() {
        let key_str: String = match key.extract() {
            Ok(s) => s,
            Err(_) => continue,
        };

        // Skip private/magic variables
        if key_str.starts_with('_') {
            continue;
        }

        // Get string representation
        let value_str: String = value
            .repr()
            .and_then(|r| r.extract())
            .unwrap_or_else(|_| "<unknown>".to_string());

        // Limit size
        let value_str = if value_str.len() > MAX_VARIABLE_LENGTH {
            format!("{}{}", &value_str[..MAX_VARIABLE_LENGTH], TRUNCATION_SUFFIX)
        } else {
            value_str
        };

        vars.insert(key_str, value_str);
    }

    vars
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_simple_execution() {
        let engine = PythonEngine::new().unwrap();
        let result = engine.execute("print('Hello, World!')", &ExecutionConfig::default());
        assert!(result.success);
        assert_eq!(result.stdout.trim(), "Hello, World!");
    }

    #[test]
    fn test_expression_evaluation() {
        let engine = PythonEngine::new().unwrap();
        let result = engine.evaluate("1 + 2");
        assert!(result.success);
        assert_eq!(result.result, Some(serde_json::json!(3)));
    }

    #[test]
    fn test_syntax_error() {
        let engine = PythonEngine::new().unwrap();
        let result = engine.execute("def foo(", &ExecutionConfig::default());
        assert!(!result.success);
        assert!(result.error.is_some());
    }

    #[test]
    fn test_variable_capture() {
        let engine = PythonEngine::new().unwrap();
        let config = ExecutionConfig {
            capture_variables: true,
            ..Default::default()
        };
        let result = engine.execute("x = 42\ny = 'hello'", &config);
        assert!(result.success);
        assert!(result.variables.contains_key("x"));
        assert!(result.variables.contains_key("y"));
    }
}
