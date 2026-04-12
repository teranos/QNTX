//! VectorSearchService impl (ADR-016).
//!
//! Qdrant's native unit of storage is a point — vector + payload — so the
//! same collection that backs SearchService documents also backs
//! VectorSearchService neighbors. That co-location is the point of ADR-017:
//! hybrid (BM25 + dense) ranking stays inside the engine.

use crate::proto::{
    vector_search_service_server::VectorSearchService, AddVectorsRequest, AddVectorsResponse,
    CreateIndexRequest, CreateIndexResponse, VectorSearchRequest, VectorSearchResponse,
};
use crate::qdrant::Supervisor;
use tonic::{Request, Response, Status};

pub struct VectorSearchServiceImpl {
    #[allow(dead_code)] // wired in a follow-up
    supervisor: Supervisor,
}

impl VectorSearchServiceImpl {
    pub fn new(supervisor: Supervisor) -> Self {
        Self { supervisor }
    }
}

#[tonic::async_trait]
impl VectorSearchService for VectorSearchServiceImpl {
    async fn search(
        &self,
        _request: Request<VectorSearchRequest>,
    ) -> Result<Response<VectorSearchResponse>, Status> {
        // TODO: qdrant_client::Qdrant::search_points with query_vector + top_k,
        // map returned ScoredPoint to VectorSearchHit { id, distance }.
        Err(Status::unimplemented(
            "vector search: qdrant mapping not wired yet (scaffold)",
        ))
    }

    async fn add_vectors(
        &self,
        _request: Request<AddVectorsRequest>,
    ) -> Result<Response<AddVectorsResponse>, Status> {
        // TODO: upsert points (id, vector) into the named collection.
        Err(Status::unimplemented(
            "add_vectors: qdrant mapping not wired yet (scaffold)",
        ))
    }

    async fn create_index(
        &self,
        _request: Request<CreateIndexRequest>,
    ) -> Result<Response<CreateIndexResponse>, Status> {
        // TODO: create_collection with named dense-vector config of given
        // dimensions, and — on the same collection — a sparse-vector config
        // so SearchService hybrid queries work without a second CreateIndex.
        Err(Status::unimplemented(
            "create_index: qdrant mapping not wired yet (scaffold)",
        ))
    }
}
