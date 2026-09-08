# ADR-024: Parquet Storage Backend (DuckDB)

Date: 2026-07-13
Revised: 2026-09-08 — the store as it runs, measured; compaction; one writer per namespace
Status: Accepted
Target: v0.29.0

## Context

ADR-023 introduces backend selection but leaves `sqlite` as the only choice. This ADR adds `parquet` as the second value: state lives as Parquet files, queried by an embedded DuckDB.

Choosing Parquet is not about mirroring SQLite for durability — it's a distinct backend with a distinct storage model. When `backend = "parquet"`, SQLite is not opened.

**Not true yet.** A `backend = "parquet"` deployment opens both: startup runs `ats-sqlite` migrations *and* `ats-duckdb` migrations in one process, because only attestations and access tokens have a parquet implementation. Everything else — watchers, canvas, aliases, embeddings, schedules, WebAuthn credentials — is still served by SQLite, so the process holds two stores at once. `make parity` prints which things are where. This paragraph describes the intended end state; the split closes as each thing moves.

The backend is named for the format, not the location. First target is **AWS Lightsail with S3**; local disk is supported for development. Other clouds (GCS, Azure Blob) are out of scope.

## Decision

Add `parquet` as a value for `[storage] backend`.

**Implementation** (per ADR-023's pattern): a new Rust crate `crates/ats-duckdb` embeds DuckDB and implements the same storage traits as `ats-sqlite`. Go accesses it through CGO at `ats/storage/duckdbcgo`. No Go-side DuckDB binding — Rust owns the DuckDB C library, one process, one lifecycle.

The crate is named `ats-duckdb` (not `qntx-parquet`) because it wraps DuckDB. Parquet is the on-disk format the backend writes; DuckDB is the runtime dependency the crate embeds. If DuckDB is later used for another purpose, the same crate is reusable.

**Configuration:**

```toml
[storage]
backend = "parquet"

[storage.parquet]
location = "s3://bucket/prefix"
# or: "file:///var/lib/qntx/parquet"
```

`location` is a URL. Supported schemes: `s3://` (production, AWS Lightsail with S3), `file://` (development). No credentials field: the AWS SDK's default credential chain resolves them (IAM role on Lightsail, env vars, `~/.aws/credentials`, etc.). QNTX does not read secrets from `am.toml`.

**Namespace is the top-level prefix.** Every path below is `<location>/<namespace>/<kind>/…` — "everything is part of a namespace", "nothing falls outside of it", "namespace isnt, pick and choose". A deployment always has two: `system` and `default`. That makes isolation structural rather than remembered — a watcher in namespace B does not fire on an attestation in A because it has no path that reaches A, and a schedule created in A stays in A for the same reason.

**Attestations** are Parquet files under `<location>/<namespace>/attestations/`, flat. Accepted writes sit in an in-memory buffer; `flush` copies the buffer to a new file named `{millis}-{uuid}.parquet` and empties it. Files are immutable once written. Reads union the buffer with every file under the prefix.

**No partition paths.** An hourly `year=/month=/day=/hour=` layout was specified here once and never built. It is dropped rather than kept as intent: the statement a store runs most is the lookup by id before every write, which carries no time predicate, so partitions prune nothing for it and multiply the files it has to open. Time pruning comes from the row-group statistics inside the compacted file.

**Compaction.** Files accumulate at the flush cadence, one per interval with a non-empty buffer, and the cost of a read is the number of files (see Consequences). When the count under a prefix passes a threshold, the store merges the files under it into one and deletes the sources. One run takes a bounded number of them, because every other read and write waits while it reads what it merges; a prefix holding more is worked off over several runs, each leaving it smaller. The threshold and the bound are constants in the crate.

A record of what is being merged is written first and removed last. The merged file then decides what an interrupted run left behind: absent, the sources are still the whole store; whole, its rows are held twice and the sources go; partial, it goes. The next open reads the record and finishes accordingly, holding every row throughout.

One file per namespace, rewritten whole on each compaction, until that file reaches 1 GB. Apache Parquet's own recommendation is a 1 GB row group and one row group per file; DuckDB's is 100 MB to 10 GB per file. At 1 GB the whole-rewrite stops being the obvious answer, and a second ADR answers it then.

Multi-value fields (`subjects`, `predicates`, `contexts`, `actors`) store as Parquet `LIST<VARCHAR>` — a native DuckDB type that round-trips through Parquet's `LIST` logical type. Reads run through DuckDB's `read_parquet(...)`; predicates push down through Parquet row-group statistics.

**All other state** (watchers, canvas, aliases, node identity, WebAuthn credentials, watcher execution queue, scheduled jobs, storage events, etc.) lives under its namespace at `<location>/<namespace>/`, in a prefix named for the SQLite table it stands in for — `make parity` pairs them by name, and a second name would read as a second thing. No store opens without being told which namespace it is. Shape per class:

- Small config (aliases, daemon config, WebAuthn creds, minimized windows) — one object per record, rewritten on change.
- Append-only logs (storage_events, task_logs, pulse_executions, ai_model_usage) — Parquet, same pattern as attestations.
- Mutable config (watchers, canvas state, canvas glyphs, compositions, edges) — one object per entity, rewritten on save.
- State machines (scheduled jobs, job checkpoints, watcher execution queue, async jobs) — one object per record, rewritten on status transition.

**Node identity is none of these.** One object at `<location>/system/node_identity/self.json`, written once at first boot, holding an ed25519 private key. A rewrite is not an update — it mints a new DID and orphans every signature made under the old one.

It shares `system/` with `access_tokens/` (ADR-025), and those two are the store's secrets — a bucket policy written for attestation data covers neither. Tokens sit there rather than under the namespace they authorize because a bearer names no namespace until it has been resolved.

The system namespace is a literal rather than a DID because this object names the identity every other namespace is keyed by: you would have to read it to know where to read it.

**Signatures** are unchanged — signing is over canonical JSON (`CanonicalJSON` in `ats/signing/signing.go`), format-independent.

**Fresh start.** No migration from an existing SQLite database.

**No Parquet format knobs exposed.** Compression, row-group size, page size, column encodings are hardcoded to DuckDB's defaults inside the backend. Add knobs only when a real workload forces the question.

**No distillation, no bounded-storage enforcement.** Parquet storage is unbounded; the SQLite-era pressure that made these necessary is gone. Compaction is not a bound: it changes how many files hold the rows, never which rows are held.

**Vector data** (embeddings, cluster centroids, embedding projections, cluster tracking) is out of scope for this ADR.

**One node writes a namespace prefix.** That node owns the buffer, the flushes and the compaction under `<location>/<namespace>/`. Compaction deletes files, so a second writer to the same prefix would need to agree on who merges what, and this ADR specifies no such agreement. A second node writing the same location is a later ADR with its own coordination; until then it is not a supported deployment.

## Dependencies

- **DuckDB C library**: provided by `pkgs.duckdb` in `flake.nix`. Not source-compiled by the crate — trusted from nixpkgs' reproducible build.
- **duckdb-rs**: `duckdb/duckdb-rs` Rust bindings. Cargo.toml uses **no cargo features** on the `duckdb` crate. The `parquet` feature transitively enables `bundled` (`parquet = ["libduckdb-sys/parquet", "bundled"]`) and only adds Rust-side Parquet APIs on top of what SQL already exposes; we use Parquet exclusively through SQL.
- **DuckDB Parquet support**: built into `pkgs.duckdb` as a first-party DuckDB extension. Accessed through SQL only: `COPY ... TO '<prefix>/{millis}-{uuid}.parquet' (FORMAT PARQUET)` for writes, `read_parquet('<prefix>/*.parquet')` for reads, and the same `COPY` from a `read_parquet` over the glob for compaction.
- **DuckDB `httpfs` extension**: loaded at runtime via `INSTALL httpfs; LOAD httpfs;`. DuckDB autoinstalls from its extension repository on first use, then caches locally. A glob against S3 is one ListObjectsV2. httpfs reads and writes objects and does not delete them; deletion for compaction goes through the AWS SDK client the crate already holds for single-object records.
- **Runtime linking**: the qntx binary dynamically links Nix's `libduckdb`. Deploying to a non-Nix host requires either shipping `libduckdb.so` alongside the binary or building `libats_duckdb.a` with libduckdb statically embedded.

- **The host's glibc sets the ceiling on the DuckDB version.** Shipping `libduckdb.so` to a non-Nix host means it links that host's libc. glibc is backward compatible and not forward compatible, so a libduckdb built against a newer glibc than the host has will not load — the process dies at startup with `version 'GLIBC_ABI_...' not found`, before any QNTX code runs. Because a nixpkgs revision fixes glibc and DuckDB together, choosing a DuckDB version silently chooses a glibc. **Check the deployment's `ldd --version` before moving the nixpkgs pin.** This is not hypothetical: pinning to a revision with DuckDB 1.5.4 also took glibc 2.42, a Debian 13 host has 2.41, and the API was down until the pin moved back.

Exact version pins live in `flake.nix` (for the C library and toolchain) and `crates/ats-duckdb/Cargo.toml` (for the Rust binding). This ADR does not restate them.

No Go DuckDB binding. All DuckDB access is through the Rust crate.

## Consequences

- **The cost of a read is the number of files.** A Parquet reader opens each file's footer before it reads a row, and against S3 every statement is one ListObjectsV2 plus one round trip per file under the glob. Measured on the production node on 2026-09-08: 4770 files holding 20 MB, 0.05 s per file on every statement, start-to-ready grown from 11 s to 250 s median in five weeks, a one-row query answered in 3 to 4 s. Compaction exists to hold the file count at a constant.
- **Point lookup by ID** (`storage_get(id)`) is a scan of the buffer and the files, and the existence check before every `put` is that lookup. After compaction it is a scan of one file, which is what makes a write cheap enough to pay at boot.
- **A boot pays one write.** The store proof subsystem writes the `node:started` attestation before the door opens, because a node whose store will not take a write has nothing to admit anyone into. Startup therefore costs whatever a write costs, and the previous bullet is what keeps that small.
- **Write durability window.** State is lost if the process crashes between accept and Parquet flush. Flush cadence is the RPO. Different from SQLite's per-write fsync guarantee.
- **Egress** on remote reads is standard S3 pricing, and the file count above is what dominates at these sizes. `file://` locations have none.
- **No `snapshot` / `restore` commands.** Parquet files at the location are the store — there is nothing to snapshot to and nothing to restore from.
- **No secrets in config.** Credentials come from the SDK's default chain. Deployments on AWS use IAM roles; local dev uses `~/.aws/credentials` or env vars.

## The floor

The floor is measured where the cost is paid: on every start of a production node, against its real location.

Each subsystem in `server/subsystem.go` logs its own duration when it finishes, at the level `openDatabase complete` already uses, and the same number goes out as a metric keyed by subsystem name. The store proof's duration is a write against the real bucket with the real file count, so it is the store's floor, taken on every boot without anyone arranging it.

A start whose subsystems together exceed 60 seconds is a Sentry event. Sixty seconds is the point at which the operator called the wait long, and it is the number a deploy is judged against.

This replaces two earlier floors. `TestPerformanceFloor` in `ats/storage/duckdbcgo/benchmark_test.go` drives 30 writes/s and 300 reads/s for ten seconds against `file://`, where opening a file costs microseconds; the growth described under Consequences ran for five weeks without moving it. It stays as a test of the code path and is not the floor. A one-time gate against S3 before a release tag was run once and never again. The one above is taken every time the node comes up.
