//! gRPC service implementation for the OpenClaw plugin.
//!
//! Implements the DomainPluginService interface for QNTX.
//! On initialize, discovers the OpenClaw workspace, takes a snapshot,
//! and starts watching for changes. Changes are ingested as attestations
//! via the ATSStore gRPC client.

use crate::config::PluginConfig;
use crate::handlers::{HandlerContext, PluginState};
use crate::proto::{
    domain_plugin_service_server::DomainPluginService, ConfigSchemaResponse, Empty,
    ExecuteJobRequest, ExecuteJobResponse, HealthResponse, HttpRequest, HttpResponse,
    InitializeRequest, InitializeResponse, MetadataResponse, WebSocketMessage,
};
use crate::workspace::{self, Snapshot, WatchHandle};
use parking_lot::RwLock as SyncRwLock;
use std::collections::HashMap;
use std::pin::Pin;
use std::sync::Arc;
use tokio::sync::RwLock;
use tokio_stream::Stream;
use tonic::{Request, Response, Status, Streaming};
use tracing::{debug, error, info, warn};

/// OpenClaw observability plugin gRPC service.
pub struct ClawPluginService {
    handlers: HandlerContext,
    watch_handle: Arc<SyncRwLock<Option<WatchHandle>>>,
}

impl ClawPluginService {
    /// Create a new ClawPluginService.
    pub fn new() -> Result<Self, Box<dyn std::error::Error>> {
        // Start with an empty snapshot — real data loaded on initialize()
        let empty_snapshot = Snapshot {
            workspace_path: std::path::PathBuf::new(),
            bootstrap_files: HashMap::new(),
            daily_memories: Vec::new(),
            taken_at: 0,
        };

        let snapshot = Arc::new(RwLock::new(empty_snapshot));

        let state = Arc::new(SyncRwLock::new(PluginState {
            config: None,
            initialized: false,
            snapshot,
            recent_changes: Vec::new(),
        }));

        Ok(Self {
            handlers: HandlerContext::new(state),
            watch_handle: Arc::new(SyncRwLock::new(None)),
        })
    }

    /// Start watching the workspace and ingesting changes as attestations.
    async fn start_watcher(
        &self,
        workspace_path: std::path::PathBuf,
        snapshot: Arc<RwLock<Snapshot>>,
    ) -> Result<(), String> {
        let (mut rx, handle) = workspace::watch(workspace_path, snapshot).await?;

        // Store handle for shutdown
        {
            let mut wh = self.watch_handle.write();
            *wh = Some(handle);
        }

        // Spawn change event processor
        let handlers = self.handlers.state.clone();
        tokio::spawn(async move {
            while let Some(event) = rx.recv().await {
                // Record the change
                {
                    let mut state = handlers.write();
                    state.recent_changes.insert(0, event.clone());
                    if state.recent_changes.len() > 100 {
                        state.recent_changes.truncate(100);
                    }
                }

                // TODO: Create attestation via ATSStore gRPC client
                // e.g., subject=AGENTS.md predicate=modified context=openclaw-workspace
                debug!(
                    "Change recorded: {} {} ({})",
                    event.operation, event.file, event.category
                );
            }
        });

        Ok(())
    }
}

#[tonic::async_trait]
impl DomainPluginService for ClawPluginService {
    /// Return plugin metadata.
    async fn metadata(
        &self,
        _request: Request<Empty>,
    ) -> Result<Response<MetadataResponse>, Status> {
        debug!("Metadata request received");
        Ok(Response::new(MetadataResponse {
            name: "claw".to_string(),
            version: env!("CARGO_PKG_VERSION").to_string(),
            qntx_version: ">=0.1.0".to_string(),
            description: "OpenClaw observability — watches workspace files and ingests changes"
                .to_string(),
            author: "QNTX Contributors".to_string(),
            license: "MIT".to_string(),
        }))
    }

    /// Initialize the plugin: discover workspace, take snapshot, start watcher.
    async fn initialize(
        &self,
        request: Request<InitializeRequest>,
    ) -> Result<Response<InitializeResponse>, Status> {
        let req = request.into_inner();
        info!("Initializing OpenClaw plugin");

        // Extract workspace path from config (or auto-discover)
        let explicit_path = req.config.get("workspace_path").cloned();
        let explicit_ref = explicit_path.as_deref().filter(|s| !s.is_empty());

        let workspace_path = workspace::discover(explicit_ref).map_err(|e| {
            error!("Failed to discover OpenClaw workspace: {}", e);
            Status::failed_precondition(format!("OpenClaw workspace not found: {}", e))
        })?;

        info!(
            "Discovered OpenClaw workspace at {}",
            workspace_path.display()
        );

        // Take initial snapshot
        let snap = workspace::take_snapshot(&workspace_path).map_err(|e| {
            error!(
                "Failed to snapshot workspace {}: {}",
                workspace_path.display(),
                e
            );
            Status::internal(format!("failed to snapshot workspace: {}", e))
        })?;

        let bootstrap_count = snap.bootstrap_files.values().filter(|f| f.exists).count();
        let memory_count = snap.daily_memories.len();
        info!(
            "Initial snapshot: {} bootstrap files present, {} daily memories",
            bootstrap_count, memory_count
        );

        let snapshot = Arc::new(RwLock::new(snap));

        // Update plugin state
        {
            let mut state = self.handlers.state.write();
            state.config = Some(PluginConfig {
                ats_store_endpoint: req.ats_store_endpoint.clone(),
                queue_endpoint: req.queue_endpoint,
                auth_token: req.auth_token,
                config: req.config,
            });
            state.snapshot = snapshot.clone();
            state.initialized = true;
        }

        // Start file watcher
        let watch_enabled = {
            let state = self.handlers.state.read();
            state
                .config
                .as_ref()
                .and_then(|c| c.config.get("watch_enabled"))
                .map(|v| v != "false")
                .unwrap_or(true)
        };

        if watch_enabled {
            if let Err(e) = self
                .start_watcher(workspace_path.clone(), snapshot)
                .await
            {
                warn!(
                    "Failed to start workspace watcher for {}: {}",
                    workspace_path.display(),
                    e
                );
                // Non-fatal: plugin still works, just no live updates
            }
        }

        // Announce handler capabilities
        let handler_names = vec!["claw.snapshot".to_string()];
        info!("OpenClaw plugin initialized");

        Ok(Response::new(InitializeResponse { handler_names }))
    }

    /// Shutdown the plugin.
    async fn shutdown(&self, _request: Request<Empty>) -> Result<Response<Empty>, Status> {
        info!("Shutting down OpenClaw plugin");

        // Stop watcher
        let handle = {
            let mut wh = self.watch_handle.write();
            wh.take()
        };
        if let Some(h) = handle {
            h.stop().await;
        }

        let mut state = self.handlers.state.write();
        state.initialized = false;
        state.config = None;

        Ok(Response::new(Empty {}))
    }

    /// Handle HTTP requests — route to appropriate handler.
    async fn handle_http(
        &self,
        request: Request<HttpRequest>,
    ) -> Result<Response<HttpResponse>, Status> {
        let req = request.into_inner();
        let path = &req.path;
        let method = &req.method;

        debug!("HTTP request: {} {}", method, path);

        let result = match (method.as_str(), path.as_str()) {
            ("GET", "/snapshot") => self.handlers.handle_snapshot().await,
            ("GET", "/bootstrap") => self.handlers.handle_bootstrap().await,
            ("GET", "/memory") => self.handlers.handle_memory().await,
            ("GET", "/changes") => self.handlers.handle_changes().await,
            ("GET", p) if p.starts_with("/file/") => {
                let name = &p[6..]; // strip "/file/"
                self.handlers.handle_file(name).await
            }
            _ => Err(Status::not_found(format!(
                "Unknown endpoint: {} {}",
                method, path
            ))),
        };

        match result {
            Ok(response) => Ok(Response::new(response)),
            Err(status) => {
                let error_body = serde_json::json!({ "error": status.message() });
                Ok(Response::new(HttpResponse {
                    status_code: match status.code() {
                        tonic::Code::NotFound => 404,
                        tonic::Code::InvalidArgument => 400,
                        tonic::Code::FailedPrecondition => 412,
                        tonic::Code::Internal => 500,
                        tonic::Code::Unavailable => 503,
                        _ => 500,
                    },
                    headers: vec![crate::proto::HttpHeader {
                        name: "Content-Type".to_string(),
                        values: vec!["application/json".to_string()],
                    }],
                    body: serde_json::to_vec(&error_body).unwrap_or_default(),
                }))
            }
        }
    }

    /// WebSocket not supported.
    type HandleWebSocketStream =
        Pin<Box<dyn Stream<Item = Result<WebSocketMessage, Status>> + Send>>;

    async fn handle_web_socket(
        &self,
        _request: Request<Streaming<WebSocketMessage>>,
    ) -> Result<Response<Self::HandleWebSocketStream>, Status> {
        Err(Status::unimplemented(
            "WebSocket not supported by claw plugin",
        ))
    }

    /// Check plugin health.
    async fn health(&self, _request: Request<Empty>) -> Result<Response<HealthResponse>, Status> {
        let (healthy, recent_changes_count, snapshot_arc) = {
            let state = self.handlers.state.read();
            (
                state.initialized,
                state.recent_changes.len(),
                state.snapshot.clone(),
            )
        };

        let mut details = HashMap::new();
        details.insert("initialized".to_string(), healthy.to_string());
        details.insert(
            "recent_changes".to_string(),
            recent_changes_count.to_string(),
        );

        if healthy {
            let snap = snapshot_arc.read().await;
            let bootstrap_present = snap.bootstrap_files.values().filter(|f| f.exists).count();
            details.insert("bootstrap_files".to_string(), bootstrap_present.to_string());
            details.insert(
                "daily_memories".to_string(),
                snap.daily_memories.len().to_string(),
            );
            details.insert(
                "workspace_path".to_string(),
                snap.workspace_path.display().to_string(),
            );
        }

        Ok(Response::new(HealthResponse {
            healthy,
            message: if healthy {
                "OK".to_string()
            } else {
                "Not initialized".to_string()
            },
            details,
        }))
    }

    /// Return configuration schema.
    async fn config_schema(
        &self,
        _request: Request<Empty>,
    ) -> Result<Response<ConfigSchemaResponse>, Status> {
        debug!("ConfigSchema request received");
        Ok(Response::new(ConfigSchemaResponse {
            fields: crate::config::build_schema(),
        }))
    }

    /// Execute an async job (snapshot refresh).
    async fn execute_job(
        &self,
        request: Request<ExecuteJobRequest>,
    ) -> Result<Response<ExecuteJobResponse>, Status> {
        let req = request.into_inner();

        debug!(
            "ExecuteJob request: job_id={}, handler={}",
            req.job_id, req.handler_name
        );

        match req.handler_name.as_str() {
            "claw.snapshot" => {
                // Refresh the snapshot
                let snapshot_arc = { self.handlers.state.read().snapshot.clone() };
                let workspace_path = snapshot_arc.read().await.workspace_path.clone();

                if workspace_path.as_os_str().is_empty() {
                    return Ok(Response::new(ExecuteJobResponse {
                        success: false,
                        error: "workspace not initialized".to_string(),
                        result: vec![],
                        progress_current: 0,
                        progress_total: 0,
                        cost_actual: 0.0,
                    }));
                }

                match workspace::take_snapshot(&workspace_path) {
                    Ok(new_snap) => {
                        let result_json = serde_json::json!({
                            "bootstrap_files": new_snap.bootstrap_files.values()
                                .filter(|f| f.exists)
                                .count(),
                            "daily_memories": new_snap.daily_memories.len(),
                        });

                        // Update shared snapshot
                        {
                            let mut snap = snapshot_arc.write().await;
                            *snap = new_snap;
                        }

                        let result_bytes = serde_json::to_vec(&result_json).unwrap_or_default();

                        Ok(Response::new(ExecuteJobResponse {
                            success: true,
                            error: String::new(),
                            result: result_bytes,
                            progress_current: 0,
                            progress_total: 0,
                            cost_actual: 0.0,
                        }))
                    }
                    Err(e) => Ok(Response::new(ExecuteJobResponse {
                        success: false,
                        error: format!("snapshot failed for {}: {}", workspace_path.display(), e),
                        result: vec![],
                        progress_current: 0,
                        progress_total: 0,
                        cost_actual: 0.0,
                    })),
                }
            }
            _ => Err(Status::not_found(format!(
                "Unknown handler: {}",
                req.handler_name
            ))),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn test_metadata() {
        let service = ClawPluginService::new().unwrap();
        let response = service.metadata(Request::new(Empty {})).await.unwrap();
        let meta = response.into_inner();
        assert_eq!(meta.name, "claw");
        assert!(!meta.version.is_empty());
        assert_eq!(meta.qntx_version, ">=0.1.0");
    }

    #[tokio::test]
    async fn test_health_before_init() {
        let service = ClawPluginService::new().unwrap();
        let response = service.health(Request::new(Empty {})).await.unwrap();
        let health = response.into_inner();
        assert!(!health.healthy);
        assert_eq!(health.message, "Not initialized");
    }

    #[tokio::test]
    async fn test_config_schema() {
        let service = ClawPluginService::new().unwrap();
        let response = service.config_schema(Request::new(Empty {})).await.unwrap();
        let schema = response.into_inner();
        assert!(schema.fields.contains_key("workspace_path"));
        assert!(schema.fields.contains_key("watch_enabled"));
    }

    #[tokio::test]
    async fn test_initialize_with_temp_workspace() {
        let dir = tempfile::tempdir().unwrap();

        // Create some files
        std::fs::write(dir.path().join("AGENTS.md"), "Test agent instructions").unwrap();
        std::fs::write(dir.path().join("SOUL.md"), "Kind and helpful").unwrap();
        std::fs::create_dir_all(dir.path().join("memory")).unwrap();
        std::fs::write(
            dir.path().join("memory/2026-02-12.md"),
            "# Today\nDid things.",
        )
        .unwrap();

        let service = ClawPluginService::new().unwrap();

        let mut config = HashMap::new();
        config.insert(
            "workspace_path".to_string(),
            dir.path().display().to_string(),
        );
        // Disable watcher in tests to avoid async cleanup issues
        config.insert("watch_enabled".to_string(), "false".to_string());

        let req = InitializeRequest {
            ats_store_endpoint: String::new(),
            queue_endpoint: String::new(),
            auth_token: String::new(),
            config,
        };

        let response = service.initialize(Request::new(req)).await.unwrap();
        let init = response.into_inner();
        assert!(init.handler_names.contains(&"claw.snapshot".to_string()));

        // Check health shows initialized
        let health_resp = service.health(Request::new(Empty {})).await.unwrap();
        let health = health_resp.into_inner();
        assert!(health.healthy);
        assert_eq!(health.details.get("bootstrap_files").unwrap(), "2");
        assert_eq!(health.details.get("daily_memories").unwrap(), "1");
    }

    #[tokio::test]
    async fn test_http_snapshot_after_init() {
        let dir = tempfile::tempdir().unwrap();
        std::fs::write(dir.path().join("AGENTS.md"), "You are helpful.").unwrap();

        let service = ClawPluginService::new().unwrap();

        let mut config = HashMap::new();
        config.insert(
            "workspace_path".to_string(),
            dir.path().display().to_string(),
        );
        config.insert("watch_enabled".to_string(), "false".to_string());

        let req = InitializeRequest {
            ats_store_endpoint: String::new(),
            queue_endpoint: String::new(),
            auth_token: String::new(),
            config,
        };
        service.initialize(Request::new(req)).await.unwrap();

        // Request snapshot via HTTP
        let http_req = HttpRequest {
            method: "GET".to_string(),
            path: "/snapshot".to_string(),
            headers: vec![],
            body: vec![],
        };

        let resp = service
            .handle_http(Request::new(http_req))
            .await
            .unwrap()
            .into_inner();

        assert_eq!(resp.status_code, 200);

        let body: serde_json::Value = serde_json::from_slice(&resp.body).unwrap();
        let agents = &body["bootstrap_files"]["AGENTS.md"];
        assert_eq!(agents["exists"], true);
        assert_eq!(agents["content"], "You are helpful.");
    }
}
