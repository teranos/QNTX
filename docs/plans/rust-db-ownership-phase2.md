# Rust DB Ownership — Phase 2

Phase 1 (PR #691) moved enforcement from Go to Rust's single `rusqlite` connection, eliminating the dual-connection `SQLITE_CORRUPT` root cause. Phase 2 removes the remaining Go `*sql.DB` touchpoints on the attestations table and cleans up dead code. Phase 3 (PR #712, v0.25.0) made Rust the sole SQLite driver — all Go SQL routes through Rust via `database/sql/driver`.

## 2.1 Delete `as` command — DONE

Deleted prior to Phase 3. Parser (`ats/parser/as.go`) remains, still used by `ats/ix`.

## 2.2 Remove warning logic — DONE

Deleted prior to Phase 3. `bounded_store_warnings.go`, `StorageWarning` type, `CheckStorageStatus` calls all removed.

## 2.3 Remove Go-side telemetry dead code — DONE

`logStorageWarning` and `bounded_store_telemetry.go` deleted. `nullIfEmpty` remains in `bounded_store.go`, still used by `watcher_store.go`.

## 2.4 Route `GetStorageStats` through Rust FFI — DONE

Already routed through Rust: `RustBackedStore.GetStorageStats()` → `RustStore.GetStorageStats()` → Rust FFI. No Go `*sql.DB` queries remain against the attestations table.

## 2.5 Deprecate old db stats window, add eviction observability to database glyph

- Remove `database-stats-window.ts` (has a TODO saying exactly this)
- Ensure the database glyph (`default-glyphs.ts`) shows the same data
- Make evictions observable in the glyph — the `storage-eviction.ts` and `storage-warning.ts` handlers currently just `console.log`; surface eviction counts/details in the database glyph

## 2.6 Update docs — NOT NEEDED

`docs/architecture/bounded-storage.md` is accurate. Enforcement flow already shows Rust path. No references to deleted code.
