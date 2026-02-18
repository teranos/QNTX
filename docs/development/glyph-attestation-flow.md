# Glyph Attestation Flow

How attestations flow through meld compositions. The meld edge is both spatial grouping and reactive data pipeline: dragging glyphs together declares the subscription.

See [AXIOMS.md](../AXIOMS.md) for the attestation flow axioms.

## What Exists Today

### Creation (complete)

- `attest()` builtin in the python glyph creates attestations via gRPC (`qntx-python/src/atsstore.rs`). Only builtin. No query/read functions in the python runtime.
- AX executor (`ats/ax/executor.go`) — full query engine with fuzzy matching, alias expansion, conflict detection.
- Prompt handler (`ats/so/actions/prompt/handler.go`) — backend "so" action with `TemporalCursor` for incremental processing and `SourceAttestationID` lineage.

### Glyph execution (isolated)

- Prompt glyph detects `{{variables}}` but shows "connect to AX glyph (coming soon)" error. Direct (no-variable) prompts work.

## The Model

### Meld edge = reactive subscription

```
[ax: contact] → [py: enrich] → [prompt: summarize {{subject}}]
```

When the user melds these three glyphs, two subscriptions compile eagerly:

1. `ax→py`: AX is a filter, always live. Its query (`subjects: contact`) becomes the subscription filter. Any new attestation matching that filter triggers py.
2. `py→prompt`: Py is a producer. The subscription filter is `actor == glyph:{py_glyph_id}`. When py calls `attest()`, the resulting attestation triggers prompt.

### Two edge types

| Source glyph | Subscription filter | Why |
|-------------|-------------------|-----|
| **ax** (filter) | The AX glyph's query filter directly | AX is a pure filter — it doesn't create attestations, it selects them. The filter definition IS the subscription. |
| **py / prompt** (producer) | `actor == glyph:{upstream_id}` | Producers create new attestations tagged with their glyph ID. The edge watches for attestations from that specific glyph. |

### Execution flow

```
1. User melds [ax: contact] → [py: enrich] → [prompt: summarize]
   Subscriptions compile immediately:
     - ax→py: filter = {subjects: ["contact"]}
     - py→prompt: filter = {actor: "glyph:{py_id}"}

2. Attestation enters the system matching "contact"
   (via CLI, another glyph, API — any source)
3. ax→py subscription fires
4. py glyph executes with that ONE attestation as `upstream`
5. py code runs, calls attest() with enriched data
   actor: glyph:{py_glyph_id}
6. py→prompt subscription fires
7. prompt glyph executes with that ONE attestation
8. {{subject}}, {{predicate}}, etc. resolve from the attestation
9. LLM runs, result attestation created
10. Result glyph appears below prompt
```

Each step is one attestation in, one execution, zero or more attestations out.

### AX is always live

AX has no play button. It is always running — this is the current state. When ax→py melds, the subscription starts delivering immediately. No backfill step, no manual trigger. The edge cursor initializes at meld time; attestations matching the AX filter from that point forward flow through.

Existing attestations from before the meld are not retroactively delivered. The subscription is forward-looking from the moment of assembly. The edge cursor marks the boundary.

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

### AX is a pure filter, not a producer

AX doesn't create attestations. It queries existing ones. When AX is the root of a meld, its query filter becomes the subscription filter for the outgoing edge. The attestations that flow downstream are the original attestations from the store — not copies, not wrappers.

This means:
- No "ax-result" intermediate attestations cluttering the store
- The downstream glyph receives the real attestation with its original subjects, predicates, contexts, actors
- AX is stateless: it defines *what to watch for*, not *what to produce*
- Standalone AX behavior is unchanged: query, display results in UI

## What Needs Building

### Prompt glyph template interpolation from upstream attestation

- Replace "coming soon" error with actual `{{variable}}` resolution from the incoming attestation
- Same delivery mechanism as py glyph (one attestation triggers one execution), different interface (template interpolation vs code)

## Relationship to Watcher System

The watcher engine's execution kernel (`OnAttestationCreated` → filter match → rate limit → dispatch) is reusable. The watcher *registry* (user-created watchers with their own filters) remains a separate, global concept for server-side automation.

Meld edges are canvas-scoped, visual, spatial watchers. They compile to the same filter-match-dispatch pattern but are defined by dragging glyphs together rather than writing filter JSON.

Long-term, the watcher registry UI could itself become a meld composition: a glyph that watches, melded to a glyph that acts. But that's later.

## Infinite Loop Prevention

Since downstream glyphs produce attestations, and edges watch for attestations, cycles are possible if the DAG has loops. Prevention:

1. **DAG enforcement at meld time** — the composition is a DAG by construction (meldability rules + port constraints prevent cycles today). `computeGridPositions` does BFS from roots, which only works on DAGs.

2. **Actor scoping on producer edges** — py→downstream and prompt→downstream subscriptions filter on `actor: glyph:{upstream_id}`. A glyph's own attestations don't match its incoming edge (different actor). Loops require an explicit cycle in the DAG, which (1) prevents.

3. **Edge cursor** — even if somehow retriggered, the cursor ensures each ASID is processed at most once per edge. This is the final safety net for all edge types including AX filter edges.

## What This Unlocks

- `[ax: customer] → [py: score] → [prompt: summarize]` — attestation-driven enrichment pipelines on the canvas
- `[py: sensor] → [py: transform] → [py: alert]` — chained python processing, each step attested
- `[ax: new-tickets] → [prompt: triage]` — reactive LLM triage as tickets enter the system
- `[py: ingest] → [py: validate] → [py: enrich] → [prompt: classify]` — multi-stage data pipelines
- Canvas compositions become runnable, attestable, inspectable workflows with full provenance
