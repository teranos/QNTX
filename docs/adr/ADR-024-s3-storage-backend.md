# ADR-024: S3 Storage Backend (DuckDB + Parquet)

Date: 2026-07-13
Status: Proposed
Target: v0.29.0

## Context

ADR-023 introduces backend selection but leaves `sqlite` as the only choice. This ADR adds `s3` as the second value: state lives in S3, queried by an embedded DuckDB.

Choosing S3 is not about mirroring SQLite for durability — it's a distinct backend. When `backend = "s3"`, SQLite is not opened, and the box holds no persistent state.

## Decision

Add `s3` as a value for `[storage] backend`. When selected:

```toml
[storage]
backend = "s3"

[storage.s3]
bucket = "…"
prefix = "qntx"
region = "…"
credentials = "env"   # or "iam", "keys"
```

**Attestations** stream to S3 as Parquet files under `s3://<bucket>/<prefix>/attestations/year=YYYY/month=MM/day=DD/hour=HH/{uuid}.parquet`. Immutable, append-only. Multi-value fields (`subjects`, `predicates`, `contexts`, `actors`) store natively as Parquet `LIST<STRING>`. Reads run through DuckDB against `read_parquet('s3://<bucket>/<prefix>/attestations/**/*.parquet')` via the `httpfs` extension. Predicates push down through Parquet row-group statistics.

**All other state** (watchers, canvas, aliases, node identity, WebAuthn credentials, watcher execution queue, scheduled jobs, storage events, etc.) also lives in S3 under distinct prefixes. Shape per class:

- Small config (node identity, aliases, daemon config, WebAuthn creds, minimized windows) — one S3 object per record, rewritten on change.
- Append-only logs (storage_events, task_logs, pulse_executions, ai_model_usage) — Parquet, same pattern as attestations.
- Mutable config (watchers, canvas state, canvas glyphs, compositions, edges) — one S3 object per entity, rewritten on save.
- State machines (scheduled jobs, job checkpoints, watcher execution queue, async jobs) — one S3 object per record, rewritten on status transition. Multi-node coordination becomes easier because state is central.

**Signatures** are unchanged — signing is over canonical JSON (`ats/signing/signing.go:86`), format-independent.

**Fresh start.** No migration from an existing SQLite database.

**Vector data** (embeddings, cluster centroids, embedding projections, cluster tracking) is **out of scope** for this ADR. Its storage story is a follow-up decision.

**What SQLite-era mechanics do not run under `backend = "s3"`:**
- Bounded-storage enforcement (16/64/64) — S3 is unbounded, the pressure it existed to relieve is gone.
- Age-triggered distillation — same reason. Compaction of small Parquet files is a separate, mechanical operation.
- Local hot-backup ticker — nothing local to back up.
- Enforcement-triggered distillation.

**Multi-node writes become viable.** No per-node enforcement counters and no single-DB-file lock. Multiple QNTX nodes can write to the same S3 prefix; each writes uniquely named objects.

## Consequences

- New dependency: DuckDB (embedded) with `httpfs` and `parquet` extensions.
- **Point lookup by ID** (`storage_get(id)`) is no longer O(1). Every existence check today (e.g. the `exists()` call before every `put` at `store.rs:617`) becomes a Parquet scan unless a local ID index is maintained. Resolution deferred to implementation.
- **Write durability window.** State is lost if the process crashes between accept and S3 flush. Flush cadence is the RPO. Different from SQLite's per-write fsync guarantee.
- **Egress cost on reads.** DuckDB downloads Parquet chunks on query. Same-region S3 is free; cross-region incurs standard egress.
- **No `snapshot` / `restore` commands.** S3 is the store — there is nothing to snapshot to and nothing to restore from.
- Existing SQLite paths (`crates/qntx-sqlite`, `ats/storage/sqlitecgo`, `pulse/schedule/ticker.checkBackup`, `internal/config/SqliteConfig`) are untouched and continue to serve `backend = "sqlite"`.

## Minimum performance floor

The backend must sustain, at bare minimum:

- **30 attestations/s written**, sustained for at least 10 seconds
- **300 attestations/s read**, sustained for at least 10 seconds
- Attestation size: 1 KB

A benchmark test enforces this floor and fails the build if either rate is not held. This is a floor, not a target — real workloads may need much more. The floor exists to catch obviously-broken configurations (misconfigured region, unbatched writes, missing predicate pushdown) before they reach a running deployment.

## Open

Deliberately unresolved in this ADR — each is its own follow-up decision:

- **Compaction.** Small Parquet files accumulate. Who runs compaction and on what trigger.
- **ID index shape.** Local-only cache rebuilt from S3 on startup, side-index on S3, or accept scan cost.
- **Flush cadence.** Interval, buffer size, or both — determines the write durability window.
- **State-machine latency.** Watcher execution queue and job status transitions do many small S3 ops. Batching strategy needed if drain rates hit S3 latency limits.
- **Vector data storage.** Separate ADR — embeddings and cluster tables need their own home.
