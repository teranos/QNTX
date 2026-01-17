// Re-export proto definitions from qntx shared crate
// Proto files are compiled in qntx's build.rs (when plugin feature is enabled)

#![allow(clippy::derive_partial_eq_without_eq)]
#![allow(clippy::enum_variant_names)]

pub use qntx::plugin::proto::*;
