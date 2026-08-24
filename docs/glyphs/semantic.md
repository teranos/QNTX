# Semantic Search Glyph (⊨)

Live natural language search on canvas. Type a query, get attestation matches ranked by cosine similarity.

## Setup

The SE glyph requires the embedding service. Enable it in `am.toml`:

```toml
[embeddings]
enabled = true
path = "ats/embeddings/models/all-MiniLM-L6-v2/model.onnx"
name = "all-MiniLM-L6-v2"
```

The ONNX model file must exist at the configured `path`. See the embeddings README for download instructions.

After enabling, restart the server (`make dev`). The glyph checks availability on spawn and shows an error state if the service is unreachable.

## How it works

1. Right-click canvas, click ⊨, then click to place the glyph
2. Type a natural language query (e.g. "supply chain risks")
3. Backend creates a watcher that generates an embedding from your query
4. Existing attestations are searched by cosine similarity (historical matches)
5. New attestations are matched in real-time (live matches)
6. Results sorted by similarity score, threshold slider filters low-confidence matches

## Cluster scoping

When embeddings have been clustered (via `POST /api/embeddings/cluster`), a dropdown appears to scope search to a single cluster.

## Meld compositions

The SE glyph feeds results into downstream glyphs:

| Composition | Effect |
|-------------|--------|
| SE → py | Matched attestations passed as input to Python glyph |
| SE → prompt | Matched attestations injected into prompt template |
| SE → SE | Intersection — downstream shows only attestations matching both queries |

### SE → SE intersection

When SE₁ melds rightward to SE₂, SE₂ switches from standalone mode to intersection mode. Its own watcher is disabled; a compound watcher takes over that requires both SE₁'s and SE₂'s queries to pass their respective thresholds. SE₁ continues showing its own results independently.

On unmeld, SE₂'s standalone watcher is re-enabled and it reverts to its own query results.

### Planned

| Composition | Effect |
|-------------|--------|
| AX → SE | AX query results fed as context for semantic refinement |
| SE → SE → SE | Transitive intersection (SE₁∩SE₂∩SE₃) — needs the full ancestor chain propagated through the meld graph; today intersection is pairwise |
| SE ↓ SE | Union — vertical composition merges disjoint semantic regions ("machine learning" ↓ "gardening" shows attestations matching either); the dual of intersection |

## Files

| File | Role |
|------|------|
| `web/ts/components/glyph/semantic-glyph.ts` | Glyph factory + result rendering |
| `ats/watcher/engine.go` | Backend semantic matching engine |
| `server/embeddings_handlers.go` | Embedding generation + search API |
| `ats/storage/embedding_store.go` | sqlite-vec vector storage |
