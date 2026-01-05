//! gRPC service implementation for FuzzyMatchService

use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;
use std::time::Instant;

use tonic::{Request, Response, Status};
use tracing::{info, warn};

use crate::engine::{FuzzyEngine, VocabularyType as EngineVocabType};
use crate::proto::fuzzy_match_service_server::FuzzyMatchService;
use crate::proto::{
    BatchMatchRequest, BatchMatchResponse, FindMatchesRequest, FindMatchesResponse, HealthRequest,
    HealthResponse, RankedMatch, RebuildIndexRequest, RebuildIndexResponse, StatsRequest,
    StatsResponse, VocabularyType,
};

/// gRPC service wrapping FuzzyEngine
pub struct FuzzyService {
    engine: Arc<FuzzyEngine>,
    start_time: Instant,
    queries_served: AtomicU64,
    total_query_time_us: AtomicU64,
}

impl FuzzyService {
    pub fn new(engine: Arc<FuzzyEngine>) -> Self {
        Self {
            engine,
            start_time: Instant::now(),
            queries_served: AtomicU64::new(0),
            total_query_time_us: AtomicU64::new(0),
        }
    }

    fn record_query(&self, time_us: u64) {
        self.queries_served.fetch_add(1, Ordering::Relaxed);
        self.total_query_time_us.fetch_add(time_us, Ordering::Relaxed);
    }

    fn convert_vocab_type(vt: i32) -> EngineVocabType {
        match VocabularyType::try_from(vt) {
            Ok(VocabularyType::Contexts) => EngineVocabType::Contexts,
            _ => EngineVocabType::Predicates,
        }
    }
}

#[tonic::async_trait]
impl FuzzyMatchService for FuzzyService {
    async fn rebuild_index(
        &self,
        request: Request<RebuildIndexRequest>,
    ) -> Result<Response<RebuildIndexResponse>, Status> {
        let req = request.into_inner();

        info!(
            predicates = req.predicates.len(),
            contexts = req.contexts.len(),
            "Rebuilding fuzzy index"
        );

        let (pred_count, ctx_count, build_time, hash) =
            self.engine.rebuild_index(req.predicates, req.contexts);

        info!(
            predicate_count = pred_count,
            context_count = ctx_count,
            build_time_ms = build_time,
            hash = %hash,
            "Index rebuilt successfully"
        );

        Ok(Response::new(RebuildIndexResponse {
            predicate_count: pred_count as i64,
            context_count: ctx_count as i64,
            build_time_ms: build_time as i64,
            index_hash: hash,
        }))
    }

    async fn find_matches(
        &self,
        request: Request<FindMatchesRequest>,
    ) -> Result<Response<FindMatchesResponse>, Status> {
        let req = request.into_inner();

        if req.query.trim().is_empty() {
            return Ok(Response::new(FindMatchesResponse {
                matches: vec![],
                search_time_us: 0,
            }));
        }

        let vocab_type = Self::convert_vocab_type(req.vocabulary_type);
        let limit = if req.limit > 0 {
            Some(req.limit as usize)
        } else {
            None
        };
        let min_score = if req.min_score > 0.0 {
            Some(req.min_score)
        } else {
            None
        };

        let (matches, search_time) =
            self.engine
                .find_matches(&req.query, vocab_type, limit, min_score);

        self.record_query(search_time);

        let proto_matches: Vec<RankedMatch> = matches
            .into_iter()
            .map(|m| RankedMatch {
                value: m.value,
                score: m.score,
                strategy: if req.include_strategy {
                    m.strategy.to_string()
                } else {
                    String::new()
                },
            })
            .collect();

        Ok(Response::new(FindMatchesResponse {
            matches: proto_matches,
            search_time_us: search_time as i64,
        }))
    }

    async fn batch_match(
        &self,
        request: Request<BatchMatchRequest>,
    ) -> Result<Response<BatchMatchResponse>, Status> {
        let req = request.into_inner();
        let start = Instant::now();

        if req.queries.is_empty() {
            return Ok(Response::new(BatchMatchResponse {
                results: vec![],
                total_time_us: 0,
            }));
        }

        let mut results = Vec::with_capacity(req.queries.len());

        for query_req in req.queries {
            let vocab_type = Self::convert_vocab_type(query_req.vocabulary_type);
            let limit = if query_req.limit > 0 {
                Some(query_req.limit as usize)
            } else {
                None
            };
            let min_score = if query_req.min_score > 0.0 {
                Some(query_req.min_score)
            } else {
                None
            };

            let (matches, search_time) =
                self.engine
                    .find_matches(&query_req.query, vocab_type, limit, min_score);

            let proto_matches: Vec<RankedMatch> = matches
                .into_iter()
                .map(|m| RankedMatch {
                    value: m.value,
                    score: m.score,
                    strategy: if query_req.include_strategy {
                        m.strategy.to_string()
                    } else {
                        String::new()
                    },
                })
                .collect();

            results.push(FindMatchesResponse {
                matches: proto_matches,
                search_time_us: search_time as i64,
            });
        }

        let total_time = start.elapsed().as_micros() as u64;
        self.record_query(total_time);

        Ok(Response::new(BatchMatchResponse {
            results,
            total_time_us: total_time as i64,
        }))
    }

    async fn get_stats(
        &self,
        _request: Request<StatsRequest>,
    ) -> Result<Response<StatsResponse>, Status> {
        let (pred_count, ctx_count) = self.engine.get_counts();
        let queries = self.queries_served.load(Ordering::Relaxed);
        let total_time = self.total_query_time_us.load(Ordering::Relaxed);

        let avg_time = if queries > 0 {
            total_time / queries
        } else {
            0
        };

        Ok(Response::new(StatsResponse {
            predicate_count: pred_count as i64,
            context_count: ctx_count as i64,
            index_hash: self.engine.get_index_hash(),
            queries_served: queries as i64,
            avg_query_time_us: avg_time as i64,
            uptime_seconds: self.start_time.elapsed().as_secs() as i64,
        }))
    }

    async fn health(
        &self,
        _request: Request<HealthRequest>,
    ) -> Result<Response<HealthResponse>, Status> {
        let is_ready = self.engine.is_ready();

        Ok(Response::new(HealthResponse {
            healthy: true,
            message: if is_ready {
                "Fuzzy matching service ready".to_string()
            } else {
                "Service running, awaiting index build".to_string()
            },
            index_ready: is_ready,
        }))
    }
}
