# Backend Coupling Analysis

## Overview

Analysis of inter-package coupling in the Go backend, measured by fan-in (how many packages depend on it) and fan-out (how many packages it depends on).

### Highest Fan-In (most depended-upon)

| Package | Fan-In | Role |
|---------|--------|------|
| `errors` | 37 | Error wrapping |
| `ats/types` | 16 | Core type definitions |
| `logger` | 12 | Structured logging |
| `ats` | 12 | Interface contracts |
| `ats/storage` | 12 | Persistence layer |

### Highest Fan-Out (most dependencies)

| Package | Fan-Out | Role |
|---------|---------|------|
| `server` | 25 | WebSocket/HTTP server |
| `cmd/qntx/commands` | 25 | CLI command handlers |
| `ats/storage` | 12 | Persistence (imports ax, alias, fuzzyax) |
| `ats/so/actions/prompt` | 12 | Prompt action handler |

---

## 5 Actionable Suggestions

### 1. Extract executor assembly out of `ats/storage`

**Problem:** `ats/storage` imports `ats/ax` and `ats/alias` solely to provide the convenience functions `NewExecutor` and `NewExecutorWithOptions` in `executor_factory.go`. This pulls the query execution layer into the persistence layer, creating a dependency from storage → ax → alias that doesn't belong there. `rich_search.go` also imports `ats/ax` to call `ax.NewDefaultMatcher()` for fuzzy backend detection.

**Files:**
- `ats/storage/executor_factory.go:7-9` — imports `ats/alias` and `ats/ax`
- `ats/storage/rich_search.go:12-13` — imports `ats/ax` and `ats/ax/fuzzy-ax/fuzzyax`

**Action:** Move `executor_factory.go` to a new package (e.g., `ats/setup` or just lift it into the callers that use it — `server/`, `cmd/`, `graph/`). For `rich_search.go`, inject the matcher via a field on `BoundedStore` at construction time rather than calling `ax.NewDefaultMatcher()` inline. This makes `ats/storage` a pure persistence package with zero knowledge of the query layer.

**Impact:** Removes 2 import edges (`storage → ax`, `storage → alias`), drops storage fan-out from 12 to ~9.

---

### 2. Define local interfaces in `server` for its dependencies

**Problem:** `QNTXServer` holds 15+ concrete types from external packages (`*graph.AxGraphBuilder`, `*budget.Tracker`, `*async.WorkerPool`, `*schedule.Ticker`, `*plugin.Registry`, `*lsp.Service`, `*tracker.UsageTracker`, `*vidstream.VideoEngine`, `*watcher.Engine`, `*handlers.CanvasHandler`, etc.). Every field is a concrete pointer — no interfaces. This means the server package has compile-time coupling to the full API surface of every dependency, and there is no seam for testing any handler in isolation.

**File:** `server/server.go:30-82` — the `QNTXServer` struct definition.

**Action:** For each dependency consumed by the server, define a narrow interface in `server/` that covers only the methods actually called. For example:

```
// server/interfaces.go

type GraphBuilder interface {
    BuildFromQuery(ctx context.Context, query string, opts BuildOptions) (*graph.Graph, error)
}

type BudgetTracker interface {
    GetStatus() budget.Status
    UpdateDailyBudget(amount int)
}

type JobPool interface {
    Submit(job async.Job) error
    GetQueue() []async.Job
}
```

Then change `QNTXServer` fields from `*graph.AxGraphBuilder` to `GraphBuilder`, etc. Production code passes the real implementations; tests can pass stubs.

**Impact:** Decouples server from 15 packages at compile time. Makes individual handlers testable without constructing the entire server. Does not require changes in any other package — Go's implicit interface satisfaction means existing types already satisfy the new interfaces.

---

### 3. Replace `client.go` message switch with a handler registry

**Problem:** `client.go` contains a `routeMessage()` switch statement with 20+ cases (`"query"`, `"clear"`, `"daemon_control"`, `"rich_search"`, `"vidstream_init"`, `"watcher_upsert"`, etc.). Each case calls into a different subsystem. Adding a new message type requires modifying this central switch, which touches every import in the file and creates merge conflicts when features are developed in parallel.

**File:** `server/client.go:225-261` — the `routeMessage()` switch.

**Action:** Define a `MessageHandler` interface and a registry map:

```
type MessageHandler interface {
    HandleMessage(ctx context.Context, client *Client, msg json.RawMessage) error
}

// In server setup:
s.handlers["query"]          = &queryHandler{builder: s.builder}
s.handlers["daemon_control"] = &daemonHandler{pool: s.daemon}
s.handlers["rich_search"]    = &searchHandler{store: s.boundedStore}
```

Each handler lives in its own file (or even sub-package) and only imports the packages it needs. `routeMessage()` becomes a single map lookup + dispatch.

**Impact:** Each handler has a focused dependency set instead of every message type sharing one file's imports. New message types are additive (register a handler) rather than requiring a central code change.

---

### 4. Split `broadcast.go` concerns: event broadcasting vs. daemon polling

**Problem:** `broadcast.go` (868 lines) mixes two distinct responsibilities: (a) broadcasting graph updates and status to WebSocket clients, and (b) polling the daemon/pulse subsystem for job state changes (`startJobUpdateBroadcaster`, `startDaemonStatusBroadcaster`, `startScheduleBroadcaster`, `startUsageBroadcaster`, `startStorageEventsBroadcaster`). The polling logic directly instantiates `async.Job`, `schedule.Store`, and `budget.Status` types, coupling the broadcast layer to the full pulse subsystem.

**File:** `server/broadcast.go` — 868 lines mixing client broadcast mechanics with daemon/pulse/usage polling.

**Action:** Extract the polling goroutines into a separate file or package (e.g., `server/pollers.go` or `server/status/`). Each poller should accept an interface (e.g., `JobStatusProvider`, `BudgetStatusProvider`) rather than concrete pulse types. The poller emits a generic status update to the broadcast channel; the broadcast worker remains a simple fan-out to clients.

**Impact:** `broadcast.go` shrinks to pure broadcast mechanics (~200 lines). Pollers become independently testable. Pulse package changes no longer ripple into broadcast logic.

---

### 5. Decouple `ats/storage/rich_search.go` from fuzzy engine internals

**Problem:** `rich_search.go` (750 lines) directly imports `ats/ax/fuzzy-ax/fuzzyax`, creates a `fuzzyax.NewFuzzyEngine()`, manages its lifecycle (`defer engine.Close()`), builds vocabulary, and calls `engine.FindMatches()` and `engine.RebuildIndex()`. This embeds the full Rust FFI fuzzy matching engine inside the storage layer — a persistence package now owns vocabulary construction and fuzzy scoring logic.

**Files:**
- `ats/storage/rich_search.go:13` — imports `fuzzyax`
- `ats/storage/rich_search.go:332-336` — creates and manages fuzzy engine lifecycle
- `ats/storage/rich_search.go:462-465` — rebuilds fuzzy index
- `ats/storage/rich_search.go:492` — calls `engine.FindMatches()`

**Action:** Define a `FuzzyMatcher` interface that `BoundedStore` accepts at construction:

```
type FuzzyMatcher interface {
    Match(query string, vocabulary []string, limit int, threshold float64) ([]FuzzyMatch, error)
}
```

The Rust-backed implementation lives in `ats/ax/fuzzy-ax/`. The storage layer calls `matcher.Match()` without knowing about engine lifecycle, vocabulary indexing, or FFI details. A no-op matcher can be injected for tests or when Rust is unavailable (replacing the current runtime check for `ax.MatcherBackendRust`).

**Impact:** Removes `fuzzyax` import from storage. Storage tests no longer require the Rust WASM binary. Fuzzy matching can evolve independently (swap implementations, add caching) without modifying storage.

---

## Summary

| # | Suggestion | Edges Removed | Primary Benefit |
|---|-----------|---------------|-----------------|
| 1 | Move executor factory out of storage | 2 | Clean persistence boundary |
| 2 | Server-local interfaces | 15 | Testability, compile-time decoupling |
| 3 | Handler registry in client.go | N/A (structural) | Extensibility, reduced merge conflicts |
| 4 | Split broadcast.go | 3-4 | Separation of concerns |
| 5 | Inject fuzzy matcher into storage | 2 | Storage independent of Rust FFI |
