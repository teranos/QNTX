# ADR-017: Qdrant as a Single Plugin Providing Both Search Services

## Status
Proposed

## Context

ADR-015 proposes `qntx-meili` (Rust) as the SearchService provider. ADR-016 proposes `qntx-faiss` (C++) as the VectorSearchService provider. Two plugins, two languages, two processes, two index stores.

Qdrant is a Rust search engine that covers both jobs in one place: dense vector search (HNSW), sparse vectors (BM25-style), payload filters, and first-class hybrid fusion. Its crates can be used in-process as a library rather than run as a separate server.

If one engine can provide both services, two plugin processes collapse to one, and the C++ plugin toolchain does not need to be widened for ADR-016.

## Decision

Add `qntx-qdrant` as a single Rust plugin that registers as the provider for **both** SearchService (ADR-015) and VectorSearchService (ADR-016). Qdrant is used embedded — its crates run in the plugin's own process, no separate Qdrant server.

Both proto contracts stay as defined in ADR-015 and ADR-016. This ADR changes the provider shape, not the service shape. Either ADR-015's and ADR-016's original providers (`qntx-meili`, `qntx-faiss`) or this one can implement the contracts — consumers don't care which.

`qntx-qdrant` lives in `qntx-plugins/qntx-qdrant/` in this repository.

## Protocol

No new proto. The plugin implements:

- `SearchService` — from `plugin/grpc/protocol/search.proto` (ADR-015)
- `VectorSearchService` — from `plugin/grpc/protocol/vectorsearch.proto` (ADR-016)

Both services register on `ServiceRegistry` from the same plugin process via `SetService`.

## Mapping services to Qdrant

- **SearchService.Search** — text query + filters → Qdrant full-text / sparse-vector search over the requested collection. `SearchRequest.filters` (JSON bytes) maps to a Qdrant payload filter. Hits return payload as `document` bytes.
- **SearchService.IndexDocuments** — JSON documents in, upserted as Qdrant points with payload. Collection created implicitly if missing; schema inferred from the first batch.
- **SearchService.DeleteDocuments** — point deletion by ID.
- **VectorSearchService.Search** — query vector + top_k → Qdrant ANN search on the named collection. Hits return `id` and `distance`.

Full-text in Qdrant is payload-field text matching plus sparse vectors, not a Lucene-grade BM25 engine. Adequate for QNTX's needs today (fuzzy-ax replacement, keyword lookup over attestations); revisit if relevance ranking becomes a bottleneck.

## Why one plugin for both

- **Hybrid retrieval.** Dense + sparse fusion (RRF) happens inside Qdrant in a single call. With two providers in two processes, fusion has to cross the plugin boundary twice and be assembled in core. This is the long-term payoff — the reason to co-locate.
- **One index store.** A document and its embedding live as one Qdrant point with payload. No divergence between the keyword index and the vector index.
- **One language, one toolchain.** Rust only. ADR-016's C++ plugin build surface is not needed for search.
- **One process, one startup dependency.** ADR-015 already notes startup ordering as a concern; one provider = one ordering constraint instead of two.

## Relationship to ADR-015 and ADR-016

This ADR does not retract either of them. It proposes an alternative provider that satisfies both contracts. Possible outcomes:

1. `qntx-qdrant` is the default provider; `qntx-meili` and `qntx-faiss` remain as optional heavy-duty replacements when Qdrant's full-text or vector recall ceiling is hit.
2. `qntx-qdrant` replaces both, and ADR-015/016's original providers are never built.
3. `qntx-qdrant` is built alongside the others; deployments pick one per service via `ServiceRegistry`.

The choice is deferred. The service contracts are what's stable.

## Routing

Same pattern as ADR-014 / ADR-015 / ADR-016. The plugin registers `SearchService` and `VectorSearchService` during Initialize. Callers go through `services.Search()` and `services.VectorSearch()` on `ServiceRegistry`. Nothing in the registry changes.

## Panel glyph

`qntx-qdrant` registers a panel glyph covering both concerns: collections, point counts, dense vs sparse index status, recent query latency. Replaces the separate index-management surface ADR-015 gives `qntx-meili`.

## Consequences

- One Rust plugin provides two `ServiceRegistry` services — first time a single provider registers for more than one service.
- Hybrid search (keyword + vector fusion) becomes a local operation inside the plugin; no core-side fusion code needed to get it.
- C++ plugin toolchain is not pulled in for vector search.
- Full-text quality ceiling is Qdrant's text-matching + sparse vectors, not Lucene/Meili. If the fuzzy-ax replacement path needs more, fall back to ADR-015's `qntx-meili` as an alternative SearchService provider — the contract is the same.
- Qdrant's embedded/in-process usage is less advertised than its server mode. If the crates prove awkward to embed, the plugin can fall back to supervising a Qdrant child process without changing the service contract.
