//! Plugin-managed Qdrant lifecycle.
//!
//! Per ADR-017, the plugin owns Qdrant end-to-end: the binary it executes,
//! the data directory it writes to, the loopback port it listens on, and the
//! readiness/shutdown protocol around it. A QNTX deployment never sees Qdrant.
//!
//! Two supervision modes share one `Supervisor` facade:
//!
//!   * `Mode::ChildProcess` — spawn the bundled qdrant binary as a child of
//!     the plugin process. This is the realistic default today: Qdrant's
//!     storage engine is not published as a reusable library.
//!
//!   * `Mode::Embedded` — link Qdrant's segment/collection crates directly.
//!     Left as a TODO until upstream exposes a stable embedding surface.
//!
//! The two modes are interchangeable from the plugin's perspective because the
//! plugin only ever talks to its Qdrant via the `QdrantClient` gRPC handle
//! this module hands out.

use parking_lot::Mutex;
use std::net::{SocketAddr, TcpListener};
use std::path::PathBuf;
use std::process::Stdio;
use std::sync::Arc;
use std::time::Duration;
use thiserror::Error;
use tokio::process::{Child, Command};
use tokio::time::sleep;
use tracing::{debug, info, warn};

/// Env var set by the nix flake to locate the bundled qdrant binary.
/// The plugin NEVER falls back to `PATH` — "plugin-managed" means the
/// binary must be one we shipped with the plugin.
const QDRANT_BIN_ENV: &str = "QNTX_QDRANT_BINARY";

/// Env var pointing at the plugin's owned state directory. Provided by the
/// plugin host at Initialize time; falls back to a per-user default.
const QDRANT_STATE_ENV: &str = "QNTX_QDRANT_STATE";

#[derive(Debug, Error)]
pub enum SupervisorError {
    #[error("qdrant binary not available: set {QDRANT_BIN_ENV} (the plugin's nix flake normally sets this)")]
    BinaryMissing,

    #[error("failed to allocate a loopback port for the managed qdrant: {0}")]
    PortAllocation(#[from] std::io::Error),

    #[error("qdrant failed to become ready within {timeout:?}")]
    ReadinessTimeout { timeout: Duration },

    #[error("qdrant exited during startup (exit status: {status:?})")]
    StartupCrash { status: Option<i32> },

    #[error("qdrant client error: {0}")]
    Client(String),
}

/// Supervision mode for the managed Qdrant.
#[derive(Clone, Copy, Debug)]
pub enum Mode {
    /// Spawn the bundled binary as a child process (default).
    ChildProcess,
    /// Link Qdrant as a library in-process. Not wired yet — see ADR-017.
    Embedded,
}

/// Where the managed Qdrant's data lives and how to reach it.
#[derive(Clone, Debug)]
pub struct Endpoint {
    pub addr: SocketAddr,
    pub data_dir: PathBuf,
}

/// Owns the Qdrant lifecycle for the plugin's entire run.
///
/// Constructed once at plugin startup. Clones share the same managed instance.
#[derive(Clone)]
pub struct Supervisor {
    inner: Arc<Mutex<State>>,
    endpoint: Endpoint,
    mode: Mode,
}

struct State {
    child: Option<Child>,
}

impl Supervisor {
    /// Lay out state directory and port, but do not start the engine yet.
    pub fn prepare(mode: Mode) -> Result<Self, SupervisorError> {
        let data_dir = resolve_state_dir();
        std::fs::create_dir_all(&data_dir).map_err(SupervisorError::PortAllocation)?;

        let addr = allocate_loopback_port()?;

        Ok(Self {
            inner: Arc::new(Mutex::new(State { child: None })),
            endpoint: Endpoint { addr, data_dir },
            mode,
        })
    }

    pub fn endpoint(&self) -> &Endpoint {
        &self.endpoint
    }

    /// Bring Qdrant up. Blocks until readiness probe succeeds.
    pub async fn start(&self) -> Result<(), SupervisorError> {
        match self.mode {
            Mode::ChildProcess => self.start_child_process().await,
            Mode::Embedded => {
                // ADR-017: embedded mode is deferred until Qdrant exposes a stable
                // library surface. Contract is identical from callers' view.
                Err(SupervisorError::Client(
                    "embedded mode not implemented yet — use Mode::ChildProcess".into(),
                ))
            }
        }
    }

    async fn start_child_process(&self) -> Result<(), SupervisorError> {
        let binary = std::env::var(QDRANT_BIN_ENV)
            .map(PathBuf::from)
            .map_err(|_| SupervisorError::BinaryMissing)?;

        if !binary.exists() {
            return Err(SupervisorError::BinaryMissing);
        }

        info!(
            binary = %binary.display(),
            data_dir = %self.endpoint.data_dir.display(),
            addr = %self.endpoint.addr,
            "spawning plugin-managed qdrant",
        );

        // Qdrant takes storage + service config via env vars. Using env keeps
        // the plugin from having to generate a config.yaml on disk.
        let child = Command::new(&binary)
            .env("QDRANT__STORAGE__STORAGE_PATH", &self.endpoint.data_dir)
            .env(
                "QDRANT__SERVICE__HOST",
                self.endpoint.addr.ip().to_string(),
            )
            .env(
                "QDRANT__SERVICE__GRPC_PORT",
                self.endpoint.addr.port().to_string(),
            )
            // Disable the REST port entirely — the plugin only speaks gRPC.
            .env("QDRANT__SERVICE__HTTP_PORT", "0")
            // Telemetry off by default: this instance is invisible to the user.
            .env("QDRANT__TELEMETRY_DISABLED", "true")
            .stdout(Stdio::null())
            .stderr(Stdio::piped())
            .kill_on_drop(true)
            .spawn()
            .map_err(SupervisorError::PortAllocation)?;

        self.inner.lock().child = Some(child);

        wait_for_ready(self.endpoint.addr, Duration::from_secs(30)).await?;
        info!("managed qdrant is ready");
        Ok(())
    }

    /// Connect a gRPC client to the managed instance.
    pub fn client(&self) -> Result<qdrant_client::Qdrant, SupervisorError> {
        // TODO: reuse a single connection across requests once the service
        // layer is wired — qdrant-client already pools internally.
        let url = format!("http://{}", self.endpoint.addr);
        qdrant_client::Qdrant::from_url(&url)
            .build()
            .map_err(|e| SupervisorError::Client(e.to_string()))
    }

    /// Terminate the managed instance and wait for it to exit.
    pub async fn shutdown(&self) {
        let child = self.inner.lock().child.take();
        if let Some(mut child) = child {
            debug!("stopping managed qdrant");
            if let Err(e) = child.start_kill() {
                warn!("failed to signal qdrant: {}", e);
            }
            let _ = child.wait().await;
            info!("managed qdrant stopped");
        }
    }
}

fn resolve_state_dir() -> PathBuf {
    if let Ok(p) = std::env::var(QDRANT_STATE_ENV) {
        return PathBuf::from(p);
    }
    // Fallback keeps dev workflow usable without the host wiring the env var.
    // Production runs always get the state dir from the plugin host.
    let base = dirs_next_cache_dir().unwrap_or_else(|| PathBuf::from("."));
    base.join("qntx").join("qntx-qdrant").join("data")
}

fn dirs_next_cache_dir() -> Option<PathBuf> {
    std::env::var_os("XDG_CACHE_HOME")
        .map(PathBuf::from)
        .or_else(|| std::env::var_os("HOME").map(|h| PathBuf::from(h).join(".cache")))
}

/// Grab a free loopback port by binding ephemeral, then releasing. The small
/// race between release and qdrant's own bind is acceptable for a plugin-local
/// instance — if it collides, the plugin restarts and re-rolls the port.
fn allocate_loopback_port() -> Result<SocketAddr, std::io::Error> {
    let listener = TcpListener::bind("127.0.0.1:0")?;
    let addr = listener.local_addr()?;
    drop(listener);
    Ok(addr)
}

async fn wait_for_ready(addr: SocketAddr, timeout: Duration) -> Result<(), SupervisorError> {
    let deadline = tokio::time::Instant::now() + timeout;
    let url = format!("http://{}", addr);

    loop {
        // TODO: use qdrant_client's health_check once we've confirmed the
        // API. For now, a TCP connect probe keeps the scaffold honest
        // without claiming semantics we haven't verified.
        if tokio::net::TcpStream::connect(addr).await.is_ok() {
            // TCP open isn't proof Qdrant is serving gRPC yet, but it's a
            // better signal than sleeping blindly. Readiness loop will be
            // replaced with qdrant_client::Qdrant::health_check() once wired.
            debug!(%url, "qdrant tcp port open");
            return Ok(());
        }
        if tokio::time::Instant::now() >= deadline {
            return Err(SupervisorError::ReadinessTimeout { timeout });
        }
        sleep(Duration::from_millis(200)).await;
    }
}

