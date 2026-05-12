use crate::search::MeiliSearchService;
use qntx_grpc::plugin::proto::domain_plugin_service_server::DomainPluginService;
use qntx_grpc::plugin::proto::{
    ConfigSchemaResponse, Empty, ExecuteJobRequest, ExecuteJobResponse, GlyphDefResponse,
    HealthResponse, HttpRequest, HttpResponse, InitializeRequest, InitializeResponse,
    MetadataResponse, ParseAxQueryRequest, ParseAxQueryResponse, WebSocketMessage,
};
use std::sync::Arc;
use tokio_stream::wrappers::ReceiverStream;
use tonic::{Request, Response, Status};
use tracing::{info, warn};

pub struct MeiliPluginService {
    search: Arc<MeiliSearchService>,
    default_url: String,
    default_key: String,
}

impl MeiliPluginService {
    pub fn new(search: Arc<MeiliSearchService>, default_url: String, default_key: String) -> Self {
        Self {
            search,
            default_url,
            default_key,
        }
    }
}

#[tonic::async_trait]
impl DomainPluginService for MeiliPluginService {
    async fn metadata(
        &self,
        _request: Request<Empty>,
    ) -> Result<Response<MetadataResponse>, Status> {
        Ok(Response::new(MetadataResponse {
            name: "meili".to_string(),
            version: env!("CARGO_PKG_VERSION").to_string(),
            qntx_version: ">=0.1.0".to_string(),
            description: "Search provider plugin — MeiliSearch backend".to_string(),
            author: "QNTX Contributors".to_string(),
            license: "MIT".to_string(),
        }))
    }

    async fn initialize(
        &self,
        request: Request<InitializeRequest>,
    ) -> Result<Response<InitializeResponse>, Status> {
        let req = request.into_inner();

        let embedded = req
            .config
            .get("embedded")
            .map(|v| v == "true")
            .unwrap_or(false);

        if embedded {
            // Embedded mode: spawn a local MeiliSearch subprocess
            let binary = req
                .config
                .get("meili_bin")
                .filter(|v| !v.is_empty())
                .cloned()
                .unwrap_or_else(|| "meilisearch".to_string());

            let db_path = req
                .config
                .get("meili_db_path")
                .filter(|v| !v.is_empty())
                .map(std::path::PathBuf::from)
                .unwrap_or_else(|| {
                    let home = std::env::var("HOME").unwrap_or_else(|_| ".".to_string());
                    std::path::PathBuf::from(home).join(".qntx/meili-data")
                });

            info!("Initializing qntx-meili (embedded, db: {})", db_path.display());

            if let Err(e) = self.search.start_embedded(&binary, db_path).await {
                warn!("Embedded MeiliSearch failed to start: {}", e);
            }
        } else {
            // Remote mode: connect to an external MeiliSearch instance
            let url = req
                .config
                .get("url")
                .filter(|v| !v.is_empty())
                .cloned()
                .unwrap_or_else(|| self.default_url.clone());
            let key = req
                .config
                .get("key")
                .filter(|v| !v.is_empty())
                .cloned()
                .unwrap_or_else(|| self.default_key.clone());

            info!("Initializing qntx-meili (remote: {})", url);

            if let Err(e) = self.search.configure(&url, &key).await {
                warn!("MeiliSearch not available: {}", e);
            }
        }

        Ok(Response::new(InitializeResponse {
            handler_names: vec![],
            schedules: vec![],
            llm_provider: false,
            search_provider: true,
            ..Default::default()
        }))
    }

    async fn handle_http(
        &self,
        _request: Request<HttpRequest>,
    ) -> Result<Response<HttpResponse>, Status> {
        Ok(Response::new(HttpResponse {
            status_code: 404,
            body: b"not found".to_vec(),
            ..Default::default()
        }))
    }

    type HandleWebSocketStream = ReceiverStream<Result<WebSocketMessage, Status>>;

    async fn handle_web_socket(
        &self,
        _request: Request<tonic::Streaming<WebSocketMessage>>,
    ) -> Result<Response<Self::HandleWebSocketStream>, Status> {
        Err(Status::unimplemented("WebSocket not supported"))
    }

    async fn health(&self, _request: Request<Empty>) -> Result<Response<HealthResponse>, Status> {
        let has_client = self.search.has_client();
        let url = self.search.get_url();
        let mode = self.search.get_mode();
        let mut details = std::collections::HashMap::new();

        if has_client {
            details.insert("backend".into(), format!("MeiliSearch at {} ({})", url, mode));
            details.insert("indexes".into(), self.search.get_index_count().to_string());
        }

        Ok(Response::new(HealthResponse {
            healthy: has_client,
            message: if has_client {
                format!("MeiliSearch at {} ({})", url, mode)
            } else {
                format!("MeiliSearch at {} not accessible", url)
            },
            details,
        }))
    }

    async fn execute_job(
        &self,
        _request: Request<ExecuteJobRequest>,
    ) -> Result<Response<ExecuteJobResponse>, Status> {
        Err(Status::unimplemented("No async jobs"))
    }

    async fn shutdown(&self, _request: Request<Empty>) -> Result<Response<Empty>, Status> {
        info!("qntx-meili shutting down");
        Ok(Response::new(Empty {}))
    }

    async fn config_schema(
        &self,
        _request: Request<Empty>,
    ) -> Result<Response<ConfigSchemaResponse>, Status> {
        Ok(Response::new(ConfigSchemaResponse {
            fields: Default::default(),
        }))
    }

    async fn register_glyphs(
        &self,
        _request: Request<Empty>,
    ) -> Result<Response<GlyphDefResponse>, Status> {
        Ok(Response::new(GlyphDefResponse { glyphs: vec![] }))
    }

    async fn parse_ax_query(
        &self,
        _request: Request<ParseAxQueryRequest>,
    ) -> Result<Response<ParseAxQueryResponse>, Status> {
        Err(Status::unimplemented("No Ax query parsing"))
    }
}
