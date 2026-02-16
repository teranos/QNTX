use crate::handlers::{HandlerContext, ReduceState};
use crate::proto::{
    domain_plugin_service_server::DomainPluginService, ConfigSchemaResponse, Empty,
    ExecuteJobRequest, ExecuteJobResponse, HealthResponse, HttpRequest, HttpResponse,
    InitializeRequest, InitializeResponse, MetadataResponse, WebSocketMessage,
};
use parking_lot::RwLock;
use std::collections::HashMap;
use std::pin::Pin;
use std::sync::Arc;
use tokio_stream::Stream;
use tonic::{Request, Response, Status, Streaming};
use tracing::{debug, info, warn};

/// Dimensionality reduction plugin gRPC service.
pub struct ReducePluginService {
    handlers: HandlerContext,
}

impl ReducePluginService {
    pub fn new() -> Self {
        let state = Arc::new(RwLock::new(ReduceState {
            fitted: HashMap::new(),
        }));

        Self {
            handlers: HandlerContext::new(state),
        }
    }
}

impl Default for ReducePluginService {
    fn default() -> Self {
        Self::new()
    }
}

#[tonic::async_trait]
impl DomainPluginService for ReducePluginService {
    async fn metadata(
        &self,
        _request: Request<Empty>,
    ) -> Result<Response<MetadataResponse>, Status> {
        debug!("Metadata request received");
        Ok(Response::new(MetadataResponse {
            name: "reduce".to_string(),
            version: env!("CARGO_PKG_VERSION").to_string(),
            qntx_version: ">=0.1.0".to_string(),
            description:
                "Dimensionality reduction plugin (UMAP, t-SNE, PCA) for embedding visualization"
                    .to_string(),
            author: "QNTX Contributors".to_string(),
            license: "MIT".to_string(),
        }))
    }

    async fn initialize(
        &self,
        _request: Request<InitializeRequest>,
    ) -> Result<Response<InitializeResponse>, Status> {
        info!("Initializing Reduce plugin");

        Ok(Response::new(InitializeResponse {
            handler_names: vec![
                "reduce.umap".to_string(),
                "reduce.tsne".to_string(),
                "reduce.pca".to_string(),
            ],
        }))
    }

    async fn shutdown(&self, _request: Request<Empty>) -> Result<Response<Empty>, Status> {
        info!("Shutting down Reduce plugin");
        self.handlers.clear_models();
        Ok(Response::new(Empty {}))
    }

    async fn handle_http(
        &self,
        request: Request<HttpRequest>,
    ) -> Result<Response<HttpResponse>, Status> {
        let req = request.into_inner();
        let path = &req.path;
        let method = &req.method;

        debug!("HTTP request: {} {}", method, path);

        let body: serde_json::Value = if req.body.is_empty() {
            serde_json::Value::Null
        } else {
            serde_json::from_slice(&req.body)
                .map_err(|e| Status::invalid_argument(format!("Invalid JSON body: {}", e)))?
        };

        let result = match (method.as_str(), path.as_str()) {
            ("POST", "/fit") => self.handlers.handle_fit(body),
            ("POST", "/transform") => self.handlers.handle_transform(body),
            ("GET", "/status") => self.handlers.handle_status(),
            _ => Err(Status::not_found(format!(
                "Unknown endpoint: {} {}",
                method, path
            ))),
        };

        match result {
            Ok(response) => Ok(Response::new(response)),
            Err(status) => {
                let error_body = serde_json::json!({
                    "error": status.message()
                });
                Ok(Response::new(HttpResponse {
                    status_code: match status.code() {
                        tonic::Code::NotFound => 404,
                        tonic::Code::InvalidArgument => 400,
                        tonic::Code::FailedPrecondition => 412,
                        tonic::Code::Internal => 500,
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

    type HandleWebSocketStream =
        Pin<Box<dyn Stream<Item = Result<WebSocketMessage, Status>> + Send>>;

    async fn handle_web_socket(
        &self,
        _request: Request<Streaming<WebSocketMessage>>,
    ) -> Result<Response<Self::HandleWebSocketStream>, Status> {
        warn!("WebSocket not supported by Reduce plugin");
        Err(Status::unimplemented(
            "WebSocket not supported by Reduce plugin",
        ))
    }

    async fn health(&self, _request: Request<Empty>) -> Result<Response<HealthResponse>, Status> {
        let state = self.handlers.state.read();

        let mut details = HashMap::new();
        let fitted_methods: Vec<String> = state.fitted.keys().cloned().collect();
        details.insert("fitted_methods".to_string(), fitted_methods.join(","));
        details.insert("n_methods".to_string(), state.fitted.len().to_string());

        Ok(Response::new(HealthResponse {
            healthy: true,
            message: "OK".to_string(),
            details,
        }))
    }

    async fn config_schema(
        &self,
        _request: Request<Empty>,
    ) -> Result<Response<ConfigSchemaResponse>, Status> {
        // No configurable fields for now
        Ok(Response::new(ConfigSchemaResponse {
            fields: HashMap::new(),
        }))
    }

    async fn execute_job(
        &self,
        _request: Request<ExecuteJobRequest>,
    ) -> Result<Response<ExecuteJobResponse>, Status> {
        Err(Status::unimplemented(
            "Reduce plugin does not support async jobs",
        ))
    }
}
