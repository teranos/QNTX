//! Plugin server utilities.

use std::net::SocketAddr;
use tonic::transport::Server;
use tracing::info;

use super::shutdown::shutdown_signal;
use crate::error::Result;
use crate::tracing::prefix;

/// Builder for creating QNTX plugin servers.
pub struct PluginServer {
    addr: SocketAddr,
    name: String,
    version: String,
}

impl PluginServer {
    /// Create a new plugin server builder.
    pub fn new(name: impl Into<String>, version: impl Into<String>) -> Self {
        Self {
            addr: "0.0.0.0:9000".parse().unwrap(),
            name: name.into(),
            version: version.into(),
        }
    }

    /// Set the server address.
    pub fn address(mut self, addr: SocketAddr) -> Self {
        self.addr = addr;
        self
    }

    /// Set the server port (uses 0.0.0.0 as host).
    pub fn port(mut self, port: u16) -> Self {
        self.addr = format!("0.0.0.0:{}", port).parse().unwrap();
        self
    }

    /// Run the server with the provided gRPC service.
    ///
    /// This method handles:
    /// - Logging startup/shutdown
    /// - Graceful shutdown on SIGTERM/Ctrl+C
    pub async fn serve<S>(self, service: S) -> Result<()>
    where
        S: tonic::codegen::Service<
                http::Request<tonic::body::BoxBody>,
                Response = http::Response<tonic::body::BoxBody>,
                Error = std::convert::Infallible,
            > + tonic::server::NamedService
            + Clone
            + Send
            + 'static,
        S::Future: Send + 'static,
    {
        info!(
            "{} Starting {} v{}",
            prefix::PULSE_OPEN,
            self.name,
            self.version
        );
        info!("  Address: {}", self.addr);

        Server::builder()
            .add_service(service)
            .serve_with_shutdown(self.addr, shutdown_signal())
            .await?;

        info!("{} {} shutdown complete", prefix::PULSE_CLOSE, self.name);
        Ok(())
    }
}
