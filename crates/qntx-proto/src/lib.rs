//! Protocol Buffer type definitions for QNTX.
//!
//! This crate provides the canonical Rust types generated from proto files,
//! used across all QNTX Rust code including WASM modules and gRPC plugins.
//!
//! Types are generated at build time with serde support for JSON serialization.
//! Uses protoc-bin-vendored to avoid requiring protoc installation.

// Include generated proto code from build.rs output
pub mod protocol {
    include!(concat!(env!("OUT_DIR"), "/protocol.rs"));
}

// Canonical symbol definitions from proto/sym.proto
pub mod sym {
    include!(concat!(env!("OUT_DIR"), "/qntx.sym.rs"));
}

// Re-export commonly used types at crate root for convenience
pub use protocol::*;

// Proto conversion utilities for attestations
pub mod proto_convert;

#[cfg(test)]
mod test;
