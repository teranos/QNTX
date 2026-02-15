# Embeddings

Semantic search over attestations using sentence transformers (all-MiniLM-L6-v2) via ONNX Runtime.

## Architecture

- **Rust** (`ats/embeddings/src/`): ONNX Runtime 2.0 inference, HuggingFace tokenizer, mean pooling, L2 normalization → 384-dim unit vectors
- **Go** (`ats/embeddings/embeddings/`): CGO bindings to Rust, model lifecycle, FLOAT32_BLOB serialization
- **Storage** (`ats/storage/embedding_store.go`): sqlite-vec L2 distance search, DELETE+INSERT for virtual table compatibility
- **API** (`server/embeddings_handlers.go`): conditional compilation via `rustembeddings` build tag (now default in `make cli`)
- **Migration**: `024_create_embeddings_table.sql` — `embeddings` table + `vec_embeddings` virtual table

## Configuration

Embeddings are configured via `am.toml` or the UI config API:

```toml
[embeddings]
enabled = true
path = "ats/embeddings/models/all-MiniLM-L6-v2/model.onnx"
name = "all-MiniLM-L6-v2"
cluster_threshold = 0.5
recluster_interval_seconds = 3600  # re-cluster every hour (0 = disabled)
min_cluster_size = 5
```

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `embeddings.enabled` | bool | `false` | Enable the embedding service on startup |
| `embeddings.path` | string | `ats/embeddings/models/all-MiniLM-L6-v2/model.onnx` | Path to ONNX model file |
| `embeddings.name` | string | `all-MiniLM-L6-v2` | Model identifier for metadata |
| `embeddings.cluster_threshold` | float | `0.5` | Minimum cosine similarity for incremental cluster assignment |
| `embeddings.recluster_interval_seconds` | int | `0` | Pulse schedule interval for HDBSCAN re-clustering (0 = disabled) |
| `embeddings.min_cluster_size` | int | `5` | Minimum cluster size for HDBSCAN |

When `enabled = false` (default), `SetupEmbeddingService` skips initialization even if built with the `rustembeddings` tag. Enabling requires the model file to exist at the configured `path`.

When `recluster_interval_seconds > 0`, a Pulse scheduled job is auto-created on startup to periodically re-run HDBSCAN clustering. The schedule is idempotent — restarting the server won't duplicate it. Changing the interval in config updates the existing schedule.

Config can also be updated at runtime via the REST API:

```
PATCH /api/config
{"updates": {"embeddings.enabled": true, "embeddings.path": "/path/to/model.onnx"}}
```

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/search/semantic?q=<text>&limit=10&threshold=0.7` | Search stored embeddings by semantic similarity |
| POST | `/api/embeddings/generate` | Generate embedding for `{"text": "..."}` — returns 384-dim vector |
| POST | `/api/embeddings/batch` | Embed attestations by ID: `{"attestation_ids": ["..."]}` |
| POST | `/api/embeddings/cluster` | Run HDBSCAN clustering: `{"min_cluster_size": 5}` |
| GET | `/api/embeddings/info` | Embedding service status, counts, and cluster summary |

Without the `rustembeddings` build tag, all endpoints return 503.

## Model Files

Located at `ats/embeddings/models/all-MiniLM-L6-v2/` (not in git). See [ats/embeddings/README.md](https://github.com/teranos/QNTX/blob/main/ats/embeddings/README.md) for download instructions.

## Completed

- **Auto-embedding pipeline**: `EmbeddingObserver` embeds attestations with rich text on creation (#482)
- **Rich text integration**: Uses `rich_string_fields` from type definitions (#479)
- **Unified search**: Text + semantic results merged and deduplicated (#485)
- **Semantic Search Glyph (⊨)**: Live canvas glyph with historical + live matching (#496, #499)
- **Scheduled re-clustering**: HDBSCAN runs on a Pulse schedule so cluster topology stays current as data grows (#506)

## Open Work

### Open Questions
- **Model distribution**: Bundled, downloaded on-demand, or user-provided?
- **Caching**: What layer? In-memory, SQLite, or external?
- **Multi-model support**: Should multiple embedding models run simultaneously?
- **Fine-tuning**: Domain-specific fine-tuning for attestation language?
- **Vector database**: sqlite-vec vs dedicated vector DB (Qdrant, Weaviate) at scale?
- **Rate limiting**: Embedding generation is CPU-intensive — what limits are appropriate?

### Design decision: embedding tests are local-only
Embedding tests (`ats/embeddings/embeddings/embeddings_test.go`) require the ONNX model files (~80MB) and add ~3s of inference per run. They're gated behind `//go:build cgo && rustembeddings` — CI doesn't pass this tag, so they only run locally.

This avoids burdening every PR with model download/caching and inference time. If the embedding surface area grows, a dedicated `ci-embeddings.yml` workflow (triggered only on changes to `ats/embeddings/`, `ats/storage/embedding_store*`, `server/embeddings_handlers*`) can be added without affecting the main pipeline.

### Technical Debt
- Error handling standardization across Rust/Go FFI boundary

### Design decision: unconditional sqlite-vec
`db/connection.go` imports `sqlite-vec` CGO bindings unconditionally — every Go build pays the CGO compilation cost, even builds that don't use embeddings. This is coupled to migration 024, which creates a `vec0` virtual table that requires the extension to be loaded. The migration runs unconditionally via `//go:embed sqlite/migrations/*.sql`.

Making this conditional requires solving both sides together:
- Move the `sqlite_vec` import behind a build tag
- Move migration 024 out of the embedded migrations directory (or split it: regular `embeddings` table stays universal, `vec_embeddings` virtual table becomes conditional)

Current choice: accept the universal CGO dependency. The `cli-nocgo` target (CGO_ENABLED=0) will fail on migration 024 at runtime if it encounters a database that hasn't run that migration yet.
