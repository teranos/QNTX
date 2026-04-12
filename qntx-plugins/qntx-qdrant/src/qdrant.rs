//! Qdrant engine, linked in-process.
//!
//! ADR-017: the whole engine lives inside the plugin. No Qdrant binary, no
//! child process, no supervisor — `Engine` is just a Rust value that the
//! plugin constructs at startup and holds for its lifetime.
//!
//! The engine is backed by Qdrant's `segment` crate (git dep on
//! qdrant/qdrant). A `Segment` is Qdrant's on-disk indexed store: HNSW for
//! vectors, payload indexes for structured fields. One segment per named
//! index is sufficient for a single-node plugin; collection/shard/replica
//! layers from upstream Qdrant are not needed here and are not linked.

use parking_lot::RwLock;
use segment::segment::Segment;
use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::sync::Arc;
use thiserror::Error;
use tracing::info;

#[derive(Debug, Error)]
pub enum EngineError {
    #[error("engine state dir unavailable: {0}")]
    StateDir(#[from] std::io::Error),

    #[error("index '{0}' not found")]
    UnknownIndex(String),

    #[error("index '{0}' already exists")]
    IndexExists(String),

    #[error("segment op failed: {0}")]
    Segment(String),
}

/// Plugin-owned state directory. Provided by the plugin host at Initialize
/// time; falls back to a per-user cache location.
const STATE_ENV: &str = "QNTX_QDRANT_STATE";

/// In-process Qdrant engine.
///
/// One `Engine` instance per plugin process. Cloneable via `Arc`. Holds a
/// map from index name to open `Segment`. All service RPCs go through here.
#[derive(Clone)]
pub struct Engine {
    inner: Arc<Inner>,
}

struct Inner {
    root: PathBuf,
    indexes: RwLock<HashMap<String, Arc<RwLock<Segment>>>>,
}

impl Engine {
    /// Lay out the engine's state directory. Does not open any existing
    /// indexes; segments are opened lazily on first touch.
    pub fn open() -> Result<Self, EngineError> {
        let root = resolve_state_dir();
        std::fs::create_dir_all(&root)?;
        info!(data_dir = %root.display(), "qdrant engine opened (in-process)");
        Ok(Self {
            inner: Arc::new(Inner {
                root,
                indexes: RwLock::new(HashMap::new()),
            }),
        })
    }

    pub fn state_dir(&self) -> &Path {
        &self.inner.root
    }

    /// Handle to a single named index's segment. The lock is held while the
    /// caller runs a read or write on the segment; segment methods are sync,
    /// so this keeps the API straightforward.
    pub fn with_index<R>(
        &self,
        name: &str,
        f: impl FnOnce(&Segment) -> Result<R, EngineError>,
    ) -> Result<R, EngineError> {
        let handle = {
            let guard = self.inner.indexes.read();
            guard
                .get(name)
                .cloned()
                .ok_or_else(|| EngineError::UnknownIndex(name.to_string()))?
        };
        let seg = handle.read();
        f(&seg)
    }

    /// Register a new segment under `name`. Errors if the name is taken.
    ///
    /// Segment creation is a synchronous disk op; callers that need
    /// non-blocking behaviour should wrap this in `spawn_blocking`.
    pub fn create_index(
        &self,
        name: &str,
        segment: Segment,
    ) -> Result<(), EngineError> {
        let mut guard = self.inner.indexes.write();
        if guard.contains_key(name) {
            return Err(EngineError::IndexExists(name.to_string()));
        }
        guard.insert(name.to_string(), Arc::new(RwLock::new(segment)));
        Ok(())
    }

    /// Directory under which a named index's segment files live.
    pub fn index_path(&self, name: &str) -> PathBuf {
        self.inner.root.join("indexes").join(name)
    }
}

fn resolve_state_dir() -> PathBuf {
    if let Ok(p) = std::env::var(STATE_ENV) {
        return PathBuf::from(p);
    }
    // Fallback so the plugin is runnable without the host wiring the env var.
    let base = std::env::var_os("XDG_CACHE_HOME")
        .map(PathBuf::from)
        .or_else(|| std::env::var_os("HOME").map(|h| PathBuf::from(h).join(".cache")))
        .unwrap_or_else(|| PathBuf::from("."));
    base.join("qntx").join("qntx-qdrant").join("data")
}
