//! QNTX Python Plugin
//!
//! A gRPC plugin that enables Python code execution within QNTX.
//! Uses PyO3 to embed a Python interpreter in Rust for safe, isolated execution.

pub mod proto;
pub mod python;
pub mod service;

pub use python::PythonEngine;
pub use service::PythonPluginService;
// Trigger CI
