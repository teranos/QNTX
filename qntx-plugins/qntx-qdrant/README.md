# qntx-qdrant

Single plugin providing both **SearchService** (ADR-015) and **VectorSearchService** (ADR-016). Qdrant's engine is linked directly into the plugin as a Rust library — there is no Qdrant binary, no child process, no sidecar (ADR-017).

## Engine: in-process

The plugin depends on Qdrant's `segment` crate via a pinned git revision of `qdrant/qdrant`. `Segment` is Qdrant's on-disk unit of storage: HNSW vector index + payload index + WAL, all in one. One `Segment` per named index is sufficient for a single-node plugin; upstream Qdrant's collection / shard / replica layers aren't linked because they aren't needed.

The `Engine` type in `src/qdrant.rs` is a plain Rust value: a map from index name to open `Segment`. It lives for the duration of the plugin process and shuts down with it.

## Services on one port

The plugin's gRPC listener serves three services that share one `Engine`:

| Service               | Source         | Status in this scaffold |
|-----------------------|----------------|-------------------------|
| DomainPluginService   | `src/service.rs` | metadata / init / shutdown / health wired |
| SearchService         | `src/search.rs`  | signatures only — keyword/BM25 mapping deferred |
| VectorSearchService   | `src/vector.rs`  | `CreateIndex` wired to `build_segment`; `Search` and `AddVectors` stubbed |

Because both search services share the same `Engine`, hybrid (dense + sparse) ranking can stay inside the engine.

## Status

What works today:

- `cargo build -p qntx-qdrant-plugin` produces a single binary with Qdrant's engine statically linked
- `Engine::open` resolves a plugin-owned state directory (`QNTX_QDRANT_STATE` env, or a per-user cache fallback), creates it if missing, and holds segments behind `Arc<RwLock<Segment>>`
- `VectorSearchService::CreateIndex` actually builds a Qdrant segment (`segment_constructor::build_segment`) with a `Distance::Cosine`, in-RAM mmap `VectorStorageType`, in-RAM payload storage — constructed on a blocking thread, registered on the engine

Still to do:

- `SearchService` method bodies (need keyword/payload-text strategy agreed)
- `VectorSearchService::Search` and `AddVectors` (segment point upsert / `SegmentEntry::search` wiring)
- Persistence across restarts — currently `Engine::open` doesn't walk the state dir and re-open existing segments
- Panel glyph for the Qdrant tray
- `InitializeResponse` provider flags — field numbers collide between the in-flight `search-service` and `vector-search-service` branches; coordinate before enabling

## Why linked, not supervised

The goal of ADR-017 is to make Qdrant disappear into the plugin. Spawning a child process or asking the user to install Qdrant would defeat that. Linking the `segment` crate is the concrete expression of "plugin managed" — the engine is a library call, not a daemon.

Top-level `Collection` / `ChannelService` / sharding / replica primitives from upstream Qdrant are intentionally not linked: they assume a cluster runtime (peer IDs, shard transfers, consensus) that doesn't exist here.
