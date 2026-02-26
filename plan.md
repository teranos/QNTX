# Plan: Switch Attestation Storage from Go SQLStore to Rust qntx-sqlite

## Current State

- **Go `SQLStore`** (`ats/storage/sql_store.go`) handles all attestation CRUD at runtime
- **Rust `SqliteStore`** (`crates/qntx-sqlite/src/store.rs`) is compiled but only used in tests
- **Go `BoundedStore`** (`ats/storage/bounded_store.go`) wraps SQLStore with eviction-based 16/64/64 enforcement
- **Rust `BoundedStore`** (`crates/qntx-sqlite/src/bounded.rs`) uses rejection-based global quotas — wrong strategy
- Two connections will coexist: Rust owns attestation CRUD, Go keeps `*sql.DB` for `SQLQueryStore`, `EmbeddingStore`, `SymbolIndex`, migrations

## What Changes

### Phase 1: Rewrite Rust BoundedStore to match Go's eviction strategy

Replace the rejection-based Rust `BoundedStore` with Go's post-insert eviction model:

1. **16 attestations per (actor, context) pair** — after insert, count attestations matching this actor+context. If > limit, delete oldest by timestamp.
2. **64 contexts per actor** — after insert, count distinct context arrays for this actor. If > limit, delete all attestations for least-used contexts.
3. **64 actors per entity/subject** — after insert, count distinct actors for this subject. If > limit, delete all attestations for least-recent actors.

Rust `BoundedStore::put()` becomes: insert → enforce limits → return eviction details.

Add `EvictionResult` struct to FFI so Go can log telemetry (Rust doesn't have the logger):

```rust
pub struct EvictionResult {
    pub actor_context_evictions: usize,
    pub actor_contexts_evictions: usize,
    pub entity_actors_evictions: usize,
}
```

The configurable limits (`BoundedStoreConfig` equivalent) pass through FFI at store construction.

Files changed:
- `crates/qntx-sqlite/src/bounded.rs` — rewrite enforcement logic
- `crates/qntx-sqlite/src/ffi.rs` — add `storage_new_bounded_memory`, `storage_new_bounded_file`, `storage_put_bounded` (returns eviction info)

### Phase 2: Expand FFI surface for missing operations

Add FFI functions for discovery queries already implemented in Rust but not exposed:

- `storage_predicates()` → `Vec<String>` via `StringArrayResultC`
- `storage_contexts()` → `Vec<String>` via `StringArrayResultC`
- `storage_subjects()` → `Vec<String>` via `StringArrayResultC`
- `storage_actors()` → `Vec<String>` via `StringArrayResultC`
- `storage_stats()` → `StorageStats` via new `StatsResultC`

Add FFI for signature fields (currently not in Rust's Attestation type):
- `signature: Option<Vec<u8>>` and `signer_did: Option<String>` need to be added to Rust's attestation model (in `qntx-core` or handled at the adapter layer)

Files changed:
- `crates/qntx-sqlite/src/ffi.rs` — new FFI functions
- `crates/qntx-core/` — may need `signature`/`signer_did` fields on Attestation (or handle in Go adapter)

### Phase 3: Expand Go CGO wrapper to implement full interfaces

Make `RustStore` implement `ats.AttestationStore` and `ats.BoundedStore`:

```go
type RustStore struct {
    store *C.SqliteStore  // Rust pointer (bounded variant)
}

// ats.AttestationStore
func (r *RustStore) CreateAttestation(as *types.As) error       // sign → put_bounded → notify observers → log evictions
func (r *RustStore) CreateAttestationInbound(as *types.As) error // put_bounded (no sign) → notify observers → log evictions
func (r *RustStore) AttestationExists(asid string) bool
func (r *RustStore) GenerateAndCreateAttestation(ctx, cmd) (*types.As, error) // vanity ID gen stays in Go
func (r *RustStore) GetAttestations(filters) ([]*types.As, error)

// ats.BoundedStore
func (r *RustStore) CreateAttestationWithLimits(cmd) (*types.As, error)
func (r *RustStore) GetStorageStats() (*ats.StorageStats, error)
```

Signing (`getDefaultSigner().Sign()`) and observer notification (`notifyObservers()`) stay in the Go wrapper — they're Go-side concerns that wrap the Rust CRUD.

Files changed:
- `ats/storage/sqlitecgo/storage_cgo.go` — implement full interface
- `ats/storage/sqlitecgo/adapter.go` — handle signature/signer_did fields in JSON conversion

### Phase 4: Wire up at initialization

Replace all `storage.NewSQLStore(db, logger)` and `storage.NewBoundedStore(db, logger)` call sites with `sqlitecgo.NewFileStore(dbPath)`:

| Call site | Current | After |
|---|---|---|
| `server/init.go:228,243,397` | `NewSQLStore(db, logger)` | shared `RustStore` |
| `server/sync_handler.go:44,133,341` | `NewSQLStore(db, logger)` | shared `RustStore` |
| `server/embeddings_labeling.go:63` | `NewSQLStore(db, logger)` | shared `RustStore` |
| `server/prompt_handlers.go:582` | `NewSQLStore(db, logger)` | shared `RustStore` |
| `server/client.go:815,931` | `NewBoundedStore(db, logger)` | shared `RustStore` |
| `server/embeddings_handlers.go:308,738` | `NewBoundedStore(db, logger)` | shared `RustStore` |
| `server/handlers_attestations.go:81` | `NewBoundedStore(db, logger)` | shared `RustStore` |

The `RustStore` should be created once during server init and passed to handlers, not constructed per-request. The Rust side opens its own SQLite connection to the same database file.

**Stays on Go `*sql.DB`:**
- `SQLQueryStore` (NL expansion, OverComparison, rich query builder)
- `AliasStore` / `AliasResolver`
- `EmbeddingStore` / vector search
- `SymbolIndex` / LSP
- Migrations
- `storage_events` telemetry table writes
- `BatchPersister`

Files changed:
- `server/init.go` — create `RustStore` during startup, pass to handlers
- `server/server.go` or equivalent — hold `RustStore` reference
- All call sites above — accept `ats.AttestationStore` or `ats.BoundedStore` interface

### Phase 5: Delete dead Go code

Once the switch is verified, remove:
- `ats/storage/sql_store.go` — replaced by Rust
- `ats/storage/bounded_store.go` — enforcement moved to Rust
- `ats/storage/bounded_store_enforcement.go` — enforcement moved to Rust
- `ats/storage/bounded_store_config.go` — config now passes through FFI
- `ats/storage/bounded_store_telemetry.go` — telemetry logging moves to `RustStore` wrapper
- `ats/storage/bounded_store_warnings.go` — warnings move to `RustStore` wrapper

Keep:
- `ats/storage/observer.go` — still used by Go wrapper
- `ats/storage/query_store.go` — still uses Go `*sql.DB`
- `ats/storage/query_builder.go` — still uses Go `*sql.DB`

## Dual-Connection Considerations

- SQLite WAL mode supports concurrent readers + one writer
- Both Rust and Go may write (Rust: attestation CRUD; Go: `storage_events`, embeddings, etc.)
- SQLite handles this with busy timeout — configure both sides with reasonable timeout (5s)
- Long-term: more operations move to Rust, narrowing Go's write surface

## Open Questions

1. **`signature`/`signer_did` in Rust**: These fields exist in the Go schema but not in Rust's `Attestation` type. Options: (a) add to `qntx-core::Attestation`, (b) handle as opaque attributes in the adapter layer, (c) store them in the Go adapter JSON but round-trip through Rust as pass-through fields.
2. **`BatchPersister`**: Currently uses `SQLStore` directly. Needs to either use `RustStore` or remain on Go `*sql.DB` as a special case.
