//! QNTX VidStream Plugin - Main Entry Point
//!
//! A pure Rust gRPC plugin for real-time video inference via ONNX Runtime.
//! Wraps the qntx-vidstream engine directly — no CGO bridge needed.
//!
//! Usage:
//!     qntx-vidstream-plugin --port 9200
//!     qntx-vidstream-plugin --address localhost:9200

use clap::Parser;
use qntx_vidstream_plugin::proto::domain_plugin_service_server::DomainPluginServiceServer;
use qntx_vidstream_plugin::VidStreamPluginService;
use std::net::SocketAddr;
use tokio::net::TcpListener;
use tokio::signal;
use tokio_stream::wrappers::TcpListenerStream;
use tonic::transport::Server;
use tracing::{debug, info, warn, Level};
use tracing_subscriber::FmtSubscriber;

#[derive(Parser, Debug)]
#[command(name = "qntx-vidstream-plugin")]
#[command(about = "QNTX VidStream — real-time video inference via ONNX")]
#[command(version)]
struct Args {
    /// gRPC server port
    #[arg(short, long, default_value = "9200")]
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

/// Max port retries when the requested port is occupied (multi-session conflicts).
const MAX_PORT_RETRIES: u16 = 10;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Set up panic hook to log panics before they terminate the process
    std::panic::set_hook(Box::new(|panic_info| {
        eprintln!("PANIC: VidStream plugin panicked");
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

    if args.version {
        println!("qntx-vidstream-plugin {}", env!("CARGO_PKG_VERSION"));
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

    debug!(
        "Initializing QNTX VidStream Plugin v{}",
        env!("CARGO_PKG_VERSION")
    );

    // Bind with port retry to handle multi-session port conflicts.
    let listener = if let Some(address) = args.address {
        let addr: SocketAddr = address
            .parse()
            .map_err(|e| format!("Invalid address '{}': {}", address, e))?;
        TcpListener::bind(addr).await?
    } else {
        let mut port = args.port;
        let mut last_err = None;
        let mut bound = None;
        for _ in 0..MAX_PORT_RETRIES {
            let addr: SocketAddr = format!("0.0.0.0:{}", port).parse()?;
            match TcpListener::bind(addr).await {
                Ok(l) => {
                    bound = Some(l);
                    break;
                }
                Err(e) if e.kind() == std::io::ErrorKind::AddrInUse => {
                    warn!("Port {} in use, trying {}", port, port + 1);
                    last_err = Some(e);
                    port += 1;
                }
                Err(e) => return Err(e.into()),
            }
        }
        bound.ok_or_else(|| {
            format!(
                "failed to bind after {} attempts (last port {}): {}",
                MAX_PORT_RETRIES,
                port,
                last_err.unwrap()
            )
        })?
    };

    let local_addr = listener.local_addr()?;

    // Announce actual port to the plugin manager via stdout protocol.
    println!("QNTX_PLUGIN_PORT={}", local_addr.port());

    // Create the VidStream plugin service
    let service = VidStreamPluginService::new();

    info!("gRPC server listening on {}", local_addr);

    let incoming = TcpListenerStream::new(listener);
    Server::builder()
        .add_service(DomainPluginServiceServer::new(service))
        .serve_with_incoming_shutdown(incoming, shutdown_signal())
        .await?;

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
