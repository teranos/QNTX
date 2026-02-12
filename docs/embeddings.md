# Embeddings

Semantic search over attestations using sentence transformers (all-MiniLM-L6-v2) via ONNX Runtime.

## Architecture

- **Rust** (`ats/embeddings/src/`): ONNX Runtime 2.0 inference, HuggingFace tokenizer, mean pooling → 384-dim vectors
- **Go** (`ats/embeddings/embeddings/`): CGO bindings to Rust, model lifecycle, FLOAT32_BLOB serialization
- **Storage** (`ats/storage/embedding_store.go`): sqlite-vec L2 distance search, DELETE+INSERT for virtual table compatibility
- **API** (`server/embeddings_handlers.go`): conditional compilation via `rustembeddings` build tag (now default in `make cli`)
- **Migration**: `024_create_embeddings_table.sql` — `embeddings` table + `vec_embeddings` virtual table

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/search/semantic?q=<text>&limit=10&threshold=0.7` | Search stored embeddings by semantic similarity |
| POST | `/api/embeddings/generate` | Generate embedding for `{"text": "..."}` — returns 384-dim vector |
| POST | `/api/embeddings/batch` | Embed attestations by ID: `{"attestation_ids": ["..."]}` |

Without the `rustembeddings` build tag, all endpoints return 503.

## Model Files

Located at `ats/embeddings/models/all-MiniLM-L6-v2/` (not in git). See [ats/embeddings/README.md](https://github.com/teranos/QNTX/blob/main/ats/embeddings/README.md) for download instructions.

## Open Work

### Attestation → Embedding pipeline
Nothing currently triggers embedding generation when attestations are created. The batch endpoint exists but must be called manually. Semantic search returns empty results until attestations have embeddings. This is the critical gap between "embeddings work" and "semantic search is useful."

Options to explore:
- Automatic embedding on attestation creation (hook in storage layer)
- Background job via Pulse daemon
- On-demand embedding at search time

### Verification (requires populated database)
To fully verify semantic search post-merge, copy attestations from an existing database, then:
1. `POST /api/embeddings/batch` with attestation IDs
2. `GET /api/search/semantic?q=<query>` — verify results return with similarity scores
3. Verify result ordering reflects actual semantic similarity

### Frontend
- Semantic Search Glyph (proposed symbol: ⊨ double turnstile)
- Composition with other glyphs via Meld system
- Integration with Glyph Attestation flow

### Open Questions
- **Model distribution**: Bundled, downloaded on-demand, or user-provided?
- **Caching**: What layer? In-memory, SQLite, or external?
- **Multi-model support**: Should multiple embedding models run simultaneously?
- **Fine-tuning**: Domain-specific fine-tuning for attestation language?
- **Vector database**: sqlite-vec vs dedicated vector DB (Qdrant, Weaviate) at scale?
- **Rate limiting**: Embedding generation is CPU-intensive — what limits are appropriate?
- **Batch queue**: Should batch jobs go through Pulse daemon instead of synchronous HTTP?

### Technical Debt
- `unsafe { std::mem::zeroed() }` in `engine_simple.rs` — undefined behavior for non-null types
- `ort::init()` return value unused in `engine.rs` — `commit()` must be called
- Error handling standardization across Rust/Go FFI boundary
- sqlite-vec CGO import in `db/connection.go` is unconditional — affects all build times
