# Handover: Review of Recent Main Changes

**Branch:** `claude/review-recent-main-changes-IwYdu`
**Date:** 2026-04-23

## Completed

### Doc fixes (committed, pushed)

Removed stale references to deleted code from five documentation files:

- `ats/lsp/README.md` — removed reference to deleted `sync/` package
- `crates/qntx-sqlite/FFI.md` — removed reference to Go `SQLStore` (now Rust)
- `docs/adr/ADR-003-plugin-communication.md` — removed reference to `sync_handler`
- `docs/arc42.md` — removed references to `sync/`, `ai/llm/`, dead display code
- `docs/security/www-readiness.md` — removed reference to `sync_handler`

These references became stale after PRs #785 (dead code removal) and #786 (embeddings extraction).

---

## Remaining Tasks

### 1. Delete `server/lsp_handler.go` (ready to act)

**File:** `server/lsp_handler.go` (528 lines)
**Test:** `server/lsp_handler_test.go`
**Route:** `server/routing.go:82` — `/lsp` → `s.HandleGLSPWebSocket`

The file header already says "Sunset candidate: serves CodeMirror AX editor, being superseded by canvas." Canvas has replaced the CodeMirror editor. Deletion requires:

1. Remove `server/lsp_handler.go` and `server/lsp_handler_test.go`
2. Remove the `/lsp` route in `server/routing.go:82`
3. Confirm no frontend code still connects to the `/lsp` WebSocket (search `web/` for `/lsp` or `ws://.*lsp`)
4. Check whether `ats/lsp/` package itself has other consumers or can also be removed

**Risk:** Low — the comment marks it as a sunset candidate, and canvas is the active editor. Verify no frontend references remain.

### 2. Finish `server/embeddings/` extraction (investigation needed)

PR #786 extracted HTTP handlers into `server/embeddings/` sub-package, but the old files remain in `server/`:

| Old file (server/) | Lines | Status |
|---|---|---|
| `embeddings_handlers.go` | 13 funcs | Still called from `server/init.go:387` (`SetupEmbeddingService`) |
| `embeddings_handlers_stub.go` | 7 funcs | Build-tag stub (non-rust builds) |
| `embeddings_cluster_handlers.go` | 1 func | `HandleEmbeddingCluster` |
| `embeddings_labeling.go` | 1 func | `setupClusterLabelSchedule` |
| `embeddings_pulse.go` | 19 funcs | `ReclusterHandler`, HDBSCAN clustering |
| `embeddings_observer_test.go` | test | Observer integration test |

**Current state:** `server/init.go` calls both old methods on `QNTXServer` (e.g., `SetupEmbeddingService`, `setupEmbeddingReclusterSchedule`) AND uses the new `serverembeddings.Handler` struct (line 388). The extraction was partial — HTTP handlers moved to `server/embeddings/`, but observer/pulse/setup logic stayed on `QNTXServer`.

**Next step:** Decide whether to move the remaining embedding logic (observer, pulse, setup) into `server/embeddings/` or a separate package. The build-tag split (`cgo && rustembeddings`) complicates this — both the real and stub implementations must move together.

### 3. Move `ai/provider/` interfaces to proto/plugin layer (needs design)

**Directory:** `ai/provider/` (4 files: `types.go`, `chat.go`, `factory.go`, `grpc_client.go`)
**Consumers:** `server/conversation.go`, `server/prompt_handlers.go`, `ats/so/actions/prompt/handler.go`

All LLM providers are gRPC plugins. The `ai/provider` package contains the gRPC client, factory, and types that bridge core ↔ plugin. These interfaces belong closer to the proto/plugin boundary rather than in an `ai/` tree.

**Considerations:**
- Three consumers reference `ai/provider` — not a large migration
- The package README confirms everything routes through `GRPCLLMClient` to plugins
- Needs a design decision on where exactly these types land (e.g., `plugin/llm/`, `proto/llm/`, or inline in `server/`)

### 4. `server/conversation.go` — no action needed

Confirmed this file belongs in `server/`. It's orchestration logic (routing conversation turns through providers), not domain logic. Stays where it is.

---

## Context: Recent PRs Reviewed

| PR | Summary |
|---|---|
| #785 | Removed dead packages: `display/`, `ai/llm/`, `am/geotime/`, `ats/so/actions/csv/` |
| #786 | Extracted embedding HTTP handlers → `server/embeddings/` sub-package |
| #787 | Namespaced plugin handler names to prevent registry collisions |

The codebase is in an active cleanup phase. #785 and #786 removed/reorganized significant dead code, but left some loose ends (the items above).
