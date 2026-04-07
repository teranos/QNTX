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
- On startup, async reindex backfills any attestations created before Meilisearch was enabled
- Documents are denormalized: subjects/predicates/contexts/actors space-joined for FTS, attributes flattened
- Search supports filters (source, actors, time range) combined with full-text queries
- `/health` reports Meilisearch status (healthy, document count, indexing state)

## HTTP API

- `GET /api/search/meilisearch?q=...&limit=20&source=...` — full-text search
- `POST /api/search/meilisearch/reindex` — rebuild index from all attestations
- `GET /api/search/meilisearch/stats` — index statistics

## gRPC (SearchService)

Plugins can search via the `SearchService` gRPC endpoint (passed as `search_endpoint` in `InitializeRequest`).

The `SearchServer` struct and `SetService` are wired into `ServicesManager`. The gRPC listener starts after `make proto` generates the protocol types.

## TODO

- [ ] **m1**: Run `make proto` to generate Go code from `search.proto`, then:
  - Remove `//go:build meilisearch` tag from `plugin/grpc/search_server_grpc.go`
  - Embed `protocol.UnimplementedSearchServiceServer` in `SearchServer`
  - Uncomment `SearchEndpoint` in `client.go` `InitializeRequest`
  - Add `startSearchService()` to `ServicesManager.Start()`
- [ ] **m7**: Search glyph (frontend) — search box with faceted results on the canvas. Use the glyph module pattern (`render(glyph, ui)`) with debounced input hitting `GET /api/search/meilisearch`. Faceted sidebar showing source/actor/context breakdowns. Results as attestation cards that can meld into other glyphs.
- [ ] **m8**: WebSocket `"meilisearch_search"` message type for search-as-you-type. Same contract as existing `"rich_search"` but backed by Meilisearch. Enables keystroke-level latency without HTTP round-trips.
- [ ] **m9**: Route existing `"rich_search"` WebSocket messages through Meilisearch. This is the actual replacement — same frontend contract, different backend. Keep WASM fuzzy engine for Ax predicate matching only. Retire `rich_search.go` and `rich_search_qntx.go` (~900 LOC).
- [ ] **m11**: Unit tests for document conversion, filter building; integration tests with live Meilisearch
- [ ] **m12**: Update `crates/qntx-grpc/build.rs` to compile `search.proto` for Rust plugins
