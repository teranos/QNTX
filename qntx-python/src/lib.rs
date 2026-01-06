//! QNTX Python Plugin
//!
//! A gRPC plugin that allows running Python code within QNTX.
//! Uses PyO3 to embed a Python interpreter in Rust.

pub mod proto;
pub mod python;
pub mod service;

pub use python::PythonEngine;
pub use service::PythonPluginService;
