# ix-json Glyph

JSON API ingestion glyph. Points at an HTTP endpoint, fetches JSON, maps fields to attestation SPC (Subject, Predicate, Context), and polls on a schedule via Pulse.

## Current State (v0.3.4)

- Per-glyph config stored as attestations (subject: `ix-json-glyph-{glyphID}`, predicate: `configured`)
- Mapping inferred via heuristics engine (`inferMapping` in `ingest.go`)
- Schedules managed via ScheduleService gRPC API, schedule IDs persisted in attestations
- Plugin uses `plugin.Base` and `plugingrpc.Run` shared infrastructure

## Three-Tier Mapping Resolution

JSON key to attestation attribute/SPC mapping resolves in priority order:

1. **Attested mapping** (runtime, per-glyph) — user configures via glyph UI, persisted as attestation
2. **Plugin config mapping** (deploy-time, all glyphs) — `ConfigSchema()` defaults in am.toml
3. **Heuristics engine** (built-in) — `inferMapping()` in `ingest.go`, fallback when nothing configured

Currently only tier 3 is implemented. Tier 1 and 2 are tracked in #626.

## Known Limitations

- #626 — Glyph UI redesign (inline Go HTML, no editable mapping, no live feedback)
- #627 — HTTP client capabilities (GET-only, no pagination, no rate-limiting)
- #628 — Data pipeline and meld integration (not meldable, no watcher integration)
- #629 — Type attestation at ingestion time (rich_string, unique, secret, array)
- #630 — Secrets and variables system (auth tokens in plaintext attestations)

## Vision

ix-json is one specialization of the broader ix universal data ingestor. The end state: point any URL at an ix glyph and it ingests — JSON, HTML, CSV, XML, RSS, binary. Compose with py/se/ax glyphs via meld for filtering and transformation pipelines.
