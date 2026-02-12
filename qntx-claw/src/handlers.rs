//! HTTP endpoint handlers for the OpenClaw plugin.
//!
//! Endpoints:
//! - GET /snapshot   — Current workspace snapshot (all bootstrap files + memories)
//! - GET /bootstrap  — Bootstrap files only (AGENTS.md, SOUL.md, etc.)
//! - GET /memory     — Daily memory files
//! - GET /file/:name — Single bootstrap file content

use crate::config::PluginConfig;
use crate::proto::{HttpHeader, HttpResponse};
use crate::workspace::{ChangeEvent, Snapshot};
use parking_lot::RwLock as SyncRwLock;
use serde::Serialize;
use std::collections::HashMap;
use std::sync::Arc;
use tokio::sync::RwLock;
use tonic::Status;

/// Internal plugin state shared with service module.
pub(crate) struct PluginState {
    pub config: Option<PluginConfig>,
    pub initialized: bool,
    /// Shared snapshot updated by the watcher
    pub snapshot: Arc<RwLock<Snapshot>>,
    /// Recent change events (ring buffer, most recent first)
    pub recent_changes: Vec<ChangeEvent>,
}

/// Handler context providing access to plugin state.
pub struct HandlerContext {
    pub(crate) state: Arc<SyncRwLock<PluginState>>,
}

impl HandlerContext {
    pub fn new(state: Arc<SyncRwLock<PluginState>>) -> Self {
        Self { state }
    }

    /// Record a change event.
    pub fn record_change(&self, event: ChangeEvent) {
        let mut state = self.state.write();
        state.recent_changes.insert(0, event);
        if state.recent_changes.len() > 100 {
            state.recent_changes.truncate(100);
        }
    }

    /// Clone the snapshot Arc without holding the parking_lot guard across awaits.
    fn snapshot_arc(&self) -> Arc<RwLock<Snapshot>> {
        self.state.read().snapshot.clone()
    }

    /// GET /snapshot — Full workspace snapshot.
    pub async fn handle_snapshot(&self) -> Result<HttpResponse, Status> {
        let snapshot = self.snapshot_arc().read().await.clone();

        #[derive(Serialize)]
        struct SnapshotResponse {
            workspace_path: String,
            bootstrap_files: HashMap<String, BootstrapFileResponse>,
            daily_memories: Vec<DailyMemoryResponse>,
            taken_at: i64,
        }

        #[derive(Serialize)]
        struct BootstrapFileResponse {
            exists: bool,
            content: String,
            content_sha: String,
            mod_time: i64,
        }

        #[derive(Serialize)]
        struct DailyMemoryResponse {
            date: String,
            content: String,
            content_sha: String,
            mod_time: i64,
        }

        let mut bootstrap = HashMap::new();
        for (name, tf) in &snapshot.bootstrap_files {
            bootstrap.insert(
                name.clone(),
                BootstrapFileResponse {
                    exists: tf.exists,
                    content: tf.content.clone(),
                    content_sha: tf.content_sha.clone(),
                    mod_time: tf.mod_time,
                },
            );
        }

        let memories: Vec<_> = snapshot
            .daily_memories
            .iter()
            .map(|dm| DailyMemoryResponse {
                date: dm.date.clone(),
                content: dm.content.clone(),
                content_sha: dm.content_sha.clone(),
                mod_time: dm.mod_time,
            })
            .collect();

        let resp = SnapshotResponse {
            workspace_path: snapshot.workspace_path.display().to_string(),
            bootstrap_files: bootstrap,
            daily_memories: memories,
            taken_at: snapshot.taken_at,
        };

        json_response(200, &resp)
    }

    /// GET /bootstrap — Bootstrap files only.
    pub async fn handle_bootstrap(&self) -> Result<HttpResponse, Status> {
        let snapshot = self.snapshot_arc().read().await.clone();

        #[derive(Serialize)]
        struct FileEntry {
            name: String,
            exists: bool,
            content: String,
            content_sha: String,
            mod_time: i64,
        }

        let mut files: Vec<FileEntry> = snapshot
            .bootstrap_files
            .values()
            .map(|tf| FileEntry {
                name: tf.name.clone(),
                exists: tf.exists,
                content: tf.content.clone(),
                content_sha: tf.content_sha.clone(),
                mod_time: tf.mod_time,
            })
            .collect();

        // Stable ordering
        files.sort_by(|a, b| a.name.cmp(&b.name));

        json_response(200, &files)
    }

    /// GET /memory — Daily memory logs.
    pub async fn handle_memory(&self) -> Result<HttpResponse, Status> {
        let snapshot = self.snapshot_arc().read().await.clone();

        #[derive(Serialize)]
        struct MemoryEntry {
            date: String,
            content: String,
            content_sha: String,
            mod_time: i64,
        }

        let memories: Vec<MemoryEntry> = snapshot
            .daily_memories
            .iter()
            .map(|dm| MemoryEntry {
                date: dm.date.clone(),
                content: dm.content.clone(),
                content_sha: dm.content_sha.clone(),
                mod_time: dm.mod_time,
            })
            .collect();

        json_response(200, &memories)
    }

    /// GET /file/:name — Single bootstrap file content.
    pub async fn handle_file(&self, name: &str) -> Result<HttpResponse, Status> {
        let snapshot = self.snapshot_arc().read().await.clone();

        match snapshot.bootstrap_files.get(name) {
            Some(tf) if tf.exists => {
                // Return raw markdown content
                Ok(HttpResponse {
                    status_code: 200,
                    headers: vec![HttpHeader {
                        name: "Content-Type".to_string(),
                        values: vec!["text/markdown; charset=utf-8".to_string()],
                    }],
                    body: tf.content.as_bytes().to_vec(),
                })
            }
            Some(_) => Err(Status::not_found(format!(
                "Bootstrap file {} does not exist in workspace",
                name
            ))),
            None => Err(Status::not_found(format!(
                "Unknown bootstrap file: {}",
                name
            ))),
        }
    }

    /// GET /changes — Recent change events.
    pub async fn handle_changes(&self) -> Result<HttpResponse, Status> {
        let changes = {
            let state = self.state.read();

            state
                .recent_changes
                .iter()
                .map(|c| ChangeEntryResponse {
                    file: c.file.clone(),
                    category: c.category.clone(),
                    operation: c.operation.clone(),
                })
                .collect::<Vec<_>>()
        };

        json_response(200, &changes)
    }
}

#[derive(Serialize)]
struct ChangeEntryResponse {
    file: String,
    category: String,
    operation: String,
}

/// Create a JSON HTTP response.
fn json_response<T: Serialize>(status_code: i32, data: &T) -> Result<HttpResponse, Status> {
    let body = serde_json::to_vec(data)
        .map_err(|e| Status::internal(format!("failed to serialize response: {}", e)))?;

    Ok(HttpResponse {
        status_code,
        headers: vec![HttpHeader {
            name: "Content-Type".to_string(),
            values: vec!["application/json".to_string()],
        }],
        body,
    })
}
