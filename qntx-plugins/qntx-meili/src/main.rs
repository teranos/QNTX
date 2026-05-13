use clap::Parser;
use qntx_grpc::plugin::proto::domain_plugin_service_server::DomainPluginServiceServer;
use qntx_grpc::plugin::proto::search_service_server::SearchServiceServer;
use qntx_grpc::plugin::shutdown_signal;
use std::net::SocketAddr;
use std::sync::Arc;
use tokio::net::TcpListener;
use tokio_stream::wrappers::TcpListenerStream;
use tonic::transport::Server;
use tracing::{info, warn, Level};
use tracing_subscriber::FmtSubscriber;

mod embedded;
mod search;
mod service;

use embedded::EmbeddedMeili;
use search::MeiliSearchService;
use service::MeiliPluginService;

#[derive(Parser, Debug)]
#[command(name = "qntx-meili")]
#[command(about = "QNTX search provider plugin (MeiliSearch)")]
#[command(version)]
struct Args {
    /// gRPC server port
    #[arg(short, long, default_value = "9010")]
    port: u16,

    /// gRPC server address (overrides port)
    #[arg(short, long)]
    address: Option<String>,

    /// Log level (debug, info, warn, error)
    #[arg(long, default_value = "info")]
    log_level: String,

    /// MeiliSearch host URL
    #[arg(long, default_value = "http://localhost:7700")]
    meili_url: String,

    /// MeiliSearch API key (optional)
    #[arg(long, default_value = "")]
    meili_key: String,

    /// Run an embedded MeiliSearch subprocess instead of connecting to an external one.
    /// Data is stored in ~/.qntx/meili-data/ (persistent across restarts).
    #[arg(long)]
    embedded: bool,

    /// Path to the meilisearch binary (for --embedded mode).
    /// Defaults to "meilisearch" (found via PATH).
    #[arg(long, default_value = "meilisearch")]
    meili_bin: String,

    /// Data directory for embedded MeiliSearch.
    /// Defaults to ~/.qntx/meili-data/
    #[arg(long)]
    meili_db_path: Option<String>,
}

const MAX_PORT_RETRIES: u16 = 10;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
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
        .with_writer(std::io::stderr)
        .init();

    info!("Initializing QNTX MeiliSearch Plugin");
    info!("  Version: {}", env!("CARGO_PKG_VERSION"));

    // Spawn embedded MeiliSearch if requested.
    // _embedded_handle is held here to keep the child process alive for the
    // duration of the plugin. Dropping it kills the subprocess.
    let (meili_url, meili_key, _embedded_handle);

    if args.embedded {
        let db_path = match &args.meili_db_path {
            Some(p) => std::path::PathBuf::from(p),
            None => {
                let home = std::env::var("HOME")
                    .map_err(|_| "HOME not set, cannot determine meili-data path")?;
                std::path::PathBuf::from(home).join(".qntx/meili-data")
            }
        };

        let handle = EmbeddedMeili::spawn(&args.meili_bin, db_path).await?;
        meili_url = handle.url();
        meili_key = handle.key().to_string();
        info!("  Mode: embedded ({})", meili_url);
        _embedded_handle = Some(handle);
    } else {
        meili_url = args.meili_url;
        meili_key = args.meili_key;
        info!("  Mode: remote ({})", meili_url);
        _embedded_handle = None;
    }

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
    println!("QNTX_PLUGIN_PORT={}", addr.port());
    use std::io::Write;
    std::io::stdout().flush().ok();

    let search_service = Arc::new(MeiliSearchService::new());
    if _embedded_handle.is_some() {
        search_service.set_mode("embedded");
    }
    let plugin_service = MeiliPluginService::new(Arc::clone(&search_service), meili_url, meili_key);

    Server::builder()
        .add_service(DomainPluginServiceServer::new(plugin_service))
        .add_service(SearchServiceServer::from_arc(search_service))
        .serve_with_incoming_shutdown(TcpListenerStream::new(listener), shutdown_signal())
        .await?;

    info!("qntx-meili shutdown complete");
    Ok(())
}

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
