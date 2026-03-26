# QNTX Reduce Plugin

Dimensionality reduction plugin for projecting 384-dim embeddings to 2D coordinates via UMAP. Enables cluster visualization on the canvas (see [docs/vision/clusters.md](../docs/vision/clusters.md)).

## Why a Separate Plugin

The Rust UMAP ecosystem is immature. Python's `umap-learn` is battle-tested and fast. This plugin embeds Python via PyO3 in a separate process — same pattern as `qntx-python` — so the UMAP model stays in Python process memory for fast `.transform()` on new points.

The `DimensionReducer` trait in `ats/embeddings/src/reduce.rs` defines the contract so a native Rust backend can replace this when the ecosystem matures.

## Building

Requires Nix (provides Python 3.13 + umap-learn + numpy + scikit-learn deterministically).

```bash
# Build binary
make rust-reduce

# Install to ~/.qntx/plugins/
make rust-reduce-install
```

## Usage

### With QNTX

Add to `am.toml`:

```toml
[plugin]
enabled = ["reduce"]
```

Then `make dev` — the plugin is discovered and started automatically.

### Standalone

```bash
qntx-reduce-plugin                    # default port 9001
qntx-reduce-plugin --port 9002        # custom port
qntx-reduce-plugin --log-level debug  # verbose logging
```

## API

All endpoints are accessible via QNTX's plugin routing at `/api/reduce/...`.

### POST /fit

Fit UMAP on all embeddings and return 2D projections. The fitted model is kept in memory for subsequent `/transform` calls.

```json
{
  "embeddings": [[0.1, 0.2, ...], [0.3, 0.4, ...]],
  "n_neighbors": 15,
  "min_dist": 0.1,
  "metric": "cosine"
}
```

Response:
```json
{
  "projections": [[1.23, -0.45], [2.67, 1.89]],
  "n_points": 2,
  "fit_ms": 3400
}
```

All parameters except `embeddings` are optional (defaults shown above).

### POST /transform

Project new points using the fitted model. Returns 412 if `/fit` hasn't been called.

```json
{
  "embeddings": [[0.5, 0.6, ...]]
}
```

Response:
```json
{
  "projections": [[1.45, -0.23]],
  "n_points": 1,
  "transform_ms": 12
}
```

### GET /status

```json
{
  "fitted": true,
  "n_points": 500
}
```

## Server-Side Integration

The Go server provides two orchestration endpoints that call the reduce plugin internally:

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/embeddings/project` | Read all embeddings, call `/fit`, store projection_x/projection_y |
| GET | `/api/embeddings/projections` | Return `[{id, source_id, x, y, cluster_id}]` for frontend |

New attestations are auto-projected via `/transform` in the `EmbeddingObserver` if the model is fitted. If not fitted (plugin unavailable or `/fit` not yet called), projection is silently skipped.

## Architecture

```
Go server
  |
  |-- POST /api/embeddings/project  (orchestrator)
  |     |
  |     +-- reads all embeddings from DB
  |     +-- gRPC HandleHTTP → reduce plugin /fit
  |     +-- writes projection_x, projection_y to embeddings table
  |
  |-- EmbeddingObserver.OnAttestationCreated
        |
        +-- generates embedding (Rust ONNX)
        +-- predicts cluster (cosine similarity)
        +-- gRPC HandleHTTP → reduce plugin /transform  (if fitted)
        +-- writes projection_x, projection_y
```

## Database

Migration `032_add_projection_columns_to_embeddings.sql` adds:

```sql
ALTER TABLE embeddings ADD COLUMN projection_x REAL;
ALTER TABLE embeddings ADD COLUMN projection_y REAL;
```

NULL = not yet projected.
