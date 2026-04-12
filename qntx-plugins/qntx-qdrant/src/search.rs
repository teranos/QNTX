//! SearchService impl (ADR-015).
//!
//! Maps the engine-agnostic proto to Qdrant operations:
//!   * `Search`           → Qdrant full-text / sparse-vector search with
//!                          `SearchRequest.filters` as payload filter
//!   * `IndexDocuments`   → upsert JSON docs as points with payload
//!   * `DeleteDocuments`  → delete points by id
//!
//! This module intentionally returns `Unimplemented` until the Qdrant
//! collection layout is decided (collection-per-index, payload-schema
//! inference rules, tokenizer config). Those decisions belong in a follow-up;
//! the service skeleton is what this change establishes.

use crate::proto::{
    search_service_server::SearchService, DeleteDocumentsRequest, DeleteDocumentsResponse,
    IndexDocumentsRequest, IndexDocumentsResponse, SearchRequest, SearchResponse,
};
use crate::qdrant::Supervisor;
use tonic::{Request, Response, Status};

pub struct SearchServiceImpl {
    #[allow(dead_code)] // wired in a follow-up
    supervisor: Supervisor,
}

impl SearchServiceImpl {
    pub fn new(supervisor: Supervisor) -> Self {
        Self { supervisor }
    }
}

#[tonic::async_trait]
impl SearchService for SearchServiceImpl {
    async fn search(
        &self,
        _request: Request<SearchRequest>,
    ) -> Result<Response<SearchResponse>, Status> {
        // TODO: translate SearchRequest → qdrant search with payload filter.
        // SearchRequest.filters (JSON bytes) maps to a Qdrant Filter via
        // a small deserializer — the mapping is small but deliberate, so
        // it lives in a follow-up change rather than a stub.
        Err(Status::unimplemented(
            "search: qdrant mapping not wired yet (scaffold)",
        ))
    }

    async fn index_documents(
        &self,
        _request: Request<IndexDocumentsRequest>,
    ) -> Result<Response<IndexDocumentsResponse>, Status> {
        // TODO: upsert documents as points in the named collection.
        // Collection is created implicitly on first use; payload schema is
        // inferred from the first batch (same contract as ADR-015).
        Err(Status::unimplemented(
            "index_documents: qdrant mapping not wired yet (scaffold)",
        ))
    }

    async fn delete_documents(
        &self,
        _request: Request<DeleteDocumentsRequest>,
    ) -> Result<Response<DeleteDocumentsResponse>, Status> {
        Err(Status::unimplemented(
            "delete_documents: qdrant mapping not wired yet (scaffold)",
        ))
    }
}
