# Glyph Attestation Flow

How attestations flow through meld compositions. The meld edge is both spatial grouping and reactive data pipeline: dragging glyphs together declares the subscription.

## Axioms

**One attestation, one execution.** A downstream glyph fires once per incoming attestation. Not a batch. Not a list. One attestation triggers one execution of the downstream glyph. If upstream produces five attestations, downstream fires five times.

**AX results are attestations.** AX doesn't return ephemeral query results. AX persists its results as attestations. Everything flowing through a meld DAG is an attestation. Uniform. No special "pass-through" path.

**Watching, not polling.** The meld edge is a live subscription. When an attestation enters the system that matches the upstream relationship, the downstream glyph fires. No `pending()`. No pull. The glyph reacts.

**The edge is the watcher.** A composition edge `from→to` declares: "when `from` produces an attestation, deliver it to `to`." The meld DAG compiles down to reactive subscriptions. Each edge IS a watcher definition scoped to the composition.

## What Exists Today

### Creation (complete)

- `attest()` builtin in the python glyph creates attestations via gRPC (`qntx-python/src/atsstore.rs`). Only builtin. No query/read functions in the python runtime.
- AX executor (`ats/ax/executor.go`) — full query engine with fuzzy matching, alias expansion, conflict detection.
- Prompt handler (`ats/so/actions/prompt/handler.go`) — backend "so" action with `TemporalCursor` for incremental processing and `SourceAttestationID` lineage.

### Reactive infrastructure (complete, disconnected from canvas)

- Watcher engine (`ats/watcher/engine.go`) — `OnAttestationCreated` fires on every new attestation, matches against filters, rate-limits, dispatches. The execution kernel is exactly what meld edges need.
- The watcher *registry* (user-defined watchers in a table) is what meld edges would subsume: each composition edge IS a watcher definition, not a separate concept.

### Meld composition (visual only)

- DAG-based edge model: compositions stored as `(from, to, direction)` edges.
- Meldability rules: `ax→py`, `ax→prompt`, `py→py`, `py→prompt` on the right axis; results flow down (`web/ts/components/glyph/meld/meldability.ts`).
- Graph utilities: `getRootGlyphIds()`, `getLeafGlyphIds()`, `computeGridPositions()` — topology is fully walkable.
- No execution awareness. Melding is purely spatial today. Clicking play on a glyph executes in isolation.

### Glyph execution (isolated)

- Py glyph POSTs code to `/api/python/execute`, auto-melds result glyph below. Has a TODO at `py-glyph.ts:118` about creating attestations for execution but not implemented.
- Prompt glyph detects `{{variables}}` but shows "connect to AX glyph (coming soon)" error. Direct (no-variable) prompts work.

## The Model

### Meld edge = reactive subscription

```
[ax: contact] → [py: enrich] → [prompt: summarize {{subject}}]
```

When the user melds these three glyphs, the system creates two subscriptions:

1. `ax→py`: When ax produces an attestation (from its query), deliver it to py
2. `py→prompt`: When py produces an attestation (via `attest()`), deliver it to prompt

Each subscription watches for attestations where `actor` matches the upstream glyph identity (e.g. `glyph:{glyph_id}`).

### Execution flow

```
1. User clicks play on [ax: contact]
2. AX queries the attestation store
3. For each result, AX persists it as an attestation
   with actor: glyph:{ax_glyph_id}
4. Watcher engine sees new attestation
5. Composition edge ax→py matches (actor = upstream glyph)
6. py glyph fires with that ONE attestation injected
7. py code runs, calls attest() with its own results
   with actor: glyph:{py_glyph_id}
8. Watcher engine sees new attestation
9. Composition edge py→prompt matches
10. prompt glyph fires with that ONE attestation
11. {{subject}}, {{predicate}}, etc. resolve from the attestation
12. LLM runs, result attestation created
13. Result glyph appears below prompt
```

Each step is one attestation in, one execution, zero or more attestations out.

### What the python glyph receives

No `pending()`. No `query()`. The attestation is injected as a variable into the execution context, like `attest()` is today:

```python
# `upstream` is the single attestation that triggered this execution
# injected by the runtime, like attest() is

print(upstream.subjects)     # ["alice@example.com"]
print(upstream.predicates)   # ["contact"]
print(upstream.contexts)     # ["crm"]
print(upstream.attributes)   # {"phone": "555-1234", ...}

# Do work with it
enriched = lookup_something(upstream.subjects[0])

# Produce output attestation (triggers downstream if melded)
attest(
    subjects=upstream.subjects,
    predicates=["enriched"],
    contexts=["pipeline"],
    attributes={"original": upstream.id, "enriched_data": enriched}
)
```

When the py glyph is NOT in a meld (standalone), `upstream` is `None`. The glyph works as it does today — user writes code, clicks play, it runs.

### What the prompt glyph receives

The prompt glyph has no user code. Variable resolution is automatic. The incoming attestation's fields map to `{{template}}` placeholders per the existing template syntax in `ats/so/actions/prompt/doc.go`:

- `{{subject}}` / `{{subjects}}`
- `{{predicate}}` / `{{predicates}}`
- `{{context}}` / `{{contexts}}`
- `{{actor}}` / `{{actors}}`
- `{{attributes.key}}`
- `{{id}}`

Same delivery mechanism (one attestation triggers one execution), different interface (template interpolation vs code).

### Cursor / deduplication

The watcher engine's existing `OnAttestationCreated` hook fires once per new attestation. Since each attestation has a unique ASID and is immutable, the natural deduplication is: fire once per ASID per edge. The edge either has or hasn't seen a given ASID.

A `(composition_id, edge_from, edge_to, last_processed_asid, last_processed_timestamp)` table tracks what each edge has consumed. This is the per-edge analog of the prompt handler's `TemporalCursor`.

On system restart or composition reconstruction, the cursor ensures we don't re-fire for attestations already processed.

### AX as attestation producer

Today AX returns ephemeral `AxResult` structs. For the meld flow to work uniformly, AX needs to persist its query results as attestations when executing within a composition:

```
Subject: [original attestation's subject]
Predicate: ax-result
Context: [the query that found it, e.g. "ax contact"]
Actor: glyph:{ax_glyph_id}
Attributes: {original_id: "...", query: "contact", ...}
```

This means the downstream py/prompt glyph receives an attestation with full provenance: what was queried, by which glyph, linking back to the original.

When AX runs standalone (not in a meld), behavior stays as-is: ephemeral results displayed in the UI.

## What Needs Building

### 1. AX attestation persistence (backend)

When an AX glyph executes within a composition, persist each result as an attestation tagged with `actor: glyph:{ax_glyph_id}`. Standalone AX execution unchanged.

- Modify AX executor or add composition-aware wrapper
- Actor convention: `glyph:{id}` for all canvas glyph executions
- New attestation source: `"canvas-ax"` (distinguishes from CLI ax)

### 2. Composition-to-subscription compiler (backend)

Given a composition's edges, produce watcher subscriptions:

```
Edge (glyph-A → glyph-B, direction: right)
  →  Watch for: actor == "glyph:{glyph-A-id}"
     Deliver to: glyph-B's execution endpoint
```

Could reuse watcher engine's `matchesFilter` + `executeAction` kernel. The filter is: `actors: ["glyph:{from_id}"]`. The action is: execute the downstream glyph with the matching attestation.

Open question: does this compile eagerly (on meld creation) or lazily (on play)?

### 3. Glyph execution with injected attestation (backend + Rust)

Python plugin needs to receive an attestation object and inject it as `upstream`:
- New gRPC field on the execute request: `upstream_attestation`
- Rust side: deserialize into a Python dict, inject as global `upstream`
- When no upstream: `upstream = None`

Prompt handler already does this via template interpolation. Just needs to receive the attestation from the composition subscription rather than from its own AX query.

### 4. Edge cursor table (backend, storage)

```sql
CREATE TABLE composition_edge_cursors (
    composition_id TEXT NOT NULL,
    from_glyph_id TEXT NOT NULL,
    to_glyph_id TEXT NOT NULL,
    last_processed_id TEXT NOT NULL,
    last_processed_at DATETIME NOT NULL,
    PRIMARY KEY (composition_id, from_glyph_id, to_glyph_id)
);
```

Updated after each successful downstream execution. Consulted on restart to avoid reprocessing.

### 5. Frontend: composition-aware play (frontend)

When user clicks play on a root glyph in a meld:
- Resolve composition edges from DOM/state
- Send composition context to backend with the execution request
- Backend activates subscriptions for downstream edges
- Downstream glyph UIs show reactive status (watching/firing/complete)

When user clicks play on a non-root glyph:
- If it has upstream edges: starts watching for upstream attestations (activates its incoming subscriptions)
- If standalone: executes as today

### 6. Actor convention for canvas glyphs

All attestations created by canvas glyph execution carry `actor: glyph:{glyph_id}`. This is the linkage that makes edge-scoped watching work. Needs to flow through:
- Python plugin execute request → `attest()` default actor
- AX composition wrapper → result attestation actor
- Prompt handler → result attestation actor

## Relationship to Watcher System

The watcher engine's execution kernel (`OnAttestationCreated` → filter match → rate limit → dispatch) is reusable. The watcher *registry* (user-created watchers with their own filters) remains a separate, global concept for server-side automation.

Meld edges are canvas-scoped, visual, spatial watchers. They compile to the same filter-match-dispatch pattern but are defined by dragging glyphs together rather than writing filter JSON.

Long-term, the watcher registry UI could itself become a meld composition: a glyph that watches, melded to a glyph that acts. But that's later.

## Infinite Loop Prevention

Since downstream glyphs produce attestations, and upstream glyphs watch for attestations, cycles are possible if the DAG has loops. Prevention:

1. **DAG enforcement at meld time** — the composition is a DAG by construction (meldability rules + port constraints prevent cycles today). `computeGridPositions` does BFS from roots, which only works on DAGs.

2. **Actor scoping** — edge subscriptions filter on `actor: glyph:{upstream_id}`. A glyph's own attestations won't match its incoming edge (different actor). Loops require an explicit cycle in the DAG, which (1) prevents.

3. **Edge cursor** — even if somehow retriggered, the cursor ensures each ASID is processed at most once per edge.

## What This Unlocks

- `[ax: customer] → [py: score] → [prompt: summarize]` — attestation-driven enrichment pipelines on the canvas
- `[py: sensor] → [py: transform] → [py: alert]` — chained python processing, each step attested
- `[ax: new-tickets] → [prompt: triage]` — reactive LLM triage as tickets enter the system
- `[py: ingest] → [py: validate] → [py: enrich] → [prompt: classify]` — multi-stage data pipelines
- Canvas compositions become runnable, attestable, inspectable workflows with full provenance
