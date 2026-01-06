//! QNTX Python Plugin - Main Entry Point
//!
//! A gRPC plugin that allows running Python code within QNTX.
//!
//! Usage:
//!     qntx-python-plugin --port 9000
//!     qntx-python-plugin --address localhost:9000

use clap::Parser;
use qntx_python_plugin::proto::domain_plugin_service_server::DomainPluginServiceServer;
use qntx_python_plugin::PythonPluginService;
use std::net::SocketAddr;
use tokio::signal;
use tonic::transport::Server;
use tracing::{info, Level};
use tracing_subscriber::FmtSubscriber;

#[derive(Parser, Debug)]
#[command(name = "qntx-python-plugin")]
#[command(about = "QNTX Python execution plugin")]
#[command(version)]
struct Args {
    /// gRPC server port
    #[arg(short, long, default_value = "9000")]
    port: u16,

    /// gRPC server address (overrides port)
    #[arg(short, long)]
    address: Option<String>,

    /// Log level (debug, info, warn, error)
    #[arg(long, default_value = "info")]
    log_level: String,

    /// Print version and exit
    #[arg(short = 'V', long)]
    version: bool,
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Set up panic hook to log panics before they terminate the process
    std::panic::set_hook(Box::new(|panic_info| {
        eprintln!("PANIC: Plugin panicked during startup or execution");
        eprintln!("  Location: {}", panic_info.location().map(|l| l.to_string()).unwrap_or_else(|| "unknown".to_string()));
        eprintln!("  Message: {}", panic_info.payload().downcast_ref::<&str>().unwrap_or(&"<no message>"));
    }));

    let args = Args::parse();

    if args.version {
        println!("qntx-python-plugin {}", env!("CARGO_PKG_VERSION"));
        return Ok(());
    }

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
        .with_thread_ids(false)
        .with_file(false)
        .with_line_number(false)
        .init();

    info!("Initializing QNTX Python Plugin");
    info!("  Version: {}", env!("CARGO_PKG_VERSION"));

    // Determine server address
    let addr: SocketAddr = if let Some(address) = args.address {
        info!("Parsing address: {}", address);
        match address.parse() {
            Ok(a) => a,
            Err(e) => {
                eprintln!("ERROR: Failed to parse address '{}': {}", address, e);
                return Err(format!("Invalid address: {}", e).into());
            }
        }
    } else {
        let addr_str = format!("0.0.0.0:{}", args.port);
        info!("Using default address: {}", addr_str);
        match addr_str.parse() {
            Ok(a) => a,
            Err(e) => {
                eprintln!("ERROR: Failed to parse address '{}': {}", addr_str, e);
                return Err(format!("Invalid address: {}", e).into());
            }
        }
    };

    info!("Address parsed successfully: {}", addr);

    // Create the Python plugin service
    info!("Creating Python plugin service...");
    let service = match PythonPluginService::new() {
        Ok(s) => {
            info!("Python plugin service created successfully");
            s
        }
        Err(e) => {
            eprintln!("ERROR: Failed to create Python plugin service: {}", e);
            return Err(format!("Service creation failed: {}", e).into());
        }
    };

    info!("Starting QNTX Python Plugin");
    info!("  Version: {}", env!("CARGO_PKG_VERSION"));
    info!("  Address: {}", addr);

    // Build and start the gRPC server
    info!("Building gRPC server...");
    let server = Server::builder()
        .add_service(DomainPluginServiceServer::new(service));

    info!("Starting gRPC server on {}...", addr);
    match server.serve_with_shutdown(addr, shutdown_signal()).await {
        Ok(_) => {
            info!("gRPC server stopped gracefully");
        }
        Err(e) => {
            eprintln!("ERROR: gRPC server failed: {}", e);
            return Err(format!("Server failed: {}", e).into());
        }
    }

    info!("Plugin shutdown complete");
    Ok(())
}

async fn shutdown_signal() {
    let ctrl_c = async {
        signal::ctrl_c()
            .await
            .expect("Failed to install Ctrl+C handler");
    };

    #[cfg(unix)]
    let terminate = async {
        signal::unix::signal(signal::unix::SignalKind::terminate())
            .expect("Failed to install signal handler")
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
