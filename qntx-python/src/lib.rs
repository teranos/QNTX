//! QNTX Python Plugin
//!
//! A gRPC plugin that enables Python code execution within QNTX.
//! Uses PyO3 to embed a Python interpreter in Rust for safe, isolated execution.
//!
//! ## Module Structure
//!
//! - `engine` - Core PythonEngine struct, types, and initialization
//! - `execution` - Code execution, file execution, evaluation
//! - `config` - Plugin configuration types
//! - `handlers` - HTTP endpoint handlers
//! - `service` - gRPC service implementation
//! - `proto` - Generated protobuf types

pub mod config;
pub mod engine;
pub mod execution;
mod handlers;
pub mod proto;
pub mod service;

pub use config::PluginConfig;
pub use engine::{ExecutionConfig, ExecutionResult, PythonEngine};
pub use service::PythonPluginService;
