//! QNTX Inference Plugin - Main Entry Point
//!
//! A gRPC plugin for local embedding generation using ONNX models.
//!
//! Usage:
//!     qntx-inference --port 9001
//!     qntx-inference --address localhost:9001

mod engine;
mod service;

use clap::Parser;
use qntx::plugin::proto::domain_plugin_service_server::DomainPluginServiceServer;
use qntx::plugin::{shutdown_signal, PluginServer};
use service::InferencePluginService;
use tracing::{info, Level};
use tracing_subscriber::FmtSubscriber;

#[derive(Parser, Debug)]
#[command(name = "qntx-inference")]
#[command(about = "QNTX local inference plugin for embeddings")]
#[command(version)]
struct Args {
    /// gRPC server port
    #[arg(short, long, default_value = "9001")]
    port: u16,

    /// gRPC server address (overrides port)
    #[arg(short, long)]
    address: Option<String>,

    /// Log level (debug, info, warn, error)
    #[arg(long, default_value = "info")]
    log_level: String,
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let args = Args::parse();

    // Set up logging
    let log_level = match args.log_level.as_str() {
        "debug" => Level::DEBUG,
        "warn" => Level::WARN,
        "error" => Level::ERROR,
        _ => Level::INFO,
    };

    FmtSubscriber::builder()
        .with_max_level(log_level)
        .with_target(false)
        .init();

    info!("Starting QNTX Inference Plugin v{}", env!("CARGO_PKG_VERSION"));

    // Create the inference engine
    let engine = engine::create_engine();
    let service = InferencePluginService::new(engine);

    // Determine server address
    let addr = if let Some(address) = args.address {
        address.parse()?
    } else {
        format!("0.0.0.0:{}", args.port).parse()?
    };

    // Start the gRPC server
    PluginServer::new("qntx-inference", env!("CARGO_PKG_VERSION"))
        .address(addr)
        .serve(DomainPluginServiceServer::new(service))
        .await?;

    Ok(())
}
