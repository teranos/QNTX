//! Protocol Buffer type definitions for QNTX.
//!
//! This crate provides the canonical Rust types generated from proto files,
//! used across all QNTX Rust code including WASM modules and gRPC plugins.
//!
//! Types are generated with serde support for JSON serialization.
//! Generated code is committed to avoid requiring protoc in CI.

// Include generated proto code from committed file
// Run `make proto-rust` to regenerate when proto files change
pub mod protocol {
    include!("generated/protocol.rs");
}

// Re-export commonly used types at crate root for convenience
pub use protocol::*;

#[cfg(test)]
mod test;
