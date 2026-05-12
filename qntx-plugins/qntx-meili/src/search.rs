use crate::embedded::EmbeddedMeili;
use meilisearch_sdk::client::Client;
use parking_lot::RwLock;
use qntx_grpc::plugin::proto::search_service_server::SearchService;
use qntx_grpc::plugin::proto::{
    ConfigureIndexRequest, ConfigureIndexResponse, DeleteDocumentsRequest, DeleteDocumentsResponse,
    IndexDocumentsRequest, IndexDocumentsResponse, SearchHit, SearchRequest, SearchResponse,
};
use tonic::{Request, Response, Status};
use tracing::{info, warn};

pub struct MeiliSearchService {
    client: RwLock<Option<Client>>,
    url: RwLock<String>,
    index_count: RwLock<usize>,
    mode: RwLock<String>,
    /// Holds the embedded MeiliSearch subprocess (if running in embedded mode).
    /// Dropping this kills the child process.
    _embedded: RwLock<Option<EmbeddedMeili>>,
}

impl MeiliSearchService {
    pub fn new() -> Self {
        Self {
            client: RwLock::new(None),
            url: RwLock::new(String::new()),
            index_count: RwLock::new(0),
            mode: RwLock::new("remote".to_string()),
            _embedded: RwLock::new(None),
        }
    }

    /// (Re)configure the MeiliSearch client with URL and API key.
    /// Called from initialize() with config from am.toml.
    /// For remote mode, validates connectivity by listing indexes.
    /// For embedded mode, skips validation (wait_for_ready already confirmed the process).
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

        let is_embedded = *self.mode.read() == "embedded";

        if is_embedded {
            // Embedded mode: trust that wait_for_ready confirmed the process.
            // list_all_indexes can block for seconds while MeiliSearch initializes LMDB.
            info!("MeiliSearch client configured for embedded instance at {}", url);
            *self.client.write() = Some(client);
            Ok(())
        } else {
            // Remote mode: validate connectivity and auth
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

    pub fn get_mode(&self) -> String {
        self.mode.read().clone()
    }

    pub fn set_mode(&self, mode: &str) {
        *self.mode.write() = mode.to_string();
    }

    /// Start an embedded MeiliSearch subprocess and configure the client to use it.
    pub async fn start_embedded(&self, binary: &str, db_path: std::path::PathBuf) -> Result<(), String> {
        let handle = EmbeddedMeili::spawn(binary, db_path).await?;
        let url = handle.url();
        let key = handle.key().to_string();
        *self._embedded.write() = Some(handle);
        self.set_mode("embedded");
        self.configure(&url, &key).await
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

        // Apply filters from req.filters (JSON bytes — expect a string filter expression)
        let filter_str = if !req.filters.is_empty() {
            match serde_json::from_slice::<String>(&req.filters) {
                Ok(f) if !f.is_empty() => Some(f),
                _ => {
                    // Try raw UTF-8 string (non-JSON-quoted filter)
                    match std::str::from_utf8(&req.filters) {
                        Ok(s) if !s.is_empty() => Some(s.to_string()),
                        _ => None,
                    }
                }
            }
        } else {
            None
        };
        if let Some(ref f) = filter_str {
            query.with_filter(f);
        }

        // Apply facets
        let facet_refs: Vec<&str> = req.facets.iter().map(|s| s.as_str()).collect();
        if !facet_refs.is_empty() {
            query.with_facets(meilisearch_sdk::search::Selectors::Some(&facet_refs));
        }

        // Request highlights on all attributes
        query.with_attributes_to_highlight(meilisearch_sdk::search::Selectors::All);
        query.with_highlight_pre_tag("<em>");
        query.with_highlight_post_tag("</em>");

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
                let highlighted = hit
                    .formatted_result
                    .as_ref()
                    .and_then(|v| serde_json::to_vec(v).ok())
                    .unwrap_or_default();
                SearchHit {
                    id: hit
                        .result
                        .get("id")
                        .and_then(|v| v.as_str())
                        .unwrap_or("")
                        .to_string(),
                    score: 0.0, // MeiliSearch doesn't expose relevance scores by default
                    document: doc_bytes,
                    highlighted,
                }
            })
            .collect();

        let total = results.estimated_total_hits.unwrap_or(hits.len());

        let facet_distribution = results
            .facet_distribution
            .as_ref()
            .and_then(|fd| serde_json::to_vec(fd).ok())
            .unwrap_or_default();

        Ok(Response::new(SearchResponse {
            hits,
            total: total as i32,
            processing_ms,
            facet_distribution,
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

    async fn configure_index(
        &self,
        request: Request<ConfigureIndexRequest>,
    ) -> Result<Response<ConfigureIndexResponse>, Status> {
        let req = request.into_inner();
        let client = self.get_client()?;

        // Create the index with the specified primary key
        let pk = if req.primary_key.is_empty() {
            None
        } else {
            Some(req.primary_key.as_str())
        };
        // create_index is idempotent — returns TaskInfo even if index exists
        let _ = client
            .create_index(&req.index, pk)
            .await
            .map_err(|e| Status::internal(format!("create index '{}' failed: {}", req.index, e)))?;

        let index = client.index(&req.index);

        // Set filterable attributes
        if !req.filterable_attributes.is_empty() {
            let attrs: Vec<&str> = req
                .filterable_attributes
                .iter()
                .map(|s| s.as_str())
                .collect();
            index.set_filterable_attributes(&attrs).await.map_err(|e| {
                Status::internal(format!(
                    "set filterable attributes on '{}' failed: {}",
                    req.index, e
                ))
            })?;
        }

        // Set sortable attributes
        if !req.sortable_attributes.is_empty() {
            let attrs: Vec<&str> = req.sortable_attributes.iter().map(|s| s.as_str()).collect();
            index.set_sortable_attributes(&attrs).await.map_err(|e| {
                Status::internal(format!(
                    "set sortable attributes on '{}' failed: {}",
                    req.index, e
                ))
            })?;
        }

        // Set searchable attributes
        if !req.searchable_attributes.is_empty() {
            let attrs: Vec<&str> = req
                .searchable_attributes
                .iter()
                .map(|s| s.as_str())
                .collect();
            index.set_searchable_attributes(&attrs).await.map_err(|e| {
                Status::internal(format!(
                    "set searchable attributes on '{}' failed: {}",
                    req.index, e
                ))
            })?;
        }

        info!(
            "Configured index '{}' (pk={}, filterable={}, sortable={}, searchable={})",
            req.index,
            pk.unwrap_or("auto"),
            req.filterable_attributes.len(),
            req.sortable_attributes.len(),
            req.searchable_attributes.len(),
        );

        Ok(Response::new(ConfigureIndexResponse { accepted: true }))
    }
}
