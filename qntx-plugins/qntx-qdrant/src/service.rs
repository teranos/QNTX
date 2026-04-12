//! DomainPluginService impl — the plugin-host contract (metadata, init,
//! shutdown, health). Search and VectorSearch RPCs live in their own modules.

use crate::proto::{
    domain_plugin_service_server::DomainPluginService, ConfigSchemaResponse, Empty,
    ExecuteJobRequest, ExecuteJobResponse, GlyphDefResponse, HealthResponse, HttpRequest,
    HttpResponse, InitializeRequest, InitializeResponse, MetadataResponse, ParseAxQueryRequest,
    ParseAxQueryResponse, WebSocketMessage,
};
use crate::qdrant::Supervisor;
use std::collections::HashMap;
use std::pin::Pin;
use tokio_stream::Stream;
use tonic::{Request, Response, Status, Streaming};
use tracing::{info, warn};

pub struct QdrantPluginService {
    supervisor: Supervisor,
}

impl QdrantPluginService {
    pub fn new(supervisor: Supervisor) -> Self {
        Self { supervisor }
    }
}

#[tonic::async_trait]
impl DomainPluginService for QdrantPluginService {
    async fn metadata(
        &self,
        _request: Request<Empty>,
    ) -> Result<Response<MetadataResponse>, Status> {
        Ok(Response::new(MetadataResponse {
            name: "qdrant".to_string(),
            version: env!("CARGO_PKG_VERSION").to_string(),
            qntx_version: ">=0.1.0".to_string(),
            description: "Plugin-managed Qdrant providing SearchService (ADR-015) and \
                VectorSearchService (ADR-016) from a single process (ADR-017)"
                .to_string(),
            author: "QNTX Contributors".to_string(),
            license: "MIT".to_string(),
        }))
    }

    async fn initialize(
        &self,
        _request: Request<InitializeRequest>,
    ) -> Result<Response<InitializeResponse>, Status> {
        info!("initializing qntx-qdrant plugin");

        // TODO (merge coordination): once branches `search-service` and
        // `vector-search-service` land, set the provider flags on
        // InitializeResponse so core registers this plugin as the backend
        // for both services. Field numbers need reconciling across the two
        // branches before those markers exist on main.

        Ok(Response::new(InitializeResponse {
            handler_names: vec![],
            schedules: vec![],
            watchers: vec![],
            llm_provider: false,
            // TODO (merge coordination): once branches `search-service` and
            // `vector-search-service` land, set `search_provider = true` and
            // `vector_search_provider = true` so core registers this plugin
            // as the backend for both services via ServiceRegistry.
        }))
    }

    async fn register_glyphs(
        &self,
        _request: Request<Empty>,
    ) -> Result<Response<GlyphDefResponse>, Status> {
        // Panel glyph for Qdrant (collections, point counts, health) —
        // deferred to a follow-up change.
        Ok(Response::new(GlyphDefResponse { glyphs: vec![] }))
    }

    async fn shutdown(&self, _request: Request<Empty>) -> Result<Response<Empty>, Status> {
        info!("shutting down qntx-qdrant plugin");
        self.supervisor.shutdown().await;
        Ok(Response::new(Empty {}))
    }

    async fn handle_http(
        &self,
        _request: Request<HttpRequest>,
    ) -> Result<Response<HttpResponse>, Status> {
        Err(Status::unimplemented(
            "qntx-qdrant exposes no HTTP surface — use SearchService / VectorSearchService",
        ))
    }

    type HandleWebSocketStream =
        Pin<Box<dyn Stream<Item = Result<WebSocketMessage, Status>> + Send>>;

    async fn handle_web_socket(
        &self,
        _request: Request<Streaming<WebSocketMessage>>,
    ) -> Result<Response<Self::HandleWebSocketStream>, Status> {
        warn!("websocket not supported by qntx-qdrant");
        Err(Status::unimplemented(
            "websocket not supported by qntx-qdrant",
        ))
    }

    async fn health(&self, _request: Request<Empty>) -> Result<Response<HealthResponse>, Status> {
        // TODO: call supervisor.client()?.health_check() once service layer is
        // wired. For the scaffold, report liveness of the plugin process only.
        let mut details = HashMap::new();
        details.insert(
            "managed_qdrant_addr".to_string(),
            self.supervisor.endpoint().addr.to_string(),
        );
        details.insert(
            "managed_qdrant_data_dir".to_string(),
            self.supervisor.endpoint().data_dir.display().to_string(),
        );

        Ok(Response::new(HealthResponse {
            healthy: true,
            message: "plugin alive; qdrant readiness check not yet wired".to_string(),
            details,
        }))
    }

    async fn config_schema(
        &self,
        _request: Request<Empty>,
    ) -> Result<Response<ConfigSchemaResponse>, Status> {
        // No user-facing config: the plugin is self-contained (ADR-017).
        Ok(Response::new(ConfigSchemaResponse {
            fields: HashMap::new(),
        }))
    }

    async fn parse_ax_query(
        &self,
        _request: Request<ParseAxQueryRequest>,
    ) -> Result<Response<ParseAxQueryResponse>, Status> {
        Err(Status::unimplemented("ParseAxQuery is handled by kern"))
    }

    async fn execute_job(
        &self,
        _request: Request<ExecuteJobRequest>,
    ) -> Result<Response<ExecuteJobResponse>, Status> {
        Err(Status::unimplemented(
            "qntx-qdrant does not run pulse jobs — call SearchService / VectorSearchService",
        ))
    }
}
