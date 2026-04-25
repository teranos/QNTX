# ADR-017: Embedding as Plugin-Provided Service

## Status

Accepted

## Context

QNTX's embedding engine (ONNX inference + HDBSCAN clustering) is compiled into the main binary via CGO/Rust FFI, gated behind `cgo && rustembeddings` build tags. This creates build complexity, prevents hot-reload, and locks to a single hardcoded model (384-dim).

Cyrnel (`ctp/Cyrnel/`) packages the same embedding + clustering capabilities as a standalone Rust plugin. The existing provider pattern (ADR-014 for LLM, ADR-015 for search) provides the model: a plugin declares `embedding_provider = true` during Initialize, QNTX routes embedding calls to it via gRPC.

## Decision

Add `embedding_provider` to the plugin provider pattern. Cyrnel implements `EmbeddingService` gRPC (Embed, BatchEmbed, Cluster, ModelInfo) alongside `DomainPluginService`. QNTX creates a `PluginEmbeddingService` that satisfies the existing `Service` interface by making gRPC calls to the plugin instead of CGO/FFI calls.

### Protocol changes

- `embedding.proto`: Add `Cluster` and `ModelInfo` RPCs
- `domain.proto`: Add `embedding_provider` bool to `InitializeResponse`
- Rust proto builds (`qntx-proto`, `qntx-grpc`): Include `embedding.proto`

### Plugin side (Cyrnel)

- Implement `EmbeddingService` trait (Embed, BatchEmbed, Cluster, ModelInfo)
- Register `EmbeddingServiceServer` on the tonic server alongside `DomainPluginServiceServer`
- Set `embedding_provider: true` in `InitializeResponse`
- Drop HTTP-based `/api/cyrnel/embed` and `/api/cyrnel/cluster` endpoints (replaced by typed gRPC)

### Core side (QNTX)

- `PluginEmbeddingService` (new, no build tags): gRPC client implementing `Service` interface
- Plugin discovery: detect `embedding_provider` flag, store plugin endpoint
- `SetupEmbeddingService`: when Cyrnel detected, use `PluginEmbeddingService` instead of `ManagedEmbeddingService`
- Clustering: `RunHDBSCANClustering` calls `Cluster` RPC when using plugin backend

### Pure Go operations (no RPC needed)

`SerializeEmbedding`, `DeserializeEmbedding`, `ComputeSimilarity` are pure math — implemented directly in Go on `PluginEmbeddingService`, not routed through gRPC.

## Consequences

- Embedding service available without CGO/Rust FFI build complexity
- Hot-reload: restart Cyrnel plugin without restarting QNTX
- Multi-model: Cyrnel detects model dimensions at load time
- Existing builtin path unchanged — `cgo && rustembeddings` builds continue to work
- `PluginEmbeddingService` has no build tags — pure Go gRPC client
