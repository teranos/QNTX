//! gRPC plugin infrastructure for QNTX plugins.
//!
//! Provides common scaffolding for building QNTX plugins:
//! - Server setup with graceful shutdown
//! - Proto definitions (compiled from plugin/grpc/protocol/)
//! - Common service patterns

mod server;
mod shutdown;

pub mod proto {
    //! Compiled protobuf definitions.
    tonic::include_proto!("domain");
    tonic::include_proto!("atsstore");
    tonic::include_proto!("queue");
}

pub use server::PluginServer;
pub use shutdown::shutdown_signal;
