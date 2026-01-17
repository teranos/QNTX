//! Python engine core - initialization and state management
//!
//! Provides the core PythonEngine struct and types for Python code execution.

use parking_lot::Mutex;
use pyo3::prelude::*;
use pyo3::types::PyList;
use qntx::error::{Error, ErrorContext};
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::sync::Arc;

/// Default execution timeout in seconds
pub const DEFAULT_TIMEOUT_SECS: u64 = 30;

/// Result of Python code execution
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ExecutionResult {
    /// Whether execution succeeded
    pub success: bool,

    /// Captured stdout output
    pub stdout: String,

    /// Captured stderr output
    pub stderr: String,

    /// Return value as JSON (if any)
    pub result: Option<serde_json::Value>,

    /// Error message (if failed)
    pub error: Option<String>,

    /// Execution time in milliseconds
    pub duration_ms: u64,

    /// Variables in the execution scope (for REPL-like usage)
    pub variables: HashMap<String, String>,
}

/// Configuration for Python execution
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ExecutionConfig {
    /// Timeout in seconds (0 = no timeout)
    /// NOTE: Not yet implemented - executions run to completion
    pub timeout_secs: u64,

    /// Whether to capture variables after execution
    pub capture_variables: bool,

    /// Additional Python paths to add to sys.path
    pub python_paths: Vec<String>,

    /// Environment variables to set
    /// NOTE: Not yet implemented
    pub env_vars: HashMap<String, String>,

    /// Whether to allow file system access
    /// NOTE: Not yet implemented - all executions have full fs access
    pub allow_fs: bool,

    /// Whether to allow network access
    /// NOTE: Not yet implemented - all executions have full network access
    pub allow_network: bool,
}

impl Default for ExecutionConfig {
    fn default() -> Self {
        Self {
            timeout_secs: DEFAULT_TIMEOUT_SECS,
            capture_variables: false,
            python_paths: Vec::new(),
            env_vars: HashMap::new(),
            allow_fs: true,
            allow_network: true,
        }
    }
}

/// Internal engine state
pub(crate) struct EngineState {
    pub initialized: bool,
    pub python_paths: Vec<String>,
}

/// Python execution engine
pub struct PythonEngine {
    /// Shared state for the interpreter
    pub(crate) state: Arc<Mutex<EngineState>>,
}

impl PythonEngine {
    /// Create a new Python engine
    pub fn new() -> Result<Self, Error> {
        Ok(Self {
            state: Arc::new(Mutex::new(EngineState {
                initialized: false,
                python_paths: Vec::new(),
            })),
        })
    }

    /// Initialize the Python interpreter with optional paths
    pub fn initialize(&self, python_paths: Vec<String>) -> Result<(), Error> {
        let mut state = self.state.lock();

        if state.initialized {
            return Ok(());
        }

        // PyO3 auto-initializes Python, but we need to set up paths
        Python::with_gil(|py| {
            // Add custom paths to sys.path
            if !python_paths.is_empty() {
                let sys = py
                    .import("sys")
                    .map_err(|e| Error::context("failed to import sys module", e))?;

                let path: Bound<'_, PyList> = sys
                    .getattr("path")
                    .map_err(|e| Error::context("failed to get sys.path", e))?
                    .extract()
                    .map_err(|e| Error::context("failed to extract sys.path as list", e))?;

                for p in &python_paths {
                    path.insert(0, p).map_err(|e| {
                        Error::context(format!("failed to add path '{}' to sys.path", p), e)
                    })?;
                }
            }

            Ok::<(), Error>(())
        })?;

        state.initialized = true;
        state.python_paths = python_paths;

        Ok(())
    }

    /// Check if a Python module is available
    pub fn check_module(&self, module_name: &str) -> bool {
        Python::with_gil(|py| py.import(module_name).is_ok())
    }

    /// Get Python version info
    pub fn python_version(&self) -> String {
        Python::with_gil(|py| {
            let sys = py.import("sys").ok();
            sys.and_then(|s| s.getattr("version").ok())
                .and_then(|v| v.extract().ok())
                .unwrap_or_else(|| "unknown".to_string())
        })
    }
}

impl Default for PythonEngine {
    fn default() -> Self {
        Self::new().expect("Failed to create Python engine")
    }
}
