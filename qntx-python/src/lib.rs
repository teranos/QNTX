//! QNTX Python Plugin
//!
//! A gRPC plugin that enables Python code execution within QNTX.
//! Uses PyO3 to embed a Python interpreter in Rust for safe, isolated execution.
//!
//! ## Module Structure
//!
//! - `engine` - Core PythonEngine struct and initialization
//! - `execution` - Code execution, file execution, evaluation
//! - `service` - gRPC service implementation
//! - `proto` - Generated protobuf types

pub mod engine;
pub mod execution;
pub mod proto;
pub mod service;

pub use engine::{ExecutionConfig, ExecutionResult, PythonEngine, PythonError};
pub use service::PythonPluginService;
