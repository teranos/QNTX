//! Logging utilities with QNTX segment prefixes.
//!
//! Provides consistent logging setup across QNTX Rust components.

use tracing_subscriber::{fmt, prelude::*, EnvFilter};

/// Initialize tracing with QNTX defaults.
///
/// Sets up tracing-subscriber with:
/// - Environment filter (RUST_LOG)
/// - Compact format suitable for terminal output
pub fn init() {
    init_with_filter("info");
}

/// Initialize tracing with a custom default filter.
pub fn init_with_filter(default_filter: &str) {
    let filter =
        EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new(default_filter));

    tracing_subscriber::registry()
        .with(filter)
        .with(fmt::layer().compact())
        .init();
}

/// QNTX segment prefixes for logging.
pub mod prefix {
    /// Pulse async operations prefix
    pub const PULSE: &str = "꩜";
    /// Graceful startup prefix
    pub const PULSE_OPEN: &str = "✿";
    /// Graceful shutdown prefix
    pub const PULSE_CLOSE: &str = "❀";
    /// Database operations prefix
    pub const DB: &str = "⊔";
}
