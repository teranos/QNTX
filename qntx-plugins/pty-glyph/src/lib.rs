//! QNTX PTY Glyph Plugin Library
//!
//! Provides persistent terminal glyphs with full PTY support for QNTX.

pub mod proto;
mod pty;
mod websocket;

use parking_lot::RwLock;
use pty::PTYManager;
use std::collections::HashMap;
use std::sync::Arc;
use tonic::{Request, Response, Status, Streaming};
use tracing::{debug, info};

use proto::domain_plugin_service_server::DomainPluginService;
use proto::*;

/// PTY Glyph Plugin Service
pub struct PTYGlyphService {
    pty_manager: Arc<RwLock<PTYManager>>,
}

impl PTYGlyphService {
    pub fn new() -> Self {
        // Default to current directory or /tmp
        let default_home = std::env::current_dir()
            .ok()
            .and_then(|p| p.to_str().map(|s| s.to_string()))
            .unwrap_or_else(|| "/tmp".to_string());

        Self {
            pty_manager: Arc::new(RwLock::new(PTYManager::new(default_home))),
        }
    }
}

#[tonic::async_trait]
impl DomainPluginService for PTYGlyphService {
    type HandleWebSocketStream =
        tokio_stream::wrappers::ReceiverStream<Result<WebSocketMessage, Status>>;

    async fn metadata(
        &self,
        _request: Request<Empty>,
    ) -> Result<Response<MetadataResponse>, Status> {
        debug!("Received GetMetadata request");

        Ok(Response::new(MetadataResponse {
            name: "pty-glyph".to_string(),
            version: env!("CARGO_PKG_VERSION").to_string(),
            qntx_version: ">=0.1.0".to_string(),
            description: "Persistent terminal glyphs with full PTY support".to_string(),
            author: "QNTX Team".to_string(),
            license: "MIT".to_string(),
        }))
    }

    async fn initialize(
        &self,
        request: Request<InitializeRequest>,
    ) -> Result<Response<InitializeResponse>, Status> {
        let req = request.into_inner();
        info!("Initializing PTY glyph plugin");
        debug!("Config: {:?}", req.config);

        // Get home directory from config (config > $HOME > current dir > /tmp)
        let home_dir = req
            .config
            .get("home_directory")
            .cloned()
            .or_else(|| std::env::var("HOME").ok())
            .or_else(|| {
                std::env::current_dir()
                    .ok()
                    .and_then(|p| p.to_str().map(|s| s.to_string()))
            })
            .unwrap_or_else(|| "/tmp".to_string());

        info!("PTY sessions will start in: {}", home_dir);

        // Update PTY manager with configured home
        *self.pty_manager.write() = PTYManager::new(home_dir);

        Ok(Response::new(InitializeResponse {
            handler_names: vec![],
            schedules: vec![],
        }))
    }

    async fn shutdown(&self, _request: Request<Empty>) -> Result<Response<Empty>, Status> {
        info!("Shutting down PTY glyph plugin");

        // Clean up all PTY sessions
        self.pty_manager.write().shutdown_all();

        Ok(Response::new(Empty {}))
    }

    async fn health(&self, _request: Request<Empty>) -> Result<Response<HealthResponse>, Status> {
        let active_sessions = self.pty_manager.read().session_count();

        let mut details = HashMap::new();
        details.insert("active_sessions".to_string(), active_sessions.to_string());

        Ok(Response::new(HealthResponse {
            healthy: true,
            message: "OK".to_string(),
            details,
        }))
    }

    async fn handle_http(
        &self,
        request: Request<HttpRequest>,
    ) -> Result<Response<HttpResponse>, Status> {
        let req = request.into_inner();
        debug!("Handling HTTP request: {} {}", req.method, req.path);

        // Route HTTP requests
        match (req.method.as_str(), req.path.as_str()) {
            ("GET", path) if path.starts_with("/api/pty-glyph/terminal") => {
                self.handle_terminal_ui(req).await
            }
            ("POST", "/api/pty-glyph/create") => self.handle_create_pty(req).await,
            ("GET", path) if path.starts_with("/api/pty-glyph/session/") => {
                self.handle_get_session(req).await
            }
            ("DELETE", path) if path.starts_with("/api/pty-glyph/session/") => {
                self.handle_kill_session(req).await
            }
            _ => Ok(Response::new(HttpResponse {
                status_code: 404,
                headers: vec![],
                body: b"Not Found".to_vec(),
            })),
        }
    }

    async fn handle_web_socket(
        &self,
        request: Request<Streaming<WebSocketMessage>>,
    ) -> Result<Response<Self::HandleWebSocketStream>, Status> {
        // Extract session_id from request metadata
        let session_id = request
            .metadata()
            .get("session_id")
            .and_then(|v| v.to_str().ok())
            .ok_or_else(|| Status::invalid_argument("Missing session_id in metadata"))?
            .to_string();

        debug!("WebSocket connection request for session: {}", session_id);

        // Handle WebSocket connection
        let rx = websocket::handle_pty_websocket(
            request.into_inner(),
            session_id,
            self.pty_manager.clone(),
        )
        .await?;

        Ok(Response::new(tokio_stream::wrappers::ReceiverStream::new(
            rx,
        )))
    }

    async fn config_schema(
        &self,
        _request: Request<Empty>,
    ) -> Result<Response<ConfigSchemaResponse>, Status> {
        debug!("ConfigSchema request");
        Ok(Response::new(ConfigSchemaResponse {
            fields: HashMap::new(),
        }))
    }

    async fn register_glyphs(
        &self,
        _request: Request<Empty>,
    ) -> Result<Response<GlyphDefResponse>, Status> {
        debug!("RegisterGlyphs request");

        let glyphs = vec![GlyphDef {
            symbol: "⌨".to_string(),
            title: "Terminal".to_string(),
            label: "pty".to_string(),
            content_path: "/terminal".to_string(),
            css_path: String::new(),
            default_width: 800,
            default_height: 600,
        }];

        Ok(Response::new(GlyphDefResponse { glyphs }))
    }

    async fn execute_job(
        &self,
        _request: Request<ExecuteJobRequest>,
    ) -> Result<Response<ExecuteJobResponse>, Status> {
        // PTY glyph doesn't use background jobs
        Err(Status::unimplemented("Background jobs not supported"))
    }
}

impl PTYGlyphService {
    async fn handle_terminal_ui(
        &self,
        _req: HttpRequest,
    ) -> Result<Response<HttpResponse>, Status> {
        let html = include_str!("../static/terminal.html");

        Ok(Response::new(HttpResponse {
            status_code: 200,
            headers: vec![HttpHeader {
                name: "Content-Type".to_string(),
                values: vec!["text/html".to_string()],
            }],
            body: html.as_bytes().to_vec(),
        }))
    }

    async fn handle_create_pty(&self, req: HttpRequest) -> Result<Response<HttpResponse>, Status> {
        debug!("Create PTY request - body length: {}", req.body.len());
        debug!(
            "Create PTY request - body: {:?}",
            String::from_utf8_lossy(&req.body)
        );

        // Parse request body
        let body: serde_json::Value = serde_json::from_slice(&req.body).map_err(|e| {
            Status::invalid_argument(format!(
                "Invalid JSON: {} (body: {:?})",
                e,
                String::from_utf8_lossy(&req.body)
            ))
        })?;

        let glyph_id = body["glyph_id"]
            .as_str()
            .ok_or_else(|| Status::invalid_argument("Missing glyph_id"))?;

        // Create PTY session
        let session_id = self
            .pty_manager
            .write()
            .create_session(glyph_id)
            .map_err(|e| Status::internal(format!("Failed to create PTY: {}", e)))?;

        let response = serde_json::json!({
            "pty_id": session_id
        });

        Ok(Response::new(HttpResponse {
            status_code: 200,
            headers: vec![HttpHeader {
                name: "Content-Type".to_string(),
                values: vec!["application/json".to_string()],
            }],
            body: response.to_string().as_bytes().to_vec(),
        }))
    }

    async fn handle_get_session(&self, req: HttpRequest) -> Result<Response<HttpResponse>, Status> {
        // Extract session ID from path
        let id = req
            .path
            .strip_prefix("/api/pty-glyph/session/")
            .ok_or_else(|| Status::invalid_argument("Invalid path"))?;

        let session = self
            .pty_manager
            .read()
            .get_session(id)
            .ok_or_else(|| Status::not_found("Session not found"))?;

        Ok(Response::new(HttpResponse {
            status_code: 200,
            headers: vec![HttpHeader {
                name: "Content-Type".to_string(),
                values: vec!["application/json".to_string()],
            }],
            body: serde_json::to_vec(&session)
                .map_err(|e| Status::internal(format!("Serialization error: {}", e)))?,
        }))
    }

    async fn handle_kill_session(
        &self,
        req: HttpRequest,
    ) -> Result<Response<HttpResponse>, Status> {
        // Extract session ID from path
        let id = req
            .path
            .strip_prefix("/api/pty-glyph/session/")
            .ok_or_else(|| Status::invalid_argument("Invalid path"))?;

        self.pty_manager
            .write()
            .kill_session(id)
            .map_err(|e| Status::internal(format!("Failed to kill session: {}", e)))?;

        Ok(Response::new(HttpResponse {
            status_code: 204,
            headers: vec![],
            body: Vec::new(),
        }))
    }
}
