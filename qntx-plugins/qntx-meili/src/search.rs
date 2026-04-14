use meilisearch_sdk::client::Client;
use parking_lot::RwLock;
use qntx_grpc::plugin::proto::search_service_server::SearchService;
use qntx_grpc::plugin::proto::{
    DeleteDocumentsRequest, DeleteDocumentsResponse, IndexDocumentsRequest, IndexDocumentsResponse,
    SearchHit, SearchRequest, SearchResponse,
};
use tonic::{Request, Response, Status};
use tracing::{info, warn};

pub struct MeiliSearchService {
    client: RwLock<Option<Client>>,
    url: RwLock<String>,
    index_count: RwLock<usize>,
}

impl MeiliSearchService {
    pub fn new() -> Self {
        Self {
            client: RwLock::new(None),
            url: RwLock::new(String::new()),
            index_count: RwLock::new(0),
        }
    }

    /// (Re)configure the MeiliSearch client with URL and API key.
    /// Called from initialize() with config from am.toml.
    /// Validates connectivity by calling the health endpoint.
    pub async fn configure(&self, url: &str, key: &str) -> Result<(), String> {
        *self.url.write() = url.to_string();
        let client = match Client::new(url, Some(key)) {
            Ok(c) => c,
            Err(e) => {
                warn!("Failed to create MeiliSearch client for {}: {}", url, e);
                *self.client.write() = None;
                return Err(format!("failed to create client for {}: {}", url, e));
            }
        };

        // Validate connectivity and auth by listing indexes (requires valid key)
        match client.list_all_indexes().await {
            Ok(indexes) => {
                let count = indexes.results.len();
                info!("MeiliSearch connected at {} ({} indexes)", url, count);
                *self.index_count.write() = count;
                *self.client.write() = Some(client);
                Ok(())
            }
            Err(e) => {
                warn!(
                    "MeiliSearch at {} auth/connectivity check failed: {}",
                    url, e
                );
                *self.client.write() = None;
                Err(format!("MeiliSearch at {} not accessible: {}", url, e))
            }
        }
    }

    pub fn has_client(&self) -> bool {
        self.client.read().is_some()
    }

    pub fn get_url(&self) -> String {
        self.url.read().clone()
    }

    pub fn get_index_count(&self) -> usize {
        *self.index_count.read()
    }

    #[allow(clippy::result_large_err)] // Status is the standard tonic error type
    fn get_client(&self) -> Result<Client, Status> {
        let url = self.url.read().clone();
        self.client
            .read()
            .clone()
            .ok_or_else(|| Status::unavailable(format!("MeiliSearch not connected to {}", url)))
    }
}

#[tonic::async_trait]
impl SearchService for MeiliSearchService {
    async fn search(
        &self,
        request: Request<SearchRequest>,
    ) -> Result<Response<SearchResponse>, Status> {
        let req = request.into_inner();
        let client = self.get_client()?;

        let index = client.index(&req.index);
        let mut query = index.search();
        query.with_query(&req.query);

        if req.top_k > 0 {
            query.with_limit(req.top_k as usize);
        }

        // TODO: apply filters from req.filters (JSON bytes)

        let start = std::time::Instant::now();
        let results = query
            .execute::<serde_json::Value>()
            .await
            .map_err(|e| Status::internal(format!("MeiliSearch query failed: {}", e)))?;

        let processing_ms = start.elapsed().as_millis() as i32;

        let hits: Vec<SearchHit> = results
            .hits
            .into_iter()
            .map(|hit| {
                let doc_bytes = serde_json::to_vec(&hit.result).unwrap_or_default();
                SearchHit {
                    id: hit
                        .result
                        .get("id")
                        .and_then(|v| v.as_str())
                        .unwrap_or("")
                        .to_string(),
                    score: 0.0, // MeiliSearch doesn't expose relevance scores by default
                    document: doc_bytes,
                }
            })
            .collect();

        let total = results.estimated_total_hits.unwrap_or(hits.len());

        Ok(Response::new(SearchResponse {
            hits,
            total: total as i32,
            processing_ms,
        }))
    }

    async fn index_documents(
        &self,
        request: Request<IndexDocumentsRequest>,
    ) -> Result<Response<IndexDocumentsResponse>, Status> {
        let req = request.into_inner();
        let client = self.get_client()?;

        let index = client.index(&req.index);

        let documents: Vec<serde_json::Value> = req
            .documents
            .iter()
            .filter_map(|doc| serde_json::from_slice(doc).ok())
            .collect();

        let accepted = documents.len() as i32;

        index
            .add_documents(&documents, Some("id"))
            .await
            .map_err(|e| Status::internal(format!("MeiliSearch indexing failed: {}", e)))?;

        Ok(Response::new(IndexDocumentsResponse { accepted }))
    }

    async fn delete_documents(
        &self,
        request: Request<DeleteDocumentsRequest>,
    ) -> Result<Response<DeleteDocumentsResponse>, Status> {
        let req = request.into_inner();
        let client = self.get_client()?;

        let index = client.index(&req.index);
        let count = req.ids.len() as i32;

        index
            .delete_documents(&req.ids)
            .await
            .map_err(|e| Status::internal(format!("MeiliSearch delete failed: {}", e)))?;

        Ok(Response::new(DeleteDocumentsResponse { deleted: count }))
    }
}
