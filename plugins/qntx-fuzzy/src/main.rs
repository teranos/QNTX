//! QNTX Fuzzy Matching Plugin
//!
//! High-performance fuzzy matching service for QNTX attestation queries.
//! Provides multi-strategy matching (exact, prefix, substring, edit distance)
//! with configurable scoring thresholds.
//!
//! ## Usage
//!
//! ```bash
//! qntx-fuzzy --port 9100
//! ```
//!
//! ## Configuration
//!
//! Environment variables:
//! - `QNTX_FUZZY_PORT`: gRPC server port (default: 9100)
//! - `QNTX_FUZZY_MIN_SCORE`: Minimum match score 0.0-1.0 (default: 0.6)
//! - `RUST_LOG`: Logging level (default: info)

use std::net::SocketAddr;
use std::sync::Arc;

use tonic::transport::Server;
use tracing::{info, Level};
use tracing_subscriber::FmtSubscriber;

mod engine;
mod service;

// Include generated protobuf code
pub mod proto {
    tonic::include_proto!("qntx.fuzzy");
}

use engine::{EngineConfig, FuzzyEngine};
use proto::fuzzy_match_service_server::FuzzyMatchServiceServer;
use service::FuzzyService;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Initialize logging
    let subscriber = FmtSubscriber::builder()
        .with_max_level(Level::INFO)
        .with_env_filter(tracing_subscriber::EnvFilter::from_default_env())
        .init();

    // Parse configuration from environment
    let port: u16 = std::env::var("QNTX_FUZZY_PORT")
        .ok()
        .and_then(|s| s.parse().ok())
        .unwrap_or(9100);

    let min_score: f64 = std::env::var("QNTX_FUZZY_MIN_SCORE")
        .ok()
        .and_then(|s| s.parse().ok())
        .unwrap_or(0.6);

    let config = EngineConfig {
        min_score,
        ..Default::default()
    };

    // Create engine and service
    let engine = Arc::new(FuzzyEngine::with_config(config));
    let service = FuzzyService::new(engine);

    let addr: SocketAddr = format!("0.0.0.0:{}", port).parse()?;

    info!(
        port = port,
        min_score = min_score,
        "Starting QNTX Fuzzy Matching Service"
    );

    // Start gRPC server
    Server::builder()
        .add_service(FuzzyMatchServiceServer::new(service))
        .serve(addr)
        .await?;

    Ok(())
}
