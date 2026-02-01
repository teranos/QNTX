//! Protocol Buffer type definitions for QNTX.
//!
//! This crate provides the canonical Rust types generated from proto files,
//! used across all QNTX Rust code including WASM modules and gRPC plugins.
//!
//! Types are generated with serde support for JSON serialization.

// Include generated proto code
// prost generates all types in the 'protocol' module since all protos use 'package protocol'
pub mod protocol {
    include!(concat!(env!("OUT_DIR"), "/protocol.rs"));
}

// Re-export commonly used types at crate root for convenience
pub use protocol::*;

#[cfg(test)]
mod test;
