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

### Similarity scoring
The current formula `1.0 - (distance / 2.0)` assumes L2 distances stay below 2.0. Structured attestation text (short keywords like "buy AAPL 150 limit") produces L2 distances of 3–6, causing similarity to clamp to 0. The default threshold of 0.7 returns nothing for these inputs. Use `threshold=0.0` to see results — ranking is correct even when similarity reads 0.

Needs rethinking: cosine similarity instead of L2, or a normalization scheme that accounts for the actual distance distribution of attestation text.

### Rich semantic search integration
Currently, embeddings are generated from the raw attestation command string. This produces poor vectors — short structured text is too sparse for sentence transformers.

The existing fuzzy search system (`ats/storage/rich_search.go`) solves the same problem differently: type definitions declare `rich_string_fields` in their attributes (e.g. a Commit type declares `["message", "description"]`), discovered dynamically via `getTypeDefinitions()` from attestations with `predicate="type"`, `context="graph"`. At search time, `json_extract(a.attributes, '$.fieldName')` pulls the actual text from those fields. The fuzzy engine then tokenizes and matches against this vocabulary.

Embeddings should follow the same pattern:
- Use `rich_string_fields` from type definitions to determine what text to embed per attestation
- Concatenate the declared fields into a single input string for the sentence transformer
- Re-embed when rich text fields change (type def updates should invalidate affected embeddings)
- Unify fuzzy search and semantic search under one search system — fuzzy becomes word-level matching, semantic becomes meaning-level matching, both operating on the same rich text fields
- UI reflects this as search modes rather than separate features

This is the critical integration point: `buildDynamicRichStringFields()` already aggregates all searchable field names across types. The embedding pipeline needs access to the same mechanism.

### Verification (requires populated database)
Verified end-to-end by copying attestations from a backup database:
1. `POST /api/embeddings/batch` with attestation IDs — 8 attestations embedded in 983ms, 0 failures
2. `GET /api/search/semantic?q=<query>&threshold=0.0` — returns semantically ranked results
3. Result ordering reflects semantic similarity (see similarity scoring caveat above)

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
- Error handling standardization across Rust/Go FFI boundary

### Design decision: unconditional sqlite-vec
`db/connection.go` imports `sqlite-vec` CGO bindings unconditionally — every Go build pays the CGO compilation cost, even builds that don't use embeddings. This is coupled to migration 024, which creates a `vec0` virtual table that requires the extension to be loaded. The migration runs unconditionally via `//go:embed sqlite/migrations/*.sql`.

Making this conditional requires solving both sides together:
- Move the `sqlite_vec` import behind a build tag
- Move migration 024 out of the embedded migrations directory (or split it: regular `embeddings` table stays universal, `vec_embeddings` virtual table becomes conditional)

Current choice: accept the universal CGO dependency. The `cli-nocgo` target (CGO_ENABLED=0) will fail on migration 024 at runtime if it encounters a database that hasn't run that migration yet.
