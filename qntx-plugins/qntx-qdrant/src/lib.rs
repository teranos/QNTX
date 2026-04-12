//! qntx-qdrant: a single plugin providing both SearchService (ADR-015)
//! and VectorSearchService (ADR-016) against a plugin-managed Qdrant
//! instance (ADR-017).
//!
//! The plugin owns Qdrant's entire lifecycle:
//!   - the binary it spawns (supplied by the nix flake, never the user's PATH)
//!   - the data directory (under plugin state, never a user-chosen location)
//!   - the listen port (loopback-only, never user-configurable)
//!   - startup readiness, shutdown, and crash recovery
//!
//! To a QNTX deployment, the plugin is a black box that answers Search and
//! VectorSearch RPCs. No external Qdrant is installed, configured, or exposed.

pub mod proto;
pub mod qdrant;
pub mod search;
pub mod service;
pub mod vector;

pub use service::QdrantPluginService;
