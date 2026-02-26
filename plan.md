# Plan: Switch Attestation Storage from Go SQLStore to Rust qntx-sqlite

## Decisions

- **`signature`/`signer_did`**: Add to `qntx-core::Attestation` as `signature: Option<Vec<u8>>`, `signer_did: Option<String>`
- **Eviction telemetry**: Rust returns `EvictionResult` via FFI, Go logs to `storage_events` — Rust doesn't touch the logger or telemetry table
- **`BatchPersister`**: Add `storage_put_batch` FFI for bulk ingest through Rust
- **`SQLQueryStore`**: Stays on Go `*sql.DB` for now, eventually moves to Rust

## Current State

- **Go `SQLStore`** (`ats/storage/sql_store.go`) handles all attestation CRUD at runtime
- **Rust `SqliteStore`** (`crates/qntx-sqlite/src/store.rs`) is compiled but only used in tests
- **Go `BoundedStore`** (`ats/storage/bounded_store.go`) wraps SQLStore with eviction-based 16/64/64 enforcement
- **Rust `BoundedStore`** (`crates/qntx-sqlite/src/bounded.rs`) uses rejection-based global quotas — wrong strategy
- Two connections will coexist: Rust owns attestation CRUD, Go keeps `*sql.DB` for `SQLQueryStore`, `EmbeddingStore`, `SymbolIndex`, migrations

## What Changes

### Phase 1: Add signature/signer_did to qntx-core, rewrite Rust BoundedStore

**qntx-core changes:**
- Add `signature: Option<Vec<u8>>` and `signer_did: Option<String>` to `Attestation`
- Update `AttestationBuilder` to support these fields
- Update serialization/deserialization

**Rewrite Rust BoundedStore** — replace rejection-based quotas with Go's post-insert eviction model:

1. **16 attestations per (actor, context) pair** — after insert, count attestations matching this actor+context. If > limit, delete oldest by timestamp.
2. **64 contexts per actor** — after insert, count distinct context arrays for this actor. If > limit, delete all attestations for least-used contexts.
3. **64 actors per entity/subject** — after insert, count distinct actors for this subject. If > limit, delete all attestations for least-recent actors.

Rust `BoundedStore::put()` becomes: insert → enforce limits → return eviction details.

```rust
pub struct EvictionResult {
    pub actor_context_evictions: usize,
    pub actor_contexts_evictions: usize,
    pub entity_actors_evictions: usize,
}
```

Configurable limits pass through at construction:

```rust
pub struct BoundedConfig {
    pub actor_context_limit: usize,   // default 16
    pub actor_contexts_limit: usize,  // default 64
    pub entity_actors_limit: usize,   // default 64
}
```

Files changed:
- `crates/qntx-core/` — add signature/signer_did to Attestation
- `crates/qntx-sqlite/src/bounded.rs` — rewrite enforcement logic
- `crates/qntx-sqlite/src/store.rs` — handle signature/signer_did columns in SQL

### Phase 2: Expand FFI surface

**Discovery queries** (already in Rust, not exposed via FFI):
- `storage_predicates()` → `StringArrayResultC`
- `storage_contexts()` → `StringArrayResultC`
- `storage_subjects()` → `StringArrayResultC`
- `storage_actors()` → `StringArrayResultC`
- `storage_stats()` → new `StatsResultC`

**Bounded store FFI:**
- `storage_new_bounded_memory(config)` / `storage_new_bounded_file(path, config)`
- `storage_put_bounded(store, json)` → new `PutResultC` containing success + `EvictionResult`

**Batch FFI:**
- `storage_put_batch(store, json_array)` → `BatchResultC` with persisted count, failure count, per-item errors, and aggregate eviction details

Files changed:
- `crates/qntx-sqlite/src/ffi.rs` — all new FFI functions

### Phase 3: Expand Go CGO wrapper to implement full interfaces

`RustStore` implements `ats.AttestationStore` and `ats.BoundedStore`:

```go
// ats.AttestationStore
func (r *RustStore) CreateAttestation(as *types.As) error       // sign → put_bounded → notify observers → log evictions
func (r *RustStore) CreateAttestationInbound(as *types.As) error // put_bounded (no sign) → notify observers → log evictions
func (r *RustStore) AttestationExists(asid string) bool
func (r *RustStore) GenerateAndCreateAttestation(ctx, cmd) (*types.As, error) // vanity ID gen stays in Go
func (r *RustStore) GetAttestations(filters) ([]*types.As, error)

// ats.BoundedStore
func (r *RustStore) CreateAttestationWithLimits(cmd) (*types.As, error)
func (r *RustStore) GetStorageStats() (*ats.StorageStats, error)

// ats.BatchStore
func (r *RustStore) PersistItems(items []AttestationItem, sourcePrefix string) *PersistenceResult
```

Signing (`getDefaultSigner().Sign()`) and observer notification (`notifyObservers()`) stay in the Go wrapper. Eviction telemetry: Go receives `EvictionResult` from FFI and writes to `storage_events`.

Files changed:
- `ats/storage/sqlitecgo/storage_cgo.go` — implement full interfaces
- `ats/storage/sqlitecgo/adapter.go` — handle signature/signer_did in JSON conversion

### Phase 4: Wire up at initialization

Replace all `NewSQLStore`/`NewBoundedStore` call sites with a shared `RustStore`:

| Call site | Current | After |
|---|---|---|
| `server/init.go:228,243,397` | `NewSQLStore(db, logger)` | shared `RustStore` |
| `server/sync_handler.go:44,133,341` | `NewSQLStore(db, logger)` | shared `RustStore` |
| `server/embeddings_labeling.go:63` | `NewSQLStore(db, logger)` | shared `RustStore` |
| `server/prompt_handlers.go:582` | `NewSQLStore(db, logger)` | shared `RustStore` |
| `server/client.go:815,931` | `NewBoundedStore(db, logger)` | shared `RustStore` |
| `server/embeddings_handlers.go:308,738` | `NewBoundedStore(db, logger)` | shared `RustStore` |
| `server/handlers_attestations.go:81` | `NewBoundedStore(db, logger)` | shared `RustStore` |

`RustStore` created once during server init, passed to handlers. Rust opens its own SQLite connection to the same database file.

**Stays on Go `*sql.DB` (this phase):**
- `SQLQueryStore` (NL expansion, OverComparison, rich query builder) — moves to Rust later
- `AliasStore` / `AliasResolver`
- `EmbeddingStore` / vector search
- `SymbolIndex` / LSP
- Migrations
- `storage_events` telemetry writes (Go receives eviction results from Rust, writes them)

Files changed:
- `server/init.go` — create `RustStore` during startup, pass to handlers
- `server/server.go` or equivalent — hold `RustStore` reference
- All call sites above — accept `ats.AttestationStore` or `ats.BoundedStore` interface

### Phase 5: Delete dead Go code

Remove:
- `ats/storage/sql_store.go` — replaced by Rust
- `ats/storage/bounded_store.go` — enforcement moved to Rust
- `ats/storage/bounded_store_enforcement.go` — enforcement moved to Rust
- `ats/storage/bounded_store_config.go` — config now passes through FFI
- `ats/storage/bounded_store_telemetry.go` — telemetry logging moves to `RustStore` Go wrapper
- `ats/storage/bounded_store_warnings.go` — warnings move to `RustStore` Go wrapper
- `ats/storage/batch_persister.go` (or equivalent) — replaced by Rust batch FFI

Keep:
- `ats/storage/observer.go` — still used by Go wrapper
- `ats/storage/query_store.go` — still uses Go `*sql.DB` (moves to Rust later)
- `ats/storage/query_builder.go` — still uses Go `*sql.DB` (moves to Rust later)

## Dual-Connection Model

- SQLite WAL mode: concurrent readers + serialized writers
- Rust writes: attestation CRUD + eviction deletes
- Go writes: `storage_events`, embeddings, symbol index, migrations
- Both sides: 5s busy timeout
- Long-term: `SQLQueryStore` and `BatchPersister` move to Rust, narrowing Go's surface to embeddings/LSP/migrations
