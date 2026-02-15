# Reduce Plugin

Dimensionality reduction for embedding visualization on the canvas. Projects 384-dim embeddings to 2D via UMAP. See [vision/clusters.md](../vision/clusters.md) for the end-state UX.

## Setup

Requires Nix (provides Python 3.13 + umap-learn + numpy + scikit-learn).

```bash
make rust-reduce            # build binary to bin/
make rust-reduce-install    # copy to ~/.qntx/plugins/
```

Enable in `am.toml`:

```toml
[plugin]
enabled = ["reduce"]
```

## Architecture

```
EmbeddingObserver.OnAttestationCreated
  +-- generates embedding (Rust ONNX)
  +-- predicts cluster (cosine similarity)
  +-- gRPC → reduce plugin /transform  (if fitted)
  +-- writes projection_x, projection_y
```

`POST /api/embeddings/project` fits UMAP on all embeddings (batch). New attestations are auto-projected inline via `/transform` if the model is fitted.

The UMAP model lives in Python process memory. The `DimensionReducer` trait in `ats/embeddings/src/reduce.rs` is the contract for a future native Rust backend.

## Endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/embeddings/project` | Fit UMAP on all embeddings, store 2D projections |
| GET | `/api/embeddings/projections` | `[{id, source_id, x, y, cluster_id}]` for frontend |
| GET | `/api/reduce/status` | `{fitted: bool, n_points: int}` |

## What's Next

- **Frontend**: Canvas component reading `/api/embeddings/projections`, scatter plot colored by `cluster_id`
- **Re-projection on re-cluster**: After `POST /api/embeddings/cluster`, auto re-run projection
- **Pulse scheduling**: Periodic re-clustering + re-projection via the daemon
