# ADR-019: Per-Embedding Multi-Model Support

**Status:** In Progress (Phase 3 verified)
**Date:** 2026-05-11
**Related:** [ADR-017: Embedding as Plugin-Provided Service](./ADR-017-embedding-as-plugin-provided-service.md), [ADR-001: Domain Plugin Architecture](./ADR-001-domain-plugin-architecture.md)

## Context

QNTX currently assumes a single embedding model. Cyrnel loads one ONNX model at startup, the `EmbeddingObserver` calls one service, and the schema hardcodes `FLOAT32[384]` for sqlite-vec. The `embeddings` table stores `model` and `dimensions` per row, but nothing above it uses them — deduplication, search, clustering, and projection all operate as if one model exists.

This blocks experimentation. Comparing embedding models (MiniLM vs BGE vs E5) requires swapping the model and re-embedding everything. There's no way to see how two models cluster the same corpus, or to migrate from one model to another without a cutover.

Two approaches were considered:

1. **Per-space** — named embedding spaces, each backed by one model. Clean query semantics (no model filter needed), but rigid: adding a model means creating a new space and backfilling all existing attestations.

2. **Per-embedding** — every vector is tagged with its model identity. Same attestation can have multiple embeddings from different models. More flexible, supports gradual migration and A/B comparison, but requires model as a filter on every query path.

## Decision

Every embedding carries its model identity. Model is a first-class dimension on storage, search, clustering, and projection.

### Schema

The `embeddings` table already has `model` and `dimensions` columns. Changes:

- **Unique constraint**: `(source_type, source_id)` becomes `(source_type, source_id, model)` — same source, multiple embeddings from different models.
- **`vec_embeddings`**: one vec0 virtual table per model, since sqlite-vec requires fixed dimensions. Named `vec_embeddings_{model_slug}` (e.g., `vec_embeddings_minilm_l6_v2`). Created dynamically when a model is first used.
- **Cluster and projection columns** remain per-embedding row — they're already model-scoped by being attached to a specific embedding.

```sql
-- Existing table, new constraint
CREATE UNIQUE INDEX IF NOT EXISTS idx_embeddings_source_model
    ON embeddings(source_type, source_id, model);

-- Per-model vec0 tables (created dynamically)
CREATE VIRTUAL TABLE IF NOT EXISTS vec_embeddings_minilm_l6_v2
    USING vec0(embedding_id TEXT PRIMARY KEY, embedding FLOAT32[384]);
```

### gRPC Protocol

`EmbedRequest`, `BatchEmbedRequest`, and `ModelInfoRequest` gain a `model` field (field number 3, 3, and 2 respectively). If empty, cyrnel uses its default (first loaded) model. Responses already included model name and dimensions — now always populated.

```protobuf
message EmbedRequest {
    string auth_token = 1;
    string text = 2;
    string model = 3;  // target model name, empty = default
}

message ModelInfoRequest {
    string auth_token = 1;
    string model = 2;  // target model name, empty = default
}
```

Proto changes are additive — existing callers that don't set the `model` field get the default model (backward compatible).

### Cyrnel

```
┌──────────────────────────────────────────────┐
│ CyrnelPluginService                          │
│                                              │
│  Initialize(config)                          │
│    ├─ models[0] → Arc<RwLock<LoadedModel>>   │
│    ├─ models[1] → Arc<RwLock<LoadedModel>>   │
│    └─ order[0] = default model               │
│                                              │
│  Embed(text, model?) ──► per-model WriteLock │
│  BatchEmbed(texts, model?) ──► same          │
│  ModelInfo(model?) ──► metadata              │
│  ListModels() ──► all loaded models          │
└──────────────────────────────────────────────┘
```

- `Engine.embed(&self)` with per-model `Arc<RwLock<LoadedModel>>` — ort 2.0.0-rc.11 `Session::run` requires `&mut self`, so inference takes a write lock per model. Model A does not block model B.
- `Engine` tracks insertion order via `Vec<String>` — first model loaded is the default.
- `resolve_model(name)` returns default when name is empty.
- Legacy single-model config (`model_path`/`model_name`) supported as fallback.

### Config

QNTX passes plugin config as `map<string, string>` via gRPC `InitializeRequest`. The Go config bridge (`client.go:doInitialize`) serializes `[]interface{}` values as JSON strings. A TOML array like `models = ["/path/a", "/path/b"]` arrives as a single key `"models"` with value `["/path/a","/path/b"]` (JSON). Cyrnel parses this with `serde_json::from_str::<Vec<String>>`. Model names are derived from the parent directory of each `.onnx` path.

```toml
# Multi-model (new) — matches gaze's models = [...] pattern
[cyrnel]
models = [
  "/path/to/all-MiniLM-L6-v2/model.onnx",
  "/path/to/bge-small-en-v1.5/model.onnx",
]

# Legacy single-model (still supported)
[cyrnel]
model_path = "/path/to/model.onnx"
model_name = "MiniLM-L6-v2"
```

### QNTX Core

- `Service` interface: `GenerateEmbedding(text, model string)`, `GenerateBatchEmbeddings(texts []string, model string)`
- `EmbeddingObserver`: configurable strategy — embed through default model, all models, or a specified subset
- `GetBySource` scoped by model: same attestation can have embeddings from multiple models
- Similarity search: model is a required parameter, routes to the correct vec0 table
- Clustering: model-scoped — `ClusterHDBSCAN` filters embeddings by model before clustering
- Projection: model-scoped — 2D coordinates are per-model, not global

### Query Flow

```
Search("similar to X", model="MiniLM-L6-v2")
  │
  ├─ Embed X via cyrnel with model=MiniLM-L6-v2
  ├─ Query vec_embeddings_minilm_l6_v2
  └─ Return results with model metadata
```

## Consequences

### Positive

✅ Compare models on the same corpus without re-embedding
✅ Gradual model migration — new model runs alongside old, switch when confident
✅ Concurrent embedding across models — loading a second model doesn't block the first
✅ Schema already stores model identity per row — migration is additive
✅ Proto changes are backward compatible — empty model field means default

### Negative

⚠️ Every query path gains a model parameter — more surface area for bugs
⚠️ Per-model vec0 tables mean dynamic DDL — tables created at runtime when new models appear
⚠️ Storage multiplies with each model — N models means N embeddings per attestation
⚠️ Backfill needed when adding a model to an existing corpus
⚠️ ort 2.0.0-rc.11 `Session::run` is `&mut self` — inference requires exclusive lock per model, no concurrent inference within a single model

### Neutral

- Clustering quality comparison becomes possible but requires UI to display side-by-side results
- The `ComputeSimilarity` function is model-agnostic (cosine similarity on any vectors of equal length) — no change needed, but callers must ensure they don't cross model boundaries

## Implementation

### Phase 1: Cyrnel Multi-Model ✅

- `Engine` stores `HashMap<String, Arc<RwLock<LoadedModel>>>` with insertion-ordered default
- `embed(&self)` with per-model write locks
- `EmbedRequest.model`, `BatchEmbedRequest.model`, `ModelInfoRequest.model` fields in proto
- Config parses `models` key as JSON array of paths, falls back to `model_path`/`model_name`
- Go/TS/OCaml proto regenerated, QNTX builds and tests pass
- Health endpoint and `/api/cyrnel/models` list all loaded models

### Phase 2: Schema + Storage ✅

- ✅ Unique index on `(source_type, source_id, model)` — migration 051
- ✅ Dynamic vec0 table creation per model — `EnsureVecTable` creates `vec_embeddings_{slug}` on first use
- ✅ `GetBySource` scoped by model — filters by model when non-empty
- ✅ `SemanticSearch` scoped by model — queries per-model vec table
- ✅ `EmbeddingStore` methods gain model parameter
- ✅ `GetAllEmbeddingVectors` scoped by model (for HDBSCAN clustering)
- ✅ `DeleteBySource` and `SweepStaleEmbeddings` handle multi-model vec tables

### Phase 3: Observer + Query ✅

- ✅ `EmbeddingObserver` multi-model strategy — loops over all configured models per attestation
- ✅ Model names derived from am.toml `cyrnel.models` paths via `ModelNamesFromPaths`
- ✅ Similarity search accepts model parameter and routes to correct vec table
- ✅ Clustering callers pass actual model — Pulse loops per-model, HTTP accepts `?model=`
- ✅ Projection callers pass actual model — Pulse loops per-model, HTTP accepts `?model=`
- ✅ Verified end-to-end: one attestation produces N embeddings (one per model)

### Phase 4: Internal Model Selection

Model selection is an internal system concern, not user-facing. Users never choose a model — the system picks underwater based on configured strategy (default model, all models, or subset). No model selector in the UI.

### Rich Text Fields

Embedding is triggered for attestation attributes matching:
- Fields declared as `rich_string` in type definitions (e.g. `response`, `label`)
- Builtin fields: `message`, `msg` — always embeddable regardless of type definitions

## Alternatives Considered

**Per-space model binding** — cleaner query semantics, no model filter needed. Rejected because it forces all-or-nothing model migration and prevents A/B comparison on the same corpus.

**Single model, swap and re-embed** — simplest approach, no schema changes. Rejected because it destroys previous embeddings and makes comparison impossible.

**Multiple cyrnel instances** — one plugin per model, each with its own config section. Rejected because it multiplies process overhead and doesn't solve the storage/query problem.
