//! SearchService impl (ADR-015).
//!
//! Single `Segment` per index isn't the natural fit for full-text search —
//! Qdrant's keyword/BM25 story rides on payload text indexes and sparse
//! vectors, which this plugin can wire up but hasn't yet. Stubbed until the
//! index layout is decided.

use crate::proto::{
    search_service_server::SearchService, DeleteDocumentsRequest, DeleteDocumentsResponse,
    IndexDocumentsRequest, IndexDocumentsResponse, SearchRequest, SearchResponse,
};
use crate::qdrant::Engine;
use tonic::{Request, Response, Status};

pub struct SearchServiceImpl {
    #[allow(dead_code)] // wired in a follow-up
    engine: Engine,
}

impl SearchServiceImpl {
    pub fn new(engine: Engine) -> Self {
        Self { engine }
    }
}

#[tonic::async_trait]
impl SearchService for SearchServiceImpl {
    async fn search(
        &self,
        _request: Request<SearchRequest>,
    ) -> Result<Response<SearchResponse>, Status> {
        Err(Status::unimplemented(
            "search: text-over-payload / sparse-vector mapping not wired yet",
        ))
    }

    async fn index_documents(
        &self,
        _request: Request<IndexDocumentsRequest>,
    ) -> Result<Response<IndexDocumentsResponse>, Status> {
        Err(Status::unimplemented(
            "index_documents: payload-schema inference not wired yet",
        ))
    }

    async fn delete_documents(
        &self,
        _request: Request<DeleteDocumentsRequest>,
    ) -> Result<Response<DeleteDocumentsResponse>, Status> {
        Err(Status::unimplemented(
            "delete_documents: not wired yet",
        ))
    }
}
