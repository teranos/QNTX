//! QNTX PTY Glyph Plugin - Main Entry Point
//!
//! A gRPC plugin that provides persistent terminal glyphs with full PTY support.
//!
//! Usage:
//!     qntx-pty-glyph --port 38701
//!     qntx-pty-glyph --address localhost:38701

use clap::Parser;
use qntx_pty_glyph::proto::domain_plugin_service_server::DomainPluginServiceServer;
use qntx_pty_glyph::PTYGlyphService;
use std::net::SocketAddr;
use tokio::net::TcpListener;
use tokio::signal;
use tokio_stream::wrappers::TcpListenerStream;
use tonic::transport::Server;
use tracing::{info, warn, Level};
use tracing_subscriber::FmtSubscriber;

#[derive(Parser, Debug)]
#[command(name = "qntx-pty-glyph")]
#[command(about = "QNTX persistent terminal glyph plugin")]
#[command(version)]
struct Args {
    /// gRPC server port
    #[arg(short, long, default_value = "38701")]
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
        eprintln!("PANIC: PTY glyph plugin panicked during startup or execution");
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
        println!("qntx-pty-glyph {}", env!("CARGO_PKG_VERSION"));
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

    info!("Initializing QNTX PTY Glyph Plugin");
    info!("  Version: {}", env!("CARGO_PKG_VERSION"));

    // Bind with port retry to handle multi-session port conflicts
    let listener = if let Some(address) = args.address {
        let addr: SocketAddr = address
            .parse()
            .map_err(|e| format!("Invalid address '{}': {}", address, e))?;
        TcpListener::bind(addr).await?
    } else {
        bind_with_retry(args.port).await?
    };

    let addr = listener.local_addr()?;
    info!("gRPC server listening on {}", addr);

    // Print port for discovery
    println!("PORT:{}", addr.port());

    // Create plugin service
    let service = PTYGlyphService::new();

    // Start gRPC server
    Server::builder()
        .add_service(DomainPluginServiceServer::new(service))
        .serve_with_incoming_shutdown(TcpListenerStream::new(listener), shutdown_signal())
        .await?;

    info!("PTY glyph plugin shutdown complete");
    Ok(())
}

/// Bind to port with retry logic for multi-session conflicts
async fn bind_with_retry(mut port: u16) -> Result<TcpListener, Box<dyn std::error::Error>> {
    for attempt in 0..MAX_PORT_RETRIES {
        let addr = SocketAddr::from(([127, 0, 0, 1], port));
        match TcpListener::bind(addr).await {
            Ok(listener) => {
                if attempt > 0 {
                    warn!("Bound to port {} after {} retries", port, attempt);
                }
                return Ok(listener);
            }
            Err(e) if e.kind() == std::io::ErrorKind::AddrInUse => {
                warn!(
                    "Port {} in use, trying {} (retry {}/{})",
                    port,
                    port + 1,
                    attempt + 1,
                    MAX_PORT_RETRIES
                );
                port += 1;
            }
            Err(e) => return Err(e.into()),
        }
    }
    Err(format!("Failed to bind after {} retries", MAX_PORT_RETRIES).into())
}

/// Wait for shutdown signal (Ctrl+C or SIGTERM)
async fn shutdown_signal() {
    let ctrl_c = async {
        signal::ctrl_c()
            .await
            .expect("failed to install Ctrl+C handler");
    };

    #[cfg(unix)]
    let terminate = async {
        signal::unix::signal(signal::unix::SignalKind::terminate())
            .expect("failed to install SIGTERM handler")
            .recv()
            .await;
    };

    #[cfg(not(unix))]
    let terminate = std::future::pending::<()>();

    tokio::select! {
        _ = ctrl_c => {
            info!("Received Ctrl+C signal");
        }
        _ = terminate => {
            info!("Received SIGTERM signal");
        }
    }
}
