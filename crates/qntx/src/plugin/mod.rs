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
    //! All proto files use `package protocol`, so they're compiled into a single module.
    tonic::include_proto!("protocol");
}

pub use server::PluginServer;
pub use shutdown::shutdown_signal;
