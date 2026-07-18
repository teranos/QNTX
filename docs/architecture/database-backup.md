# Database Hot Backup

Periodic hot backup of the ATS SQLite database while the system is running.

## Architecture

Backup runs on the Pulse ticker (every `backup_interval_seconds`, default 3600). It rotates two files: `.bak1` (newest) and `.bak2` (previous).

The backup opens its own **read-only SQLite connection** to the source database. It does not use the shared `RustStore` connection and does not hold the Go mutex. This means ATS operations (reads, writes, enforce_limits) continue uninterrupted during backup.

The backup runs in a **goroutine** so the Pulse ticker loop is never blocked. An `atomic.Bool` guard prevents overlapping backups.

## SQLite Backup API Under Write Load

SQLite's `sqlite3_backup_step()` copies pages from source to destination. Under concurrent writes:

- **`StepResult::More`** — pages copied successfully. The backup yields briefly (`thread::yield_now()`) to let writers proceed.
- **`StepResult::Busy` / `Locked`** — the source is being written to. The backup backs off 5ms and retries.

The backup uses a manual `step()` loop instead of rusqlite's `run_to_completion()`. Under sustained write load, `run_to_completion` stalls because its fixed sleep between steps (originally 250ms) gives writers enough time to re-acquire the lock before the next step can read. The manual loop uses minimal delays: `yield_now()` on success, 5ms on Busy.

### Performance (measured under rw_flood at 100ms write interval, 273MB database)

| Approach | Duration | Notes |
|----------|----------|-------|
| Old: mutex held during `C.storage_backup()` | 4-12s | **All ATS blocked for the entire duration** |
| `run_to_completion(10k, 250ms)` | never finishes | 250ms sleep lets writers starve the backup |
| `run_to_completion(1k, 10ms)` | 42s | Better but still too much yielding |
| Manual `step(5k)`, yield/5ms backoff | 4-10s | No ATS blocking, completes reliably |

## Configuration

In `am.toml`:

```toml
[storage.sqlite]
backup_interval_seconds = 3600  # 0 disables backup
```

## Key Files

- `crates/qntx-sqlite/src/store.rs` — `SqliteStore::backup()`: opens read-only source, manual step loop
- `crates/qntx-sqlite/src/ffi.rs` — `storage_backup()`: FFI entry point
- `ats/storage/sqlitecgo/storage_cgo.go` — `RustStore.Backup()`: Go wrapper, no mutex
- `pulse/schedule/ticker.go` — `checkBackup()`: scheduling, goroutine, rotation
