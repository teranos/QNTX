//! Graceful shutdown utilities for QNTX plugins.

use tokio::signal;
use tracing::info;

// Pulse symbol for logging
const PULSE_CLOSE: &str = "❀";

/// Returns a future that resolves when a shutdown signal is received.
///
/// Handles both Ctrl+C and SIGTERM (on Unix).
pub async fn shutdown_signal() {
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
            info!("{} Received Ctrl+C, shutting down", PULSE_CLOSE);
        }
        _ = terminate => {
            info!("{} Received terminate signal, shutting down", PULSE_CLOSE);
        }
    }
}
