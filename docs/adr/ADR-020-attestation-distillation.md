# ADR-020: Attestation Distillation (Sigma)

Date: 2026-05-12
Updated: 2026-05-15
Status: Accepted

## Context

Bounded storage enforcement deletes attestations when limits are exceeded (16 per actor/context, 64 contexts per actor, 64 actors per subject). At steady state with high-volume predicates, enforcement fires on every `put()`, evicting one attestation per write. This is pure data loss — evicted attestations are destroyed with no trace beyond a `storage_events` log entry.

## Decision

Distillation preserves aggregate data from evicted attestations by folding them into a **sigma** (Σ) — a single compressed attestation — before deletion. Sigmas are normal attestations — they participate in enforcement like any other, enabling recursive meta-distillation where sigmas themselves get folded into coarser summaries over time.

**Why built-in, not a plugin:** Distillation requires atomic read-merge-delete. The gRPC plugin interface does not expose deletion.

## Two Distillation Paths

### 1. Enforcement-triggered (Rust, `crates/qntx-sqlite/src/enforcement.rs`)

Fires when bounded limits are exceeded. The eviction batch is loaded, distilled into a sigma, originals deleted, sigma inserted. Operates within a single SQLite transaction.

### 2. Age-triggered (Go, `server/distill_pulse.go`)

A Pulse handler that runs on a configurable interval. Queries attestations older than `max_age_hours` **plus all existing sigmas regardless of age**. Sigmas are exempt from the age rule — they are always eligible for re-distillation. This means every cycle absorbs the existing sigma for a predicate alongside any newly-aged raw attestations, producing a single growing sigma per predicate.

Groups by predicate, creates one sigma per predicate group, deletes originals.

**Minimum group size:** Groups with fewer than 2 attestations are skipped. A sigma that absorbs a single attestation is a 1:1 copy — it adds no compression, wastes a write, and deletes the original (net negative). Singles remain until more attestations with the same predicate age in.

Also performs ghost row cleanup (NULL/empty IDs, zero timestamps) at the start of each cycle.

## Half-Bound Threshold

Instead of evicting at `limit`, enforcement triggers at `limit + limit/2`. For actor_context_limit=16, enforcement triggers at 24 and evicts down to 16. This amortizes enforcement cost (1 cycle per 8 writes instead of per write) and produces meaningful batches for distillation.

## Insert Ordering

In the Rust enforcement path, the sigma is inserted AFTER deleting originals. This prevents the sigma insert from inflating the enforcement counter before deletion. The delete count is `count - limit + 1` — the `+1` compensates for the sigma that will be inserted after deletion, which increments the counter back up by 1. Without this, enforcement leaves `limit + 1` attestations instead of `limit`.

## Sigma Shape

```
subjects:   ["distill:<predicate>"]
predicates: ["distill:<predicate>"]
actors:     ["system:distill", ...inherited actors]   // all unique actors from evicted attestations (capped at 10K)
contexts:   [union of evicted contexts, capped at 50]
source:     "distill"
attributes: {
  _distill: true,
  _count: 8,
  _total: 342,
  _subjects_count: 45,
  _subjects_sample: ["sub_1", "sub_2", ...],         // up to 10
  _first_seen: "2026-05-04T...",
  _last_seen:  "2026-05-12T...",
  _histogram: {"2026-05-04T12:10": 3, ...},          // temporal distribution
  _version: "v0.8.0 (fd7326d)",                   // Go binary version
  _rust_version: "0.2.3",                          // Cargo workspace version
  some_number: {min: 0, max: 4500, sum: 14640, count: 8},
  some_string: {frequencies: {"a": 5, "b": 3}, count: 8},
  some_constant: "abc123"                          // constant across all -- kept as scalar
}
```

## Attribute Merging

- **Number** -> `{min, max, sum, count}` — avg derived as sum/count
- **String** -> `{frequencies: {val: count, ...}, count}` — frequency-tracked distribution, capped at 50 unique values. Legacy `{values: [...], count}` format (pre-frequency sigmas) passes values through as `unplaced` without fabricating counts. If constant across all attestations, kept as scalar.
- **Sigma metadata keys** (`_distill`, `_count`, `_total`, `_first_seen`, `_last_seen`, `_version`, `_rust_version`, `_subjects_count`, `_subjects_sample`, `_actors_count`, `_actors_sample`, `_histogram`) are skipped during merging and rebuilt from the batch. Note: `_actors_count` and `_actors_sample` are legacy — actors are now inherited directly into the sigma's `actors[]` field.

### Re-distillation attribute stripping

When re-distilling sigmas, nested aggregate maps (numeric `{min,max,sum,count}` and string `{frequencies,count}`) from prior sigmas are **stripped before merging**. Without this, frequency maps and numeric aggregates compound across re-distillation cycles, growing unboundedly until they exceed the 1MB attestation JSON limit.

The first distill (raw -> sigma) preserves full attribute aggregates. Subsequent re-distillation preserves only scalar attributes and distill metadata. The temporal story (`_total`, `_histogram`, `_first_seen`, `_last_seen`) is the primary value of a sigma — attribute aggregates are secondary.

## Observation Counting: `_count` vs `_total`

`_count` is the batch size — how many attestations were folded in this distill cycle. `_total` is the transitive count of original observations across all sigma generations. For a first-generation sigma of 8 raw attestations: `_count = 8`, `_total = 8`. When two such sigmas are meta-distilled: `_count = 2` (two sigmas in this batch), `_total = 16` (8 + 8 original observations).

`_total` is computed by summing each input's `_total` (falling back to `_count`, then to 1 for raw attestations). This preserves the full observation lineage regardless of how many meta-distillation cycles have occurred.

## Meta-Distillation

Sigmas are always eligible for re-distillation — they are not gated by `max_age_hours`. Every distill cycle picks up all existing sigmas and merges them with any raw attestations that have aged past the threshold. This produces a single growing sigma per predicate that absorbs more observations each cycle.

The number of sigmas is bounded by the number of unique predicates — a small fixed set. Rewriting ~20-30 sigmas every cycle is negligible cost.

### Float64 Interop

Go stores JSON numbers as `float64`. Rust's `serde_json::Value::as_u64()` returns `None` for float-typed numbers like `342.0`. Both `_total` and `_count` parsing use `as_u64().or_else(|| as_f64().map(|f| f as u64))` to handle values that round-tripped through Go.

### Prefix Stacking Prevention

Each distillation cycle prefixes predicates with `distill:`. Without prevention, meta-distillation produces `distill:distill:distill:...`. The builder strips all existing `distill:` prefixes before adding one.

## Version Tracking

Each sigma records `_version` (Go binary version + commit hash) and `_rust_version` (Cargo workspace version from `crates/qntx-sqlite`). This creates traceable lineage — when `max_age_hours` is lowered incrementally, the Rust patch version is bumped each time, making it possible to identify which distill cycle produced a given sigma.

Go and Rust versions are independent. Rust version lives in `Cargo.toml` workspace version.

## Configuration

```toml
[distill]
interval_seconds = 360    # omit to disable
max_age_hours = 54
batch_size = 5000          # default 500
dry_run = false            # default
```

Enforcement-path distillation has no configuration — it is always on when enforcement runs.

## Temporal Histogram

Without a histogram, a sigma collapses temporal distribution to two points (`_first_seen`, `_last_seen`). A sigma carrying 13,803 observations over 3 days has no internal time structure — bursts and lulls are indistinguishable.

The `_histogram` attribute preserves temporal distribution as bucketed observation counts. Key format encodes resolution tier:

| Tier | Key format | Budget span | Use |
|------|-----------|-------------|-----|
| 10min | `2026-05-13T14:10` | 33h | fresh sigmas (hours) |
| Hourly | `2026-05-13T14` | 8.3 days | day-scale |
| Daily | `2026-05-13` | 200 days | week/month-scale |
| Weekly | `2026-W20` | 3.8 years | deep history (terminal tier) |

`sum(_histogram.values()) <= _total`. The gap is unplaced observations from pre-histogram sigmas — no backfilling, no fabrication. New observations accumulate with histogram placement; legacy unplaced counts become a shrinking fraction over time.

**Coarsening:** Key count budget is 200. When a merge produces >200 keys, the finest tier is collapsed by prefix-grouping and summing. Mixed-tier merges coarsen to the coarser of the two inputs before summing.

## Information Loss

Distillation preserves aggregate statistical properties but destroys:

- **Temporal distribution** — reduced to `_first_seen`/`_last_seen`, mitigated by `_histogram`
- **Per-event correlation** — numeric ranges are global, not per-context or per-actor
- **Ordering** — event sequence within the time window is lost
- **String value long tail** — capped at 50 unique values, then just a count
- **Batch boundaries** — meta-distilled sigmas don't record which intermediate contributed what proportion
- **Attribute aggregates across re-distillation** — stripped to prevent unbounded growth; only first-generation sigmas carry full attribute statistics

## Consequences

- Old attestations are preserved as sigmas instead of silently deleted
- Database size stabilizes over time as old data compacts
- Sigmas carry `source = "distill"` and `_distill: true` in attributes for identification
- `_version` and `_rust_version` provide deployment traceability across sigma generations
- High-volume predicates with attribute-level semantic differences (e.g. `crawl-stage-changed` with 7 stages) lose granularity — the fix is upstream: emit distinct predicates per semantic category
- Sigma count is bounded by unique predicates, not by attestation volume
