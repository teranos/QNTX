//! QNTX Image Generation Plugin
//!
//! A gRPC plugin that runs Stable Diffusion 1.5 inference via ONNX Runtime.
//! Local-first image generation — no cloud dependency.

pub mod atsstore;
pub mod config;
mod handlers;
pub mod models;
pub mod pipeline;
pub mod proto;
pub mod service;

pub use config::PluginConfig;
pub use service::ImagegenPluginService;
