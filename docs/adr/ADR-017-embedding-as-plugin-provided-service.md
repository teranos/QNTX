# ADR-017: Embedding as Plugin-Provided Service

## Status

Accepted

## Context

QNTX's embedding engine (ONNX inference + HDBSCAN clustering) was compiled into the main binary via CGO/Rust FFI, gated behind `cgo && rustembeddings` build tags. This created build complexity, prevented hot-reload, and locked to a single hardcoded model (384-dim).

The existing provider pattern (ADR-014 for LLM, ADR-015 for search) provides the model: a plugin declares `embedding_provider = true` during Initialize, QNTX routes embedding calls to it via gRPC.

## Decision

Add `embedding_provider` to the plugin provider pattern. Any plugin implementing `EmbeddingService` gRPC (Embed, BatchEmbed, Cluster, ModelInfo) alongside `DomainPluginService` can serve as the embedding backend. QNTX creates a `PluginEmbeddingService` that satisfies the existing `Service` interface by making gRPC calls to the plugin instead of CGO/FFI calls.

### Protocol changes

- `embedding.proto` ([EmbeddingService gRPC API](../api/grpc-embedding.md)): Add `Cluster` and `ModelInfo` RPCs
- `domain.proto` ([Plugin gRPC API](../api/grpc-plugin.md)): Add `embedding_provider` bool to `InitializeResponse`
- Rust proto builds (`qntx-proto`, `qntx-grpc`): Include `embedding.proto`

### Plugin side

- Implement `EmbeddingService` trait (Embed, BatchEmbed, Cluster, ModelInfo)
- Register `EmbeddingServiceServer` on the gRPC server alongside `DomainPluginServiceServer`
- Set `embedding_provider: true` in `InitializeResponse`

### Core side (QNTX)

- `PluginEmbeddingService` (no build tags): gRPC client implementing `Service` interface
- Plugin discovery: detect `embedding_provider` flag, call `SetupPluginEmbeddingService`
- Clustering: `RunHDBSCANClustering` accepts a `ClusterFunc` — plugin provides its own via `Cluster` RPC
- Restart re-wiring: `onEmbeddingProviderReady` callback re-establishes the embedding backend when a plugin restarts

### Pure Go operations (no RPC needed)

`SerializeEmbedding`, `DeserializeEmbedding`, `ComputeSimilarity` are pure math — implemented directly in Go on `PluginEmbeddingService`, not routed through gRPC.

## Consequences

- Embedding service available without CGO/Rust FFI build complexity
- Hot-reload: restart embedding plugin without restarting QNTX
- Multi-model: plugin detects model dimensions at load time
- `PluginEmbeddingService` has no build tags — pure Go gRPC client
- CGO/FFI embedding path removed — plugin is the only backend
