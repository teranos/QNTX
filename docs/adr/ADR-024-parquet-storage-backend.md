# ADR-024: Parquet Storage Backend (DuckDB)

Date: 2026-07-13
Status: Accepted
Target: v0.29.0

## Context

ADR-023 introduces backend selection but leaves `sqlite` as the only choice. This ADR adds `parquet` as the second value: state lives as Parquet files, queried by an embedded DuckDB.

Choosing Parquet is not about mirroring SQLite for durability — it's a distinct backend with a distinct storage model. When `backend = "parquet"`, SQLite is not opened.

The backend is named for the format, not the location. First target is **AWS Lightsail with S3**; local disk is supported for development. Other clouds (GCS, Azure Blob) are out of scope.

## Decision

Add `parquet` as a value for `[storage] backend`.

**Implementation** (per ADR-023's pattern): a new Rust crate `crates/qntx-duckdb` embeds DuckDB and implements the same storage traits as `qntx-sqlite`. Go accesses it through CGO at `ats/storage/duckdbcgo`. No Go-side DuckDB binding — Rust owns the DuckDB C library, one process, one lifecycle.

The crate is named `qntx-duckdb` (not `qntx-parquet`) because it wraps DuckDB. Parquet is the on-disk format the backend writes; DuckDB is the runtime dependency the crate embeds. If DuckDB is later used for another purpose, the same crate is reusable.

**Configuration:**

```toml
[storage]
backend = "parquet"

[storage.parquet]
location = "s3://bucket/prefix"
# or: "file:///var/lib/qntx/parquet"
```

`location` is a URL. Supported schemes: `s3://` (production, AWS Lightsail with S3), `file://` (development). No credentials field: the AWS SDK's default credential chain resolves them (IAM role on Lightsail, env vars, `~/.aws/credentials`, etc.). QNTX does not read secrets from `am.toml`.

**Attestations** stream as Parquet files under `<location>/attestations/year=YYYY/month=MM/day=DD/hour=HH/{uuid}.parquet`. Immutable, append-only. **Hourly partition granularity is a chosen default**, not a config knob — it balances predicate pushdown (fewer partitions to scan) against small-file count (more partitions = more small files). Revisit only if a real workload forces the question.

Multi-value fields (`subjects`, `predicates`, `contexts`, `actors`) store as Parquet `LIST<VARCHAR>` — a native DuckDB type that round-trips through Parquet's `LIST` logical type. Reads run through DuckDB's `read_parquet(...)`; predicates push down through Parquet row-group statistics.

**All other state** (watchers, canvas, aliases, node identity, WebAuthn credentials, watcher execution queue, scheduled jobs, storage events, etc.) also lives at `<location>` under distinct prefixes. Shape per class:

- Small config (node identity, aliases, daemon config, WebAuthn creds, minimized windows) — one object per record, rewritten on change.
- Append-only logs (storage_events, task_logs, pulse_executions, ai_model_usage) — Parquet, same pattern as attestations.
- Mutable config (watchers, canvas state, canvas glyphs, compositions, edges) — one object per entity, rewritten on save.
- State machines (scheduled jobs, job checkpoints, watcher execution queue, async jobs) — one object per record, rewritten on status transition.

**Signatures** are unchanged — signing is over canonical JSON (`ats/signing/signing.go:86`), format-independent.

**Fresh start.** No migration from an existing SQLite database.

**No Parquet format knobs exposed.** Compression, row-group size, page size, column encodings are hardcoded to DuckDB's defaults inside the backend. Add knobs only when a real workload forces the question.

**No distillation, no bounded-storage enforcement, no compaction.** Parquet storage is unbounded; the SQLite-era pressure that made these necessary is gone. Small-file accumulation is accepted; if it ever becomes a real cost, compaction is a separate future ADR.

**Vector data** (embeddings, cluster centroids, embedding projections, cluster tracking) is out of scope for this ADR.

**Multi-node writes.** No per-node enforcement counters, no single-DB-file lock. Multiple QNTX nodes write to the same location; each writes uniquely named objects. No coordination needed because no compaction runs.

## Dependencies

Pinned versions (verified 2026-07-13):

- **DuckDB C library**: provided by `pkgs.duckdb` in `flake.nix` (currently `v1.5.4`). Not source-compiled by the crate — trusted from nixpkgs' reproducible build. This is a deliberate DX choice: bundling from source added 15+ minute compile times on M1 8GB machines, which is unacceptable for every dev, every CI run, every cache invalidation.
- **duckdb-rs**: `v1.10504.0` — official `duckdb/duckdb-rs` Rust bindings, actively maintained (last commit 2026-07-10). Cargo.toml uses `features = ["parquet"]` (no `bundled`), so the crate dynamically links to the Nix-provided libduckdb.
- DuckDB `httpfs` extension: loaded at runtime via `INSTALL httpfs; LOAD httpfs;` (DuckDB autoinstalls from its extension repository on first use, then caches locally). For strict-offline deployments a pre-fetched binary is required; not in scope for the AWS Lightsail first target.
- `parquet`: built into the linked DuckDB library and enabled via the `parquet` Cargo feature.

No Go DuckDB binding. All DuckDB access is through the Rust crate.

## Consequences

- **Point lookup by ID** (`storage_get(id)`) is no longer O(1) via a primary-key index. Every existence check today (e.g. the `exists()` call before every `put` at `store.rs:617`) becomes a Parquet scan unless a local ID index is maintained. Whether this is a real problem is answered by the performance floor — if the floor holds without an index, we ship without one.
- **Write durability window.** State is lost if the process crashes between accept and Parquet flush. Flush cadence is the RPO. Different from SQLite's per-write fsync guarantee.
- **Egress cost on remote reads.** DuckDB downloads Parquet chunks on query. Same-region S3 is free; cross-region incurs standard egress. `file://` locations have none.
- **No `snapshot` / `restore` commands.** Parquet files at the location are the store — there is nothing to snapshot to and nothing to restore from.
- **No secrets in config.** Credentials come from the SDK's default chain. Deployments on AWS use IAM roles; local dev uses `~/.aws/credentials` or env vars.
- Existing SQLite paths (`crates/qntx-sqlite`, `ats/storage/sqlitecgo`, `pulse/schedule/ticker.checkBackup`, `internal/config/SqliteConfig`) are untouched and continue to serve `backend = "sqlite"`.

## Minimum performance floor

The backend must sustain, at bare minimum:

- **30 attestations/s written** (accepted at the API), sustained for at least 10 seconds
- **300 attestations/s read** (returned by query), sustained for at least 10 seconds
- Attestation size: 1 KB

"Written" here means accepted by the API — attestations may still be in-memory buffer waiting for flush. Durability latency (time from accept to Parquet file landing) is a separate measurement, bounded by the flush interval.

**The floor is validated on AWS Lightsail against a real S3 bucket**, not in CI. Per-commit CI runs a functional smoke test against `file://` locations that proves the stack works end-to-end but does not attempt the throughput number. The Lightsail benchmark is a release-gate step — run manually or via a make target, prints numbers, blocks tagging v0.29.0 if either rate is not held.

This is a floor, not a target — real workloads may need much more. The floor exists to catch obviously-broken configurations (misconfigured region, unbatched writes, missing predicate pushdown) before they reach a running deployment.

## Open

Deliberately unresolved in this ADR — each is its own follow-up decision:

- **ID index shape** (if the performance floor fails without one). Local-only cache rebuilt from location on startup, side-index at the location, or accept scan cost.
- **Flush cadence.** Interval, buffer size, or both — determines the write durability window.
- **State-machine latency.** Watcher execution queue and job status transitions do many small object ops. Batching strategy needed if drain rates hit location latency limits.
- **Vector data storage.** Separate ADR — embeddings and cluster tables need their own home.
