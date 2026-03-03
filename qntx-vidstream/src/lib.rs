//! QNTX VidStream Plugin
//!
//! A pure Rust gRPC plugin for real-time video inference via ONNX Runtime.
//! Uses the qntx-vidstream engine directly — no FFI/CGO bridge.
//!
//! ## Module Structure
//!
//! - `handlers` - HTTP endpoint handlers (/init, /frame, /status)
//! - `service` - gRPC DomainPluginService implementation
//! - `proto` - Generated protobuf types

pub mod handlers;
pub mod proto;
pub mod service;

pub use service::VidStreamPluginService;
