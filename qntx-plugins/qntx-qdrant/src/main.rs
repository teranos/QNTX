//! Entry point for the qntx-qdrant plugin.
//!
//! Boots the managed Qdrant instance first, then starts the gRPC server that
//! exposes three services on one port:
//!
//!   * DomainPluginService  — plugin-host contract
//!   * SearchService        — ADR-015
//!   * VectorSearchService  — ADR-016
//!
//! If Qdrant fails to come up, the plugin exits non-zero rather than serving
//! degraded RPCs. "Entire thing plugin-managed" (ADR-017) only has teeth if
//! the plugin refuses to run when it can't manage the engine.

use clap::Parser;
use qntx_qdrant_plugin::proto::domain_plugin_service_server::DomainPluginServiceServer;
use qntx_qdrant_plugin::proto::search_service_server::SearchServiceServer;
use qntx_qdrant_plugin::proto::vector_search_service_server::VectorSearchServiceServer;
use qntx_qdrant_plugin::qdrant::{Mode, Supervisor};
use qntx_qdrant_plugin::search::SearchServiceImpl;
use qntx_qdrant_plugin::vector::VectorSearchServiceImpl;
use qntx_qdrant_plugin::QdrantPluginService;
use std::net::SocketAddr;
use tokio::net::TcpListener;
use tokio::signal;
use tokio_stream::wrappers::TcpListenerStream;
use tonic::transport::Server;
use tracing::{error, info, warn, Level};
use tracing_subscriber::FmtSubscriber;

#[derive(Parser, Debug)]
#[command(name = "qntx-qdrant-plugin")]
#[command(about = "QNTX Qdrant plugin (ADR-017): SearchService + VectorSearchService")]
#[command(version)]
struct Args {
    /// gRPC server port for the plugin's own listener (not Qdrant's).
    #[arg(short, long, default_value = "9002")]
    port: u16,

    /// Full address override for the plugin's listener.
    #[arg(short, long)]
    address: Option<String>,

    /// Log level (debug, info, warn, error).
    #[arg(long, default_value = "info")]
    log_level: String,
}

/// Matches the pattern used by qntx-reduce for multi-session port conflicts.
const MAX_PORT_RETRIES: u16 = 10;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    std::panic::set_hook(Box::new(|panic_info| {
        eprintln!("PANIC: qntx-qdrant plugin panicked during startup or execution");
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
        .init();

    info!("qntx-qdrant plugin {}", env!("CARGO_PKG_VERSION"));

    // 1) Bring up the plugin-managed Qdrant instance before binding the
    //    gRPC port. A plugin that can't manage its engine has nothing useful
    //    to say, so we fail fast instead of serving Unavailable forever.
    let supervisor = Supervisor::prepare(Mode::ChildProcess).map_err(box_err)?;
    if let Err(e) = supervisor.start().await {
        error!("managed qdrant failed to start: {}", e);
        return Err(box_err(e));
    }
    info!(
        addr = %supervisor.endpoint().addr,
        data_dir = %supervisor.endpoint().data_dir.display(),
        "managed qdrant ready",
    );

    // 2) Bind the plugin's own gRPC port (the one the plugin host talks to).
    let listener = bind_plugin_listener(&args).await?;
    let local_addr = listener.local_addr()?;
    println!("QNTX_PLUGIN_PORT={}", local_addr.port());

    // 3) Build services and serve. All three share the same supervisor,
    //    so all gRPC calls route to the single managed Qdrant.
    let plugin_svc = QdrantPluginService::new(supervisor.clone());
    let search_svc = SearchServiceImpl::new(supervisor.clone());
    let vector_svc = VectorSearchServiceImpl::new(supervisor.clone());

    info!("starting gRPC server on {}", local_addr);
    let incoming = TcpListenerStream::new(listener);

    let shutdown_supervisor = supervisor.clone();
    let serve_result = Server::builder()
        .add_service(
            DomainPluginServiceServer::new(plugin_svc)
                .max_decoding_message_size(100 * 1024 * 1024)
                .max_encoding_message_size(100 * 1024 * 1024),
        )
        .add_service(SearchServiceServer::new(search_svc))
        .add_service(VectorSearchServiceServer::new(vector_svc))
        .serve_with_incoming_shutdown(incoming, shutdown_signal())
        .await;

    // Always tear Qdrant down, even if the gRPC server errored.
    shutdown_supervisor.shutdown().await;

    serve_result?;
    info!("qntx-qdrant shutdown complete");
    Ok(())
}

async fn bind_plugin_listener(args: &Args) -> Result<TcpListener, Box<dyn std::error::Error>> {
    if let Some(address) = &args.address {
        let addr: SocketAddr = address
            .parse()
            .map_err(|e| format!("invalid address '{}': {}", address, e))?;
        return Ok(TcpListener::bind(addr).await?);
    }

    let mut port = args.port;
    let mut last_err = None;
    for _ in 0..MAX_PORT_RETRIES {
        let addr: SocketAddr = format!("0.0.0.0:{}", port).parse()?;
        match TcpListener::bind(addr).await {
            Ok(l) => return Ok(l),
            Err(e) if e.kind() == std::io::ErrorKind::AddrInUse => {
                warn!("port {} in use, trying {}", port, port + 1);
                last_err = Some(e);
                port += 1;
            }
            Err(e) => return Err(e.into()),
        }
    }
    Err(format!(
        "failed to bind after {} attempts (last port {}): {}",
        MAX_PORT_RETRIES,
        port,
        last_err.map(|e| e.to_string()).unwrap_or_default()
    )
    .into())
}

fn box_err<E: std::error::Error + Send + Sync + 'static>(e: E) -> Box<dyn std::error::Error> {
    Box::new(e)
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
        _ = ctrl_c => info!("received Ctrl+C, shutting down"),
        _ = terminate => info!("received SIGTERM, shutting down"),
    }
}
