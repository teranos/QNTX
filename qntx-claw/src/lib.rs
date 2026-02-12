//! QNTX OpenClaw Plugin
//!
//! Observability into your OpenClaw setup. Watches workspace files
//! (AGENTS.md, SOUL.md, MEMORY.md, daily memory logs) and ingests
//! changes as QNTX attestations.
//!
//! ## Module Structure
//!
//! - `workspace` - OpenClaw workspace discovery, snapshot, and file watching
//! - `config` - Plugin configuration types
//! - `handlers` - HTTP endpoint handlers
//! - `service` - gRPC service implementation
//! - `proto` - Generated protobuf types

pub mod config;
mod handlers;
pub mod proto;
pub mod service;
pub mod workspace;

pub use service::ClawPluginService;
