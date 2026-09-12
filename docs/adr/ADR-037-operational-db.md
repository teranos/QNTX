# ADR-037: The operational db

Date: 2026-09-12
Status: Accepted

"operational db authoritative while running, S3 as the record, reconcile-on-open by watermark, GRACE's ✿/❀ as the lifecycle."

## Context

A parquet deployment opens two stores, not one. Attestations go to S3; watchers, canvas, aliases, schedules, jobs and WebAuthn credentials go to a local SQLite db that `sqlitecgo.NewFileStore` migrates at boot. That db is already local, already indexed, and already in the process.

Reads of attestations do not use it. Every query calls `parquet_file_count()`, which is a live ListObjectsV2, then `read_parquet` over the glob, which opens every file under the prefix. There is no cache and no index on the four LIST columns a filter names.

## Decision

Nothing here is built yet. This ADR is where it gets decided.

## Consequences

[ADR-023](ADR-023-storage-backend-selection.md) used to say a running QNTX has exactly one backend and forbid dual-backend operation. It now says parquet is optional persistence and the operational db keeps running either way, which is what made this ADR possible.
