//! DomainPluginService impl — the plugin-host contract (metadata, init,
//! shutdown, health). Search and VectorSearch RPCs live in their own modules.

use crate::proto::{
    domain_plugin_service_server::DomainPluginService, ConfigSchemaResponse, Empty,
    ExecuteJobRequest, ExecuteJobResponse, GlyphDefResponse, HealthResponse, HttpRequest,
    HttpResponse, InitializeRequest, InitializeResponse, MetadataResponse, ParseAxQueryRequest,
    ParseAxQueryResponse, WebSocketMessage,
};
use crate::qdrant::Engine;
use std::collections::HashMap;
use std::pin::Pin;
use tokio_stream::Stream;
use tonic::{Request, Response, Status, Streaming};
use tracing::{info, warn};

pub struct QdrantPluginService {
    engine: Engine,
}

impl QdrantPluginService {
    pub fn new(engine: Engine) -> Self {
        Self { engine }
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
            description: "Qdrant engine linked in-process; provides SearchService \
                (ADR-015) and VectorSearchService (ADR-016) from a single plugin \
                (ADR-017)"
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

        // vector_search_provider: true — core registers this plugin as the
        // VectorSearchService backend (ADR-016 landed on main via PR #774).
        //
        // search_provider (ADR-015) is still on the search-service branch;
        // add it here once that merges. Same plugin, both flags.

        Ok(Response::new(InitializeResponse {
            handler_names: vec![],
            schedules: vec![],
            watchers: vec![],
            llm_provider: false,
            vector_search_provider: true,
        }))
    }

    async fn register_glyphs(
        &self,
        _request: Request<Empty>,
    ) -> Result<Response<GlyphDefResponse>, Status> {
        Ok(Response::new(GlyphDefResponse { glyphs: vec![] }))
    }

    async fn shutdown(&self, _request: Request<Empty>) -> Result<Response<Empty>, Status> {
        info!("shutting down qntx-qdrant plugin");
        // Engine holds segments behind Arc; they close on drop when the
        // last reference goes away. Explicit flush/close is a TODO.
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
        let mut details = HashMap::new();
        details.insert(
            "state_dir".to_string(),
            self.engine.state_dir().display().to_string(),
        );
        Ok(Response::new(HealthResponse {
            healthy: true,
            message: "engine alive (in-process)".to_string(),
            details,
        }))
    }

    async fn config_schema(
        &self,
        _request: Request<Empty>,
    ) -> Result<Response<ConfigSchemaResponse>, Status> {
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
