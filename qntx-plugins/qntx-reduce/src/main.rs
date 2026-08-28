use clap::Parser;
use qntx_reduce_plugin::proto::domain_plugin_service_server::DomainPluginServiceServer;
use qntx_reduce_plugin::ReducePluginService;
use std::net::SocketAddr;
use tokio::net::TcpListener;
use tokio::signal;
use tokio_stream::wrappers::TcpListenerStream;
use tonic::transport::Server;
use tracing::{error, info, warn, Level};
use tracing_subscriber::FmtSubscriber;

#[derive(Parser, Debug)]
#[command(name = "qntx-reduce-plugin")]
#[command(about = "QNTX dimensionality reduction plugin (UMAP)")]
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

/// Max port retries when the requested port is occupied (multi-session conflicts).
const MAX_PORT_RETRIES: u16 = 10;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Built by hand rather than #[tokio::main]: the macro's expansion holds
    // an expect() on runtime construction, a panic the lint wall forbids for
    // good reason — a runtime that cannot build is an error to report.
    tokio::runtime::Builder::new_multi_thread()
        .enable_all()
        .build()
        .map_err(|e| format!("failed to build tokio runtime: {e}"))?
        .block_on(run())
}

async fn run() -> Result<(), Box<dyn std::error::Error>> {
    std::panic::set_hook(Box::new(|panic_info| {
        eprintln!("PANIC: Plugin panicked during startup or execution");
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

    info!("Initializing QNTX Reduce Plugin");
    info!("  Version: {}", env!("CARGO_PKG_VERSION"));

    // Bind with port retry to handle multi-session port conflicts.
    // When multiple QNTX sessions run concurrently, they each allocate ports
    // starting from DefaultPluginBasePort (38700). If another session's plugin
    // already occupies our assigned port, we increment and retry.
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
                last_err.map_or_else(|| "no bind error recorded".to_string(), |e| e.to_string())
            )
        })?
    };

    let local_addr = listener.local_addr()?;

    // Announce actual port to the plugin manager via stdout protocol.
    // The manager watches for QNTX_PLUGIN_PORT=N and uses it instead of
    // the port it passed via --port. Must be println (stdout), not info (stderr).
    println!("QNTX_PLUGIN_PORT={}", local_addr.port());

    let service = ReducePluginService::new();

    info!("Starting gRPC server on {}", local_addr);

    let incoming = TcpListenerStream::new(listener);
    Server::builder()
        .add_service(
            DomainPluginServiceServer::new(service)
                .max_decoding_message_size(100 * 1024 * 1024)
                .max_encoding_message_size(100 * 1024 * 1024),
        )
        .serve_with_incoming_shutdown(incoming, shutdown_signal())
        .await?;

    info!("Plugin shutdown complete");
    Ok(())
}

async fn shutdown_signal() {
    // A handler that cannot be installed must not resolve this future: an
    // early resolve here IS a shutdown order, turning a setup failure into a
    // silent immediate stop. Say what is lost and wait on the other signal.
    let ctrl_c = async {
        if let Err(e) = signal::ctrl_c().await {
            error!(
                "Cannot listen for Ctrl+C; this plugin stops only by signal or kill: {}",
                e
            );
            std::future::pending::<()>().await;
        }
    };

    #[cfg(unix)]
    let terminate = async {
        match signal::unix::signal(signal::unix::SignalKind::terminate()) {
            Ok(mut sig) => {
                sig.recv().await;
            }
            Err(e) => {
                error!(
                    "Cannot listen for SIGTERM; this plugin stops only by Ctrl+C or kill: {}",
                    e
                );
                std::future::pending::<()>().await;
            }
        }
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
