//! Embedded MeiliSearch subprocess management.
//!
//! When `--embedded` is passed (or `embedded = true` in am.toml), the plugin
//! spawns a local MeiliSearch process on a random port with a temp data directory.
//! The subprocess is killed on plugin shutdown.
//!
//! This is for development only — no auth, single-node.
//! Data persists in `~/.qntx/meili-data/` across restarts so indexes survive plugin restarts.

use std::io::{BufRead, BufReader};
use std::net::TcpListener;
use std::path::PathBuf;
use std::process::{Child, Command, Stdio};
use std::sync::mpsc;
use std::time::Duration;
use tracing::{info, warn};

/// A managed MeiliSearch child process.
#[allow(dead_code)] // db_path is kept for diagnostics and future use
pub struct EmbeddedMeili {
    child: Child,
    port: u16,
    db_path: PathBuf,
}

impl EmbeddedMeili {
    /// Spawn a MeiliSearch subprocess on an available port.
    ///
    /// `binary` is the path to the `meilisearch` executable.
    /// `db_path` is where MeiliSearch stores its data (use a temp dir for ephemeral mode).
    ///
    /// The master key is set to "qntx-dev" — this is dev-only, not for production.
    pub async fn spawn(binary: &str, db_path: PathBuf) -> Result<Self, String> {
        let port = find_available_port().map_err(|e| format!("no available port: {}", e))?;

        // Ensure the data directory exists
        std::fs::create_dir_all(&db_path)
            .map_err(|e| format!("failed to create db path {}: {}", db_path.display(), e))?;

        info!(
            "Spawning embedded MeiliSearch on port {} (db: {})",
            port,
            db_path.display()
        );

        let mut child = Command::new(binary)
            .args([
                "--http-addr",
                &format!("127.0.0.1:{}", port),
                "--db-path",
                &db_path.to_string_lossy(),
                "--master-key",
                "qntx-dev",
                "--env",
                "development",
                "--no-analytics",
                // Cap indexing memory to 256MB — MeiliSearch defaults to 2/3 of RAM
                "--max-indexing-memory",
                "256MB",
            ])
            .stdout(Stdio::null())
            .stderr(Stdio::piped())
            .spawn()
            .map_err(|e| format!("failed to spawn meilisearch at '{}': {}", binary, e))?;

        // Drain stderr continuously in a background thread.
        // Dropping stderr causes SIGPIPE to the child on macOS, killing it (exit 101).
        let stderr = child.stderr.take();
        let (ready_tx, ready_rx) = mpsc::sync_channel::<bool>(1);
        let drain_port = port;

        std::thread::spawn(move || {
            let Some(stderr) = stderr else {
                let _ = ready_tx.send(false);
                return;
            };
            let reader = BufReader::new(stderr);
            let mut ready_tx = Some(ready_tx);
            for line in reader.lines() {
                match line {
                    Ok(text) => {
                        if let Some(tx) = ready_tx.take() {
                            if text.contains("Server listening on") {
                                let _ = tx.send(true);
                            } else {
                                ready_tx = Some(tx);
                            }
                        }
                    }
                    Err(_) => break,
                }
            }
            if let Some(tx) = ready_tx {
                let _ = tx.send(false);
            }
            info!("MeiliSearch stderr drainer exited (port {})", drain_port);
        });

        let ready = tokio::task::spawn_blocking(move || {
            ready_rx.recv_timeout(Duration::from_secs(25)).unwrap_or(false)
        })
        .await
        .map_err(|e| format!("ready-wait task failed: {}", e))?;

        if !ready {
            let _ = child.kill();
            return Err(format!(
                "MeiliSearch did not become ready within timeout on port {}",
                port
            ));
        }

        info!("Embedded MeiliSearch ready on port {}", port);

        Ok(Self {
            child,
            port,
            db_path,
        })
    }

    /// The URL to connect the meilisearch-sdk client to.
    pub fn url(&self) -> String {
        format!("http://127.0.0.1:{}", self.port)
    }

    /// The master key for the embedded instance.
    pub fn key(&self) -> &str {
        "qntx-dev"
    }

    /// The data directory path.
    #[allow(dead_code)]
    pub fn db_path(&self) -> &PathBuf {
        &self.db_path
    }

    /// Check if the MeiliSearch child process is still running.
    /// Returns false if the process has exited (crashed, killed, etc).
    pub fn is_alive(&mut self) -> bool {
        match self.child.try_wait() {
            Ok(None) => true, // still running
            Ok(Some(status)) => {
                warn!(
                    "Embedded MeiliSearch exited with {} (port {})",
                    status, self.port
                );
                false
            }
            Err(e) => {
                warn!(
                    "Failed to check MeiliSearch process status: {} (port {})",
                    e, self.port
                );
                false
            }
        }
    }
}

impl Drop for EmbeddedMeili {
    fn drop(&mut self) {
        info!(
            "Stopping embedded MeiliSearch (pid {}, port {})",
            self.child.id(),
            self.port
        );
        if let Err(e) = self.child.kill() {
            warn!("Failed to kill MeiliSearch subprocess: {}", e);
        }
        let _ = self.child.wait();
    }
}

/// Find an available TCP port by binding to port 0.
fn find_available_port() -> Result<u16, std::io::Error> {
    let listener = TcpListener::bind("127.0.0.1:0")?;
    let port = listener.local_addr()?.port();
    drop(listener);
    Ok(port)
}

