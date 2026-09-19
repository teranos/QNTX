// Re-export proto definitions from qntx-grpc
// Proto files are compiled in qntx-grpc's build.rs (when plugin feature is enabled)

#![allow(clippy::derive_partial_eq_without_eq)]
#![allow(clippy::enum_variant_names)]

pub use qntx_grpc::plugin::proto::*;
