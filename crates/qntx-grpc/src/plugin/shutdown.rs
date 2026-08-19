//! Graceful shutdown utilities for QNTX plugins.

use tokio::signal;
use tracing::{info, warn};

// Pulse symbol for logging
const PULSE_CLOSE: &str = "❀";

/// Returns a future that resolves when a shutdown signal is received.
///
/// Handles both Ctrl+C and SIGTERM (on Unix).
pub async fn shutdown_signal() {
    let ctrl_c = async {
        if let Err(e) = signal::ctrl_c().await {
            warn!("{} Ctrl+C handler unavailable: {e}", PULSE_CLOSE);
            std::future::pending::<()>().await
        }
    };

    #[cfg(unix)]
    let terminate = async {
        match signal::unix::signal(signal::unix::SignalKind::terminate()) {
            Ok(mut sig) => {
                sig.recv().await;
            }
            Err(e) => {
                warn!("{} SIGTERM handler unavailable: {e}", PULSE_CLOSE);
                std::future::pending::<()>().await
            }
        }
    };

    #[cfg(not(unix))]
    let terminate = std::future::pending::<()>();

    tokio::select! {
        _ = ctrl_c => {
            info!("{} Received Ctrl+C, shutting down", PULSE_CLOSE);
        }
        _ = terminate => {
            info!("{} Received terminate signal, shutting down", PULSE_CLOSE);
        }
    }
}
