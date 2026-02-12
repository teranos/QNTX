# Embeddings Integration Handover Document

## Context
This document provides a handover from Opus 4.1 to Opus 4.6 for continuing the semantic search embeddings feature integration. The `feat/sentence-transformers` branch was started on Feb 2, 2024, and significant architectural changes have occurred on main in the intervening 10 days.

## Current Branch Status

### What's Been Built (feat/sentence-transformers)
1. **Rust FFI Layer** (`crates/qntx-core/src/embeddings/`)
   - ONNX Runtime 2.0 integration for inference
   - HuggingFace tokenizers for text processing
   - Mean pooling for sentence-level embeddings
   - FFI bindings exposed via `crates/qntx-core/src/lib.rs`

2. **Go Embeddings Service** (`ats/embeddings/`)
   - CGO bindings to Rust FFI
   - ManagedEmbeddingService with model lifecycle management
   - FLOAT32_BLOB serialization for sqlite-vec compatibility

3. **Storage Layer** (`ats/storage/`)
   - EmbeddingStore with CRUD operations
   - Semantic search using sqlite-vec L2 distance
   - Migration 019 creates embeddings and vec_embeddings tables
   - Fixed virtual table UPSERT limitations (DELETE+INSERT pattern)

4. **API Endpoints** (`server/embeddings_handlers.go`)
   - GET `/api/search/semantic` - Semantic search
   - POST `/api/embeddings/generate` - Generate embeddings
   - POST `/api/embeddings/batch` - Batch processing
   - Conditional compilation with `rustembeddings` build tag

5. **sqlite-vec Integration**
   - Successfully integrated via `github.com/asg017/sqlite-vec-go-bindings/cgo`
   - vec0 module now available in all database connections
   - All tests passing, server starts successfully

## Major Changes on Main Branch

### 1. WASM Architecture (PRs #382, #403, #421, #452)
**Impact**: The core parsing and processing has moved to WASM, both browser and server-side via wazero.
**Integration Consideration**: The embeddings Rust code might need to be compiled to WASM for browser-side semantic search.

### 2. Removal of Go ax-parser (PRs #425, #460)
**Impact**: Parser is now WASM-based, changing how attestations are processed.
**Integration Consideration**: Embedding generation might need to hook into the new WASM pipeline.

### 3. FFI Utilities Extraction (PRs #415, #417)
**Impact**: Common FFI utilities now in shared crate.
**Integration Consideration**: The embeddings FFI code should use the shared utilities instead of duplicating.

### 4. Glyph System Evolution
- **Meld Compositions** (PRs #407, #426, #428, #437, #436, #443, #444, #445, #446, #451)
- **Chart Glyph** (PR #439)
- **Glyph Attestation Flow** (PRs #455, #466)

**Impact**: Glyphs are now the primary UI/UX abstraction with composition capabilities.
**Integration Consideration**: Semantic search should be expressed as a Glyph, potentially composed with other glyphs.

## Critical Integration Tasks

### 1. Merge Strategy
```bash
# Recommended approach to minimize risk
git checkout feat/sentence-transformers
git fetch origin
git merge origin/main --strategy=ours --no-commit
# Then selectively apply main's changes while preserving embeddings work
```

**Key Files to Preserve**:
- `crates/qntx-core/src/embeddings/` (entire module)
- `ats/embeddings/` (entire package)
- `ats/storage/embedding_store*.go`
- `server/embeddings_handlers.go`
- `db/sqlite/migrations/019_create_embeddings_table.sql`

**Key Files to Carefully Merge**:
- `crates/qntx-core/src/lib.rs` (FFI exports)
- `server/routing.go` (route registration)
- `server/init.go` (service initialization)
- `db/connection.go` (sqlite-vec init)
- `go.mod` (dependencies)

### 2. Server-Side Architecture (Decision Made)

**Decision**: Embeddings will remain server-side only for now.

**Implications**:
- No WASM compilation needed for embeddings/ONNX Runtime
- Semantic search requires network connectivity
- Simpler architecture, smaller client bundle
- Clear separation between WASM (parser/core) and native (embeddings)

**Integration Points**:
- API endpoints remain the primary interface
- Frontend communicates via REST/WebSocket
- Results can be cached client-side for performance
- Future WASM migration remains possible but not priority

### 3. Glyph Integration Design

Create a **Semantic Search Glyph** that:
- Accepts text input for query
- Displays results with similarity scores
- Can be composed with other glyphs (e.g., filter results with Chart Glyph)
- Integrates with Glyph Attestation flow

**Proposed Glyph Structure**:
```typescript
interface SemanticSearchGlyph {
  symbol: "⊨"; // Double turnstile - semantic entailment/inference
  query: string;
  results: Array<{
    text: string;
    similarity: number;
    sourceId: string;
    sourceType: string;
  }>;
  threshold: number;
}
```

**Symbol Rationale**: The double turnstile (⊨) is perfect for semantic search as it represents semantic entailment in logic - the relationship between meaning and inference, which aligns with how sentence transformers find semantically related content.

### 4. FFI Consolidation

Move common FFI patterns to shared crate:
- String marshalling (already in embeddings)
- Error handling patterns
- Memory management utilities

**Files to refactor**:
- `crates/qntx-core/src/embeddings/ffi.rs`
- Align with patterns in `crates/qntx-ffi-utils/` (if it exists)

### 5. Testing Strategy

**Current Test Status**: ✅ All backend tests passing

**Needed Tests After Merge**:
1. End-to-end with real model files
2. API integration tests (server-side focus)
3. Glyph integration tests
4. Performance benchmarks with new architecture
5. Network latency impact on UX

### 6. Frontend Implementation

**Priority Tasks**:
1. Create SemanticSearchGlyph component
2. Add to glyph palette/toolbar
3. Implement composition with other glyphs
4. Add batch embedding interface

**Integration Points**:
- Hook into new Glyph Attestation flow
- Use Meld Compositions for combining search with filters
- Leverage Chart Glyph for visualizing similarity distributions

## Risk Mitigation

### High Risk Areas
1. **FFI Changes**: The shared FFI utilities might conflict with embeddings FFI
2. **WASM Parser**: Attestation processing flow has fundamentally changed
3. **Database Migrations**: Ensure migration 019 still applies cleanly

### Mitigation Strategies
1. **Feature Flag**: Keep `rustembeddings` build tag for gradual rollout
2. **Parallel Testing**: Run old and new flows side-by-side initially
3. **Incremental Merge**: Merge main in small chunks, testing after each

## Recommended Development Sequence

1. **Phase 1: Merge & Stabilize** (2-3 days)
   - Carefully merge main into feat/sentence-transformers
   - Fix compilation issues
   - Ensure all existing tests pass

2. **Phase 2: Architecture Alignment** (1-2 days) *[Reduced - no WASM evaluation needed]*
   - Refactor FFI to use shared utilities
   - Design Semantic Search Glyph interface (⊨ symbol)
   - Ensure clean separation: WASM for parser, native for embeddings

3. **Phase 3: Glyph Implementation** (3-4 days)
   - Implement SemanticSearchGlyph with ⊨ symbol
   - Add composition capabilities with other glyphs
   - Integrate with attestation flow
   - Server-side API integration

4. **Phase 4: Testing & Polish** (2-3 days)
   - End-to-end testing with real models
   - Performance optimization (server-side caching)
   - Documentation updates

## Open Questions for Opus 4.6

1. **Model Distribution**: How should embedding models be distributed? Bundled, downloaded on-demand, or user-provided?

2. **Server-side Caching**: What's the optimal caching strategy for embeddings? Redis, in-memory, or SQLite?

3. **Multi-model Support**: Should we support multiple embedding models simultaneously?

4. **Fine-tuning**: Is there interest in domain-specific fine-tuning capabilities?

5. **Vector Database**: Should we consider dedicated vector DB (Qdrant, Weaviate) for scale?

6. **API Rate Limiting**: What are appropriate rate limits for embedding generation to prevent abuse?

7. **Batch Processing Queue**: Should batch embedding jobs use a background job queue (e.g., Redis Queue)?

## Technical Debt to Address

1. **TODO Comments**: Review and address all TODO comments in embeddings code
2. **Error Handling**: Standardize error handling across Rust/Go boundary
3. **Memory Management**: Audit FFI memory allocation/deallocation
4. **Configuration**: Add config options for model selection, dimensions, etc.

## Success Criteria

The integration is complete when:
1. ✅ Main branch merged successfully
2. ✅ All tests passing (including new WASM-based tests)
3. ✅ Semantic Search Glyph implemented and composable
4. ✅ End-to-end semantic search working in the UI
5. ✅ Performance acceptable (<100ms for search query)
6. ✅ Documentation updated for new architecture

## Contact & Resources

- Original PRs: #400 (this branch)
- Related Issues: (list any)
- Model Files: `ats/embeddings/models/` (need to be downloaded)
- Documentation: `docs/embeddings_integration_status.md`

## Final Notes for Opus 4.6

This branch contains 10 days of focused work on semantic search infrastructure. The core backend is complete and functional, but needs careful integration with the evolved main branch architecture.

**Key Decisions Already Made**:
1. **Server-only architecture** - No WASM compilation needed for embeddings
2. **⊨ (double turnstile)** as the Semantic Search Glyph symbol - represents semantic entailment
3. **sqlite-vec integration resolved** - Using CGO bindings, all tests passing

**Architecture Clarity**:
- WASM handles: Parser, core logic, browser-side operations
- Native handles: Embeddings, ONNX Runtime, vector operations
- Clear separation simplifies the integration

The main challenge is expressing semantic search as a first-class Glyph citizen (⊨) that composes well with the new Meld system. The backend foundation is solid - focus should be on the Glyph implementation and UI/UX integration.

**Time Estimate**: With server-only decision made, the integration should take approximately 8-10 days total, with Phase 2 reduced by 1-2 days.

Good luck with the integration! The semantic search capability will be a powerful addition to QNTX's attestation and graph exploration features.