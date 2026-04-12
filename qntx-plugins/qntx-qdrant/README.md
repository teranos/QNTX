# qntx-qdrant

Single plugin providing both **SearchService** (ADR-015) and **VectorSearchService** (ADR-016), backed by a Qdrant instance that the plugin manages end-to-end (ADR-017).

## Plugin-managed contract

The whole point of picking Qdrant over separate MeiliSearch + FAISS plugins is that one engine covers both jobs. The only way that payoff is real is if the plugin is a drop-in: no Qdrant install, no sidecar daemon, no user-touched storage.

This plugin owns:

- **the binary** — supplied by the plugin's nix flake via `QNTX_QDRANT_BINARY`; the plugin refuses to fall back to `PATH`.
- **the data directory** — under plugin state (`QNTX_QDRANT_STATE` or a per-user cache default); not a user-chosen path.
- **the listen port** — ephemeral loopback, rolled on each startup; Qdrant is not reachable from outside the plugin process.
- **the lifecycle** — `Supervisor::start` blocks until Qdrant is ready; `shutdown` tears it down before the plugin exits. If Qdrant can't start, the plugin exits non-zero instead of serving degraded RPCs.

To a QNTX deployment, Qdrant does not exist as a separate thing to operate.

## Services on one port

The plugin's gRPC listener serves three services:

| Service               | Source         | Status in this scaffold |
|-----------------------|----------------|-------------------------|
| DomainPluginService   | `src/service.rs` | metadata / init / shutdown / health wired; panel glyph deferred |
| SearchService         | `src/search.rs`  | signatures only — Qdrant mapping is a follow-up |
| VectorSearchService   | `src/vector.rs`  | signatures only — Qdrant mapping is a follow-up |

Both search services share the same `Supervisor`, so both route to the single managed Qdrant. That shared backend is what lets hybrid (BM25 + dense) ranking stay inside the engine.

## Supervision modes

`src/qdrant.rs` exposes `Mode::ChildProcess` (the default, spawns the bundled binary) and `Mode::Embedded` (deferred — Qdrant does not expose a stable library surface today). The service layer is written against `Supervisor` so the two modes are interchangeable.

## Status

This is a scaffold. What is in place:

- workspace registration (`Cargo.toml`)
- `search.proto` and `vectorsearch.proto` wired through `qntx-proto` and `qntx-grpc` builds
- plugin binary that brings up managed Qdrant, then serves all three gRPC services
- `Supervisor` with child-process lifecycle, ephemeral loopback port, managed data dir

What is not yet done:

- `SearchService` / `VectorSearchService` method bodies (return `Unimplemented`)
- Readiness probe via `qdrant_client::health_check` (currently a TCP-connect probe)
- Panel glyph for the Qdrant tray
- `InitializeResponse` provider flags (`search_provider`, `vector_search_provider`) — field numbers need coordinating with in-flight branches `search-service` and `vector-search-service` before this plugin can register as the backend via the core service mesh
- Nix flake to supply the `qdrant` binary (plugin expects `QNTX_QDRANT_BINARY`)
- `qdrant-client` crate version pin aligned with the bundled binary
