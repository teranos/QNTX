//! QNTX OpenClaw Plugin - Main Entry Point
//!
//! A gRPC plugin that watches OpenClaw workspace files and provides
//! observability into system prompt, memory, and identity changes.
//!
//! Usage:
//!     qntx-claw-plugin --port 9001
//!     qntx-claw-plugin --address localhost:9001

use clap::Parser;
use qntx_claw::proto::domain_plugin_service_server::DomainPluginServiceServer;
use qntx_claw::ClawPluginService;
use std::net::SocketAddr;
use tokio::signal;
use tonic::transport::Server;
use tracing::{info, Level};
use tracing_subscriber::FmtSubscriber;

#[derive(Parser, Debug)]
#[command(name = "qntx-claw-plugin")]
#[command(about = "QNTX OpenClaw observability plugin")]
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
    std::panic::set_hook(Box::new(|panic_info| {
        eprintln!("PANIC: Plugin panicked");
        eprintln!(
            "  Location: {}",
            panic_info
                .location()
                .map(|l| l.to_string())
                .unwrap_or_else(|| "unknown".to_string())
        );
        eprintln!(
            "  Message: {}",
            panic_info
                .payload()
                .downcast_ref::<&str>()
                .unwrap_or(&"<no message>")
        );
    }));

    let args = Args::parse();

    let log_level = match args.log_level.as_str() {
        "debug" => Level::DEBUG,
        "warn" => Level::WARN,
        "error" => Level::ERROR,
        _ => Level::INFO,
    };

    FmtSubscriber::builder()
        .with_max_level(log_level)
        .with_target(false)
        .with_thread_ids(false)
        .with_file(false)
        .with_line_number(false)
        .init();

    info!("Initializing QNTX OpenClaw Plugin");
    info!("  Version: {}", env!("CARGO_PKG_VERSION"));

    let addr: SocketAddr = if let Some(address) = args.address {
        address.parse().map_err(|e| {
            format!("failed to parse address '{}': {}", address, e)
        })?
    } else {
        format!("0.0.0.0:{}", args.port).parse().map_err(|e| {
            format!("failed to parse port {}: {}", args.port, e)
        })?
    };

    let service = ClawPluginService::new()?;

    info!("Starting QNTX OpenClaw Plugin on {}", addr);

    Server::builder()
        .add_service(DomainPluginServiceServer::new(service))
        .serve_with_shutdown(addr, shutdown_signal())
        .await?;

    info!("Plugin shutdown complete");
    Ok(())
}

async fn shutdown_signal() {
    let ctrl_c = async {
        signal::ctrl_c()
            .await
            .expect("failed to install Ctrl+C handler");
    };

    #[cfg(unix)]
    let terminate = async {
        signal::unix::signal(signal::unix::SignalKind::terminate())
            .expect("failed to install signal handler")
            .recv()
            .await;
    };

    #[cfg(not(unix))]
    let terminate = std::future::pending::<()>();

    tokio::select! {
        _ = ctrl_c => {
            info!("Received Ctrl+C, shutting down");
        }
        _ = terminate => {
            info!("Received terminate signal, shutting down");
        }
    }
}
