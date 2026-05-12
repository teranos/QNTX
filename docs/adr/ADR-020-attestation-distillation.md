# ADR-020: Attestation Distillation

Date: 2026-05-12
Status: Accepted

## Context

Bounded storage enforcement deletes attestations when limits are exceeded (16 per actor/context, 64 contexts per actor, 64 actors per subject). At steady state with high-volume predicates, enforcement fires on every `put()`, evicting one attestation per write. This is pure data loss — evicted attestations are destroyed with no trace beyond a `storage_events` log entry.

## Decision

Distillation preserves aggregate data from evicted attestations by folding them into a single "distill attestation" before deletion. Distill attestations are normal attestations — they participate in enforcement like any other, enabling recursive meta-distillation where distill attestations themselves get folded into coarser summaries over time.

**Why built-in, not a plugin:** Distillation requires atomic read-merge-delete. The gRPC plugin interface does not expose deletion.

## Two Distillation Paths

### 1. Enforcement-triggered (Rust, `crates/qntx-sqlite/src/enforcement.rs`)

Fires when bounded limits are exceeded. The eviction batch is loaded, distilled into a summary, originals deleted, summary inserted. Operates within a single SQLite transaction.

### 2. Age-triggered (Go, `server/distill_pulse.go`)

A Pulse handler that runs on a configurable interval. Queries attestations older than `max_age_hours`, groups by predicate, creates one distill attestation per predicate group, deletes originals. Handles meta-distillation: old distill attestations are re-distilled alongside regular attestations.

Also performs ghost row cleanup (NULL/empty IDs, zero timestamps) at the start of each cycle.

## Half-Bound Threshold

Instead of evicting at `limit`, enforcement triggers at `limit + limit/2`. For actor_context_limit=16, enforcement triggers at 24 and evicts down to 16. This amortizes enforcement cost (1 cycle per 8 writes instead of per write) and produces meaningful batches for distillation.

## Insert Ordering

In the Rust enforcement path, the distill attestation is inserted AFTER deleting originals. This prevents the distill insert from inflating the enforcement counter before deletion. The delete count is `count - limit + 1` — the `+1` compensates for the distill attestation that will be inserted after deletion, which increments the counter back up by 1. Without this, enforcement leaves `limit + 1` attestations instead of `limit`.

## Distill Attestation Shape

```
subjects:   ["distill:<predicate>"]
predicates: ["distill:<predicate>"]
actors:     [union of evicted actors, capped at 50]
contexts:   [union of evicted contexts, capped at 50]
source:     "distill"
attributes: {
  _distill: true,
  _count: 8,
  _total: 342,
  _subjects_count: 45,
  _subjects_sample: ["sub_1", "sub_2", ...],      // up to 10
  _first_seen: "2026-05-04T...",
  _last_seen:  "2026-05-12T...",
  _version: "v0.8.0 (fd7326d)",                   // Go binary version
  _rust_version: "0.2.3",                          // Cargo workspace version
  some_number: {min: 0, max: 4500, sum: 14640, count: 8},
  some_string: {values: ["a", "b"], count: 2},
  some_constant: "abc123"                          // constant across all -- kept as scalar
}
```

## Attribute Merging

- **Number** -> `{min, max, sum, count}` — avg derived as sum/count
- **String** -> `{values: [...], count}` — values capped at 50, then just count. If constant across all attestations, kept as scalar.
- **Already-aggregated** (from prior distill, detected by `{min, max, sum}` keys) -> merge: min the mins, max the maxes, sum the sums, add counts; union value sets
- **Distill metadata keys** (`_distill`, `_count`, `_total`, `_first_seen`, `_last_seen`, `_version`, `_rust_version`) are skipped during merging and rebuilt from the batch

## Observation Counting: `_count` vs `_total`

`_count` is the batch size — how many attestations were folded in this distill cycle. `_total` is the transitive count of original observations across all distill generations. For a first-generation distill of 8 raw attestations: `_count = 8`, `_total = 8`. When two such distill attestations are meta-distilled: `_count = 2` (two attestations in this batch), `_total = 16` (8 + 8 original observations).

`_total` is computed by summing each input's `_total` (falling back to `_count`, then to 1 for raw attestations). This preserves the full observation lineage regardless of how many meta-distillation cycles have occurred.

## Meta-Distillation

Distill attestations are normal attestations. When they age past `max_age_hours`, the Pulse handler re-distills them alongside any regular attestations in the same predicate group.

### Prefix Stacking Prevention

Each distillation cycle prefixes predicates with `distill:`. Without prevention, meta-distillation produces `distill:distill:distill:...`. The builder strips all existing `distill:` prefixes before adding one.

## Version Tracking

Each distill attestation records `_version` (Go binary version + commit hash) and `_rust_version` (Cargo workspace version from `crates/qntx-sqlite`). This creates traceable lineage — when `max_age_hours` is lowered incrementally, the Rust patch version is bumped each time, making it possible to identify which distill cycle produced a given attestation.

Go and Rust versions are independent. Rust version lives in `Cargo.toml` workspace version.

## Configuration

```toml
[distill]
interval_seconds = 120    # omit to disable
max_age_hours = 350
batch_size = 500           # default
dry_run = false            # default
```

Enforcement-path distillation has no configuration — it is always on when enforcement runs.

## Consequences

- Old attestations are preserved as aggregates instead of silently deleted
- Database size stabilizes over time as old data compacts
- Distill attestations carry `source = "distill"` and `_distill: true` in attributes for identification
- `_version` and `_rust_version` provide deployment traceability across distillation cycles
