# ADR-023: Storage Backend Selection

Date: 2026-07-13
Status: Proposed
Target: v0.29.0

## Context

Storage today is hard-wired to SQLite via Rust (per ADR-013). Every code path that touches attestation storage assumes the `SqliteStore` concrete type. There is no notion of "backend" — there is only "the store."

The existing `[database]` config block conflates two things: which backend is used (implicit: SQLite) and how that backend is configured (`path`, `backup_interval_seconds`, `bounded_storage`). Introducing a separate `[storage]` block for backend selection alongside `[database]` for SQLite settings is confusing — two blocks doing storage-adjacent things.

## Decision

Backend becomes a chosen thing. `[storage] backend = "sqlite"` selects the concrete store at startup. `sqlite` is the only value today; new values are added by subsequent ADRs.

Backend-specific configuration lives under `[storage.<backend>]`. SQLite's settings — `path`, `backup_interval_seconds`, `bounded_storage` — move from `[database]` to `[storage.sqlite]`. `[database]` is removed.

```toml
[storage]
backend = "sqlite"

[storage.sqlite]
path = "qntx.db"
backup_interval_seconds = 3600
[storage.sqlite.bounded_storage]
actor_context_limit = 32
actor_contexts_limit = 64
entity_actors_limit = 64
```

Backend implementations are Rust crates at `crates/qntx-<name>` exposed to Go via CGO/FFI, following the ADR-013 ownership pattern. The Go side lives at `ats/storage/<name>cgo`. Adding a new backend means adding a new crate and its FFI surface, not modifying an existing one.

A running QNTX has exactly one backend. No dual-backend operation, no runtime swap.

## Consequences

- `DatabaseConfig` in `internal/config/am.go` is renamed and moved under `StorageConfig`. Every `cfg.Database.*` reader becomes `cfg.Storage.Sqlite.*` (~15 call sites).
- Existing `am.toml` files with `[database]` no longer parse. Pre-release, no migration required.
- The `QNTX_DATABASE_PATH` environment variable is renamed to `QNTX_STORAGE_SQLITE_PATH` to match the new config key.
- New backends are added by subsequent ADRs — each names its own value and its own `[storage.<name>]` block.
- Behaviors that only make sense on one backend (distillation, bounded-storage enforcement, local hot backup) gate on the selected backend.
- No abstraction beyond what selection requires: no plugin service, no cross-backend replication, no live migration.
