# ADR-037: The operational db

Date: 2026-09-12
Status: Accepted

## Context

A parquet deployment opens two stores, not one. Attestations go to S3; watchers, canvas, aliases, schedules, jobs and WebAuthn credentials go to a local SQLite db that `sqlitecgo.NewFileStore` migrates at boot. That db is already local and already in the process, and its migrations already build the index the read path wants: `attestation_subjects`, `attestation_actors`, `attestation_contexts` and `attestation_predicates`, with an `idx_junc_*` on each.

Reads of attestations do not use it. Every query calls `parquet_file_count()`, which is a live ListObjectsV2, then `read_parquet` over the glob, which opens every file under the prefix. There is no cache and no index on the four LIST columns a filter names.

The target is 2 attestations written per second and 1200 read per second, so 600 reads for every write. [ADR-024](ADR-024-parquet-storage-backend.md) records what the read path measures against that on the production node: a one-row query answered in 3 to 4 seconds.

## Decision

"operational db authoritative while running writes land there first"

"S3 as the record it's the rebuild source and protects against host loss, not process crash"

"reconcile-on-open by watermark is approved"

Nothing here is built yet, and the index cannot be chosen until the read mix is known. `QueryFilter` admits three shapes — point lookups by id through `get_many`, filters on subjects, predicates, contexts or actors, and time ranges — and 1200 reads per second of each is a different index.

## Consequences

[ADR-023](ADR-023-storage-backend-selection.md) used to say a running QNTX has exactly one backend and forbid dual-backend operation. It now says parquet is optional persistence and the operational db keeps running either way, which is what made this ADR possible.

The db glyph gets its numbers back. `dimensionsDescribeTheCount` in `server/db_stats_cache.go` is set only when the count falls through to the operational tables, which on parquet it does not, so `unique_actors`, `unique_subjects`, `unique_contexts`, `distillation` and `predicate_histograms` are left out of the response rather than sent as zero. Attestations in the operational db set that flag, and the five return with no frontend change: `web/ts/db-glyph.ts` already reads an absent key as a backend that does not answer, and a zero as an answer of none.
