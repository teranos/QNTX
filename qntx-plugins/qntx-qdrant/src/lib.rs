//! qntx-qdrant: a single plugin providing both SearchService (ADR-015)
//! and VectorSearchService (ADR-016) from a Qdrant engine linked in-process
//! (ADR-017).
//!
//! The plugin links Qdrant's `segment` crate directly — there is no Qdrant
//! binary, no child process, no sidecar. The whole engine lives inside
//! this plugin's address space.

pub mod proto;
pub mod qdrant;
pub mod search;
pub mod service;
pub mod vector;

pub use service::QdrantPluginService;
