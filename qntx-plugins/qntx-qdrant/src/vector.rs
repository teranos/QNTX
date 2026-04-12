//! VectorSearchService impl (ADR-016).
//!
//! Backed by Qdrant's `segment::segment_constructor::build_segment` — one
//! `Segment` per named index, all in-process. All operations go through
//! the `Engine` handle.

use crate::proto::{
    vector_search_service_server::VectorSearchService, AddVectorsRequest, AddVectorsResponse,
    CreateIndexRequest, CreateIndexResponse, VectorSearchRequest, VectorSearchResponse,
};
use crate::qdrant::Engine;
use segment::data_types::vectors::DEFAULT_VECTOR_NAME;
use segment::segment::Segment;
use segment::segment_constructor::build_segment;
use segment::types::{
    Distance, Indexes, PayloadStorageType, SegmentConfig, VectorDataConfig, VectorStorageType,
};
use std::collections::HashMap;
use tonic::{Request, Response, Status};

pub struct VectorSearchServiceImpl {
    #[allow(dead_code)] // wired in a follow-up
    engine: Engine,
}

impl VectorSearchServiceImpl {
    pub fn new(engine: Engine) -> Self {
        Self { engine }
    }
}

#[tonic::async_trait]
impl VectorSearchService for VectorSearchServiceImpl {
    async fn search(
        &self,
        _request: Request<VectorSearchRequest>,
    ) -> Result<Response<VectorSearchResponse>, Status> {
        // TODO: translate query_vector + top_k into `SegmentEntry::search`.
        // Skeleton deliberately stubbed until ID mapping (string <-> point id)
        // and scoring conversions are agreed.
        Err(Status::unimplemented(
            "vector search: segment query mapping not wired yet",
        ))
    }

    async fn add_vectors(
        &self,
        _request: Request<AddVectorsRequest>,
    ) -> Result<Response<AddVectorsResponse>, Status> {
        Err(Status::unimplemented(
            "add_vectors: segment upsert mapping not wired yet",
        ))
    }

    async fn create_index(
        &self,
        request: Request<CreateIndexRequest>,
    ) -> Result<Response<CreateIndexResponse>, Status> {
        let req = request.into_inner();
        if req.name.is_empty() {
            return Err(Status::invalid_argument("index name is required"));
        }
        if req.dimensions <= 0 {
            return Err(Status::invalid_argument(format!(
                "dimensions must be positive, got {}",
                req.dimensions
            )));
        }

        let segments_path = self.engine.index_path(&req.name);
        let dim = req.dimensions as usize;
        let engine = self.engine.clone();
        let name = req.name.clone();

        // Segment construction is sync and touches disk — run on a blocking
        // thread so we don't stall the tokio worker.
        let segment: Result<Segment, Status> = tokio::task::spawn_blocking(move || {
            std::fs::create_dir_all(&segments_path).map_err(|e| {
                Status::internal(format!(
                    "failed to create segment dir {}: {}",
                    segments_path.display(),
                    e
                ))
            })?;

            let config = SegmentConfig {
                vector_data: HashMap::from([(
                    DEFAULT_VECTOR_NAME.to_owned(),
                    VectorDataConfig {
                        size: dim,
                        distance: Distance::Cosine,
                        storage_type: VectorStorageType::from_on_disk(false),
                        index: Indexes::Plain {},
                        quantization_config: None,
                        multivector_config: None,
                        datatype: None,
                    },
                )]),
                sparse_vector_data: Default::default(),
                payload_storage_type: PayloadStorageType::from_on_disk_payload(false),
            };

            build_segment(&segments_path, &config, None, true)
                .map_err(|e| Status::internal(format!("build_segment failed: {}", e)))
        })
        .await
        .map_err(|e| Status::internal(format!("segment build task panicked: {}", e)))?;

        let segment = segment?;

        engine
            .create_index(&name, segment)
            .map_err(|e| Status::internal(format!("engine register failed: {}", e)))?;

        Ok(Response::new(CreateIndexResponse { name: req.name }))
    }
}
