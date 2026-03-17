# Rust DB Ownership — Phase 2

Phase 1 (PR #691) moved enforcement from Go to Rust's single `rusqlite` connection, eliminating the dual-connection `SQLITE_CORRUPT` root cause. Phase 2 removes the remaining Go `*sql.DB` touchpoints on the attestations table and cleans up dead code.

## 2.1 Delete `as` command

Remove `cmd/qntx/commands/as.go` and its registration in `cmd/qntx/main.go`. The CLI attestation creation path is deprecated.

## 2.2 Remove warning logic

Delete `bounded_store_warnings.go`, `bounded_store_warnings_test.go`, `StorageWarning` type, and all `CheckStorageStatus` calls (`batch.go`). Warnings predicted when evictions *would* happen — we now accept evictions and observe them after the fact.

## 2.3 Remove Go-side telemetry dead code

Delete `logStorageWarning` and `nullIfEmpty` from `bounded_store_telemetry.go`. Rust writes `storage_events` directly since Phase 1. If the file becomes empty, delete it.

## 2.4 Route `GetStorageStats` through Rust FFI

`BoundedStore.GetStorageStats()` still queries via Go's `*sql.DB`. Replace with the existing `RustStore.GetStorageStats()` FFI call (`storage_get_stats`). This removes the last Go SQL query against the attestations table.

## 2.5 Deprecate old db stats window, add eviction observability to database glyph

- Remove `database-stats-window.ts` (has a TODO saying exactly this)
- Ensure the database glyph (`default-glyphs.ts`) shows the same data
- Make evictions observable in the glyph — the `storage-eviction.ts` and `storage-warning.ts` handlers currently just `console.log`; surface eviction counts/details in the database glyph

## 2.6 Update docs

Update `docs/architecture/bounded-storage.md`:
- Code organization section references `bounded_store_enforcement.go` (deleted in Phase 1)
- Enforcement flow diagram shows Go path (now Rust)
- Testing section uses `qntx as` commands (deleted in 2.1)
- Remove warning-related guidance
