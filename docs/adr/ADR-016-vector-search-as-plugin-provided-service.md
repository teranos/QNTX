# ADR-016: Vector Search as a Plugin-Provided Service

## Status
Proposed

## Context

EmbeddingService exists and generates vectors from text (core-owned, Rust FFI). Vectors are stored in sqlite-vec. But there is no way to search them — a plugin that has a vector and wants to find the nearest neighbors has no service to call.

FAISS is the standard library for fast approximate nearest-neighbor search over dense vectors. This is a separate concern from both EmbeddingService (which generates vectors) and SearchService (ADR-015, which provides full-text/keyword search via MeiliSearch).

Embedding is "turn text into a vector." Searching an index of vectors is a different operation. They should not be conflated.

## Decision

Add VectorSearchService as a plugin-provided gRPC service. A new plugin — `qntx-faiss` (C++) — owns the FAISS indexes and registers as the vector search backend. Core routes calls to the provider. Same plugin-provided service pattern as LLMService (ADR-014) and SearchService (ADR-015).

`qntx-faiss` is a dedicated vector search provider plugin, separate from any domain plugin. Domain plugins are consumers.

This is the third plugin-provided service on `ServiceRegistry`.

## Protocol

`plugin/grpc/protocol/vectorsearch.proto`

```protobuf
service VectorSearchService {
  rpc Search(VectorSearchRequest) returns (VectorSearchResponse);
}

message VectorSearchRequest {
  string index = 1;                 // which FAISS index to search
  repeated float query_vector = 2;  // query vector
  int32 top_k = 3;
}

message VectorSearchResponse {
  repeated VectorSearchHit results = 1;
}

message VectorSearchHit {
  string id = 1;              // identifier of the matched item
  float distance = 2;         // distance from query vector
}
```

## Relationship to other services

Three services, three plugins, three concerns:

- **EmbeddingService** (core, Rust FFI) — text → vector
- **SearchService** (ADR-015, `qntx-meili`, Rust) — full-text/keyword search over documents
- **VectorSearchService** (`qntx-faiss`, C++) — nearest-neighbor search over vector indexes

A plugin may use all three. A workflow might embed text via EmbeddingService, find semantically similar items via VectorSearchService, then search documents by keyword via SearchService. Different tools for different jobs.

## Routing

Same pattern as LLMService and SearchService: `VectorSearchServer` in core holds a reference to the provider backend. Provider plugins register via `RegisterProvider(name, client)`. Callers go through `services.VectorSearch()` on `ServiceRegistry`.

## Index management

FAISS index creation, rebuilding, and persistence are `qntx-faiss`'s responsibility. Consumers only search. How indexes are populated — whether `qntx-faiss` subscribes to embedding events, receives explicit index-build requests, or manages its own ingestion — is an implementation detail of the plugin.

## Future: EmbeddingService on ServiceRegistry

EmbeddingService is currently accessed by plugins via `_embedding_endpoint` in config — plugins create their own gRPC client (same pattern as Werf). This works. A future improvement would be to expose `Embedding()` on `ServiceRegistry` for convenience, following the same lazy-init pattern as `VectorSearch()` and `LLM()`. Not a blocker — just less boilerplate for consumer plugins.

## Consequences

- `qntx-faiss` is a standalone C++ plugin — vector search infrastructure lives in its own process
- EmbeddingService stays pure: text → vector, nothing more
- Domain plugins get vector similarity search without linking FAISS or managing indexes
- Clear separation: embedding (core), full-text search (`qntx-meili`), vector search (`qntx-faiss`)
