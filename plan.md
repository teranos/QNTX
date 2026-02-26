# Plan: Switch Attestation Storage from Go SQLStore to Rust qntx-sqlite

## Decisions

- **`signature`/`signer_did`**: Added to `qntx-core::Attestation` as `signature: Option<Vec<u8>>`, `signer_did: Option<String>` — Phase 1 complete
- **Eviction telemetry**: Rust returns `EvictionResult` via FFI, Go logs to `storage_events` — Rust doesn't touch the logger or telemetry table
- **`BatchPersister`**: Add `storage_put_batch` FFI for bulk ingest through Rust
- **`SQLQueryStore`**: Stays on Go `*sql.DB` for now, eventually moves to Rust
- **Store-per-operation pattern**: Go currently creates `NewSQLStore`/`NewBoundedStore` per handler call from `*sql.DB`. We replace this with a single shared `*RustStore` created at server startup and passed to handlers.

## Current State (after Phase 1)

- **Rust `Attestation`** now has `signature`/`signer_did` — matches Go's `types.As`
- **Rust `SqliteStore`** reads/writes all 11 columns (including signature/signer_did) — migration 040 embedded
- **Rust `BoundedStore`** uses post-insert eviction (16/64/64) matching Go strategy — returns `EvictionResult`
- **Go `SQLStore`** still handles all runtime CRUD — not yet wired to Rust
- **Go `RustStore`** (`ats/storage/sqlitecgo/storage_cgo.go`) exists with basic CRUD methods but is missing: `CreateAttestationInbound`, bounded store methods, batch persistence, signature/signer_did in JSON adapter
- **Rust FFI** (`ffi.rs`) has basic CRUD + query but is missing: discovery queries, bounded store lifecycle, bounded put, batch put

## What Changes

### Phase 1: Add signature/signer_did to qntx-core, rewrite Rust BoundedStore -- DONE

Delivered:
- `signature: Option<Vec<u8>>` and `signer_did: Option<String>` on `Attestation` with builder methods
- `SqliteStore` INSERT/SELECT/UPDATE include both columns; migration 040 embedded
- `BoundedStore` rewritten: `BoundedConfig` (actor_context_limit, actor_contexts_limit, entity_actors_limit) replaces `StorageQuotas`; `put_bounded()` returns `EvictionResult`
- `qntx-proto` conversion passes through signature/signer_did instead of defaulting
- 52 Rust tests pass

Files changed:
- `crates/qntx-core/src/attestation/types.rs`
- `crates/qntx-sqlite/src/bounded.rs`
- `crates/qntx-sqlite/src/store.rs`
- `crates/qntx-sqlite/src/migrate.rs`
- `crates/qntx-sqlite/src/lib.rs`
- `crates/qntx-proto/src/proto_convert.rs`
- `crates/qntx-proto/src/test.rs`

### Phase 2: Expand FFI surface

Existing FFI (`ffi.rs`) already exposes:
- Store lifecycle: `storage_new_memory`, `storage_new_file`, `storage_free`
- CRUD: `storage_put`, `storage_get`, `storage_exists`, `storage_delete`, `storage_update`
- Query: `storage_query`, `storage_ids`, `storage_count`, `storage_clear`
- Memory: various `*_free` functions

**Add discovery queries** (Rust has these on `QueryStore`, not exposed via FFI):
- `storage_predicates(store)` → `StringArrayResultC`
- `storage_contexts(store)` → `StringArrayResultC`
- `storage_subjects(store)` → `StringArrayResultC`
- `storage_actors(store)` → `StringArrayResultC`
- `storage_stats(store)` → new `StatsResultC` struct

**Add bounded store FFI:**
- `storage_new_bounded_memory(config_json)` → `*mut BoundedStore`
- `storage_new_bounded_file(path, config_json)` → `*mut BoundedStore`
- `storage_free_bounded(store)` → void
- `storage_put_bounded(store, json)` → `PutBoundedResultC` containing success flag + serialized `EvictionResult` (eviction counts per dimension)

**Add batch FFI:**
- `storage_put_batch(store, json_array)` → `BatchResultC` with persisted count, failure count, per-item error strings, aggregate eviction details

**New C-compatible result types:**
```rust
#[repr(C)]
pub struct StatsResultC {
    success: bool,
    total_attestations: usize,
    unique_subjects: usize,
    unique_predicates: usize,
    unique_contexts: usize,
    unique_actors: usize,
    error: *mut c_char,
}

#[repr(C)]
pub struct PutBoundedResultC {
    success: bool,
    actor_context_evictions: usize,
    actor_contexts_evictions: usize,
    entity_actors_evictions: usize,
    error: *mut c_char,
}

#[repr(C)]
pub struct BatchResultC {
    success: bool,
    persisted_count: usize,
    failure_count: usize,
    errors_json: *mut c_char, // JSON array of error strings
    actor_context_evictions: usize,
    actor_contexts_evictions: usize,
    entity_actors_evictions: usize,
    error: *mut c_char,
}
```

Files changed:
- `crates/qntx-sqlite/src/ffi.rs` — all new FFI functions + result types

### Phase 3: Expand Go CGO wrapper to implement full interfaces

**Go interfaces to satisfy** (from `ats/store.go`):

```go
type AttestationStore interface {
    CreateAttestation(as *types.As) error
    CreateAttestationInbound(as *types.As) error
    AttestationExists(asid string) bool
    GenerateAndCreateAttestation(ctx context.Context, cmd *types.AsCommand) (*types.As, error)
    GetAttestations(filters AttestationFilter) ([]*types.As, error)
}

type BoundedStore interface {
    AttestationStore
    CreateAttestationWithLimits(cmd *types.AsCommand) (*types.As, error)
    GetStorageStats() (*StorageStats, error)
}

type BatchStore interface {
    PersistItems(items []AttestationItem, sourcePrefix string) *PersistenceResult
}
```

**RustStore already implements:**
- `CreateAttestation` — exists but currently calls plain `storage_put`, needs to call `storage_put_bounded` instead
- `AttestationExists` — done
- `GenerateAndCreateAttestation` — done (vanity ID gen stays in Go)
- `GetAttestations` — done
- `GetAttestation`, `UpdateAttestation`, `ListAttestationIDs`, `CountAttestations` — done (extra methods)

**RustStore needs added:**
- `CreateAttestationInbound(as *types.As) error` — calls `storage_put_bounded` without signing; used for synced attestations
- `CreateAttestationWithLimits(cmd *types.AsCommand) (*types.As, error)` — full pipeline: build As from command, sign, put_bounded, notify observers, log evictions
- `GetStorageStats() (*ats.StorageStats, error)` — calls `storage_stats`
- `PersistItems(items []AttestationItem, sourcePrefix string) *PersistenceResult` — calls `storage_put_batch`

**JSON adapter update** (`adapter.go`):
- Add `Signature` and `SignerDID` to `rustAttestation` struct
- `toRustJSON`: convert `[]byte` signature to base64 or pass through as bytes
- `fromRustJSON`: convert back

**Signing flow** — stays in Go wrapper:
- `CreateAttestation`: Go signs (`getDefaultSigner().Sign()`), sets signature/signer_did on As, then calls Rust `storage_put_bounded`
- `CreateAttestationInbound`: Go passes through existing signature/signer_did to Rust (no re-signing)
- Observer notification (`notifyObservers`) stays in Go after Rust returns

**Eviction telemetry** — Go wrapper:
- Rust `PutBoundedResultC` returns eviction counts
- Go logs them to `storage_events` table using existing `*sql.DB`

**Store lifecycle change:**
- `RustStore` holds a `*C.BoundedStore` instead of `*C.SqliteStore`
- Constructor accepts config: `NewRustStore(path string, config BoundedConfig) (*RustStore, error)`
- Also holds `db *sql.DB` and `logger *zap.SugaredLogger` for Go-side telemetry/observer operations

Files changed:
- `ats/storage/sqlitecgo/storage_cgo.go` — add missing interface methods, switch to bounded store pointer
- `ats/storage/sqlitecgo/adapter.go` — add signature/signer_did to JSON adapter
- `ats/storage/sqlitecgo/storage_cgo_test.go` — update tests

### Phase 4: Wire up at initialization

**Pattern change**: Go currently creates stores per-operation (`NewSQLStore(db, logger)` or `NewBoundedStore(db, logger)` in each handler). We replace this with a single `*RustStore` created at server startup.

**Server startup** (`server/init.go`):
```go
rustStore, err := sqlitecgo.NewRustStore(dbPath, boundedConfig)
server.rustStore = rustStore
```

**Replace call sites** — anywhere `NewSQLStore(db, logger)` or `NewBoundedStore(db, logger)` is called in server code, use the shared `server.rustStore` instead:

| File | Current | After |
|---|---|---|
| `server/init.go:228,243,397` | `NewSQLStore(db, logger)` | `server.rustStore` |
| `server/sync_handler.go:44,133,341` | `NewSQLStore(db, logger)` | `server.rustStore` |
| `server/embeddings_labeling.go:63` | `NewSQLStore(db, logger)` | `server.rustStore` |
| `server/prompt_handlers.go:582` | `NewSQLStore(db, logger)` | `server.rustStore` |
| `server/client.go:815,931` | `NewBoundedStore(db, logger)` | `server.rustStore` |
| `server/embeddings_handlers.go:308,738` | `NewBoundedStore(db, logger)` | `server.rustStore` |
| `server/handlers_attestations.go:81` | `NewBoundedStore(db, logger)` | `server.rustStore` |
| `cmd/qntx/commands/as.go:97` | `NewBoundedStoreWithConfig(...)` | `sqlitecgo.NewRustStore(...)` |
| `cmd/qntx/commands/handler.go:88` | `NewBoundedStoreWithConfig(...)` | `sqlitecgo.NewRustStore(...)` |

**Non-server call sites** (these create their own stores, not from server):
- `ats/so/actions/prompt/store.go:41` — `NewSQLStore` for PromptStore, uses it as `AttestationStore`
- `qntx-code/ixgest/git/ingest.go:102` — `NewSQLStore` for GitIxProcessor
- `qntx-code/ixgest/git/deps.go:64` — `NewSQLStore` for DepsIxProcessor
- `plugin/grpc/` — plugin integration, may need `RustStore` option

**Stays on Go `*sql.DB`:**
- `SQLQueryStore` (NL expansion, OverComparison, rich query builder)
- `AliasStore` / `AliasResolver`
- `EmbeddingStore` / vector search
- `SymbolIndex` / LSP
- Migrations
- `storage_events` telemetry writes (Go receives eviction results from Rust, writes them)
- Test files (dozens of test files use `NewSQLStore`/`NewBoundedStore` directly — these migrate last or stay on Go for test isolation)

Files changed:
- `server/server.go` — add `rustStore *sqlitecgo.RustStore` field
- `server/init.go` — create `RustStore` during startup
- All server call sites above
- `cmd/qntx/commands/as.go`, `cmd/qntx/commands/handler.go`

### Phase 5: Delete dead Go code

Remove (only after Phase 4 is verified working):
- `ats/storage/sql_store.go` — CRUD replaced by Rust
- `ats/storage/bounded_store.go` — eviction moved to Rust
- `ats/storage/bounded_store_enforcement.go` — enforcement moved to Rust
- `ats/storage/bounded_store_config.go` — config now passes through FFI
- `ats/storage/bounded_store_telemetry.go` — telemetry logging moves to RustStore Go wrapper
- `ats/storage/bounded_store_warnings.go` — warnings move to RustStore Go wrapper
- `ats/storage/batch.go` — replaced by Rust batch FFI

Keep:
- `ats/storage/observer.go` — still used by Go wrapper for `notifyObservers()`
- `ats/storage/query_store.go` — still uses Go `*sql.DB` (moves to Rust later)
- `ats/storage/query_builder.go` — still uses Go `*sql.DB` (moves to Rust later)
- `ats/storage/lsp_index.go` — LSP symbol indexing, stays on Go
- Test files — migrate incrementally or keep for regression

## Dual-Connection Model

- SQLite WAL mode: concurrent readers + serialized writers
- Rust writes: attestation CRUD + eviction deletes (via BoundedStore)
- Go writes: `storage_events`, embeddings, symbol index, migrations
- Both sides: 5s busy timeout
- Long-term: `SQLQueryStore` and `BatchPersister` move to Rust, narrowing Go's surface to embeddings/LSP/migrations
