# search — Meilisearch full-text search

In-process Go service wrapping the Meilisearch HTTP API for full-text search over attestations.

## Setup

1. Install and run Meilisearch (see https://www.meilisearch.com/docs/learn/getting_started/installation)
2. Configure in `am.toml`:

```toml
[meilisearch]
enabled = true
url = "http://localhost:7700"
api_key = "your-master-key"
```

Or via environment variable: `QNTX_MEILISEARCH_API_KEY=your-master-key`

## How it works

- Every attestation write triggers immediate Meilisearch indexing via the `AttestationObserver` pattern (zero lag)
- Documents are denormalized: subjects/predicates/contexts/actors space-joined for FTS, attributes flattened
- Search supports filters (source, actors, time range) combined with full-text queries

## HTTP API

- `GET /api/search/meilisearch?q=...&limit=20&source=...` — full-text search
- `POST /api/search/meilisearch/reindex` — rebuild index from all attestations
- `GET /api/search/meilisearch/stats` — index statistics

## gRPC (SearchService)

Plugins can search via the `SearchService` gRPC endpoint (passed as `search_endpoint` in `InitializeRequest`).

## TODO

- [ ] **m1**: Run `make proto` to generate Go code from `search.proto`, then remove `//go:build meilisearch` tag from `plugin/grpc/search_server.go`
- [ ] **m3**: Wire SearchService into `ServicesManager.Start()` and pass `search_endpoint` to plugins via `InitializeRequest`
- [ ] **m4**: Initial reindex on startup (async) to backfill attestations created before Meilisearch was enabled
- [ ] **m5**: Retry buffer or health degradation when Meilisearch is unreachable
- [ ] **m6**: Expose Meilisearch status in `/health` response
- [ ] **m7**: Search glyph (frontend) — search box with faceted results on the canvas
- [ ] **m8**: WebSocket `"meilisearch_search"` message type for search-as-you-type
- [ ] **m9**: Route existing `"rich_search"` WebSocket messages through Meilisearch
- [ ] **m10**: Auto-manage Meilisearch binary (download + lifecycle)
- [ ] **m11**: Unit tests for document conversion, filter building; integration tests with live Meilisearch
- [ ] **m12**: Update `crates/qntx-grpc/build.rs` to compile `search.proto` for Rust plugins
