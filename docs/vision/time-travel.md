# Time-Travel

Navigate QNTX's attestation history to understand how knowledge evolved.

## Core Concept
Every attestation has a timestamp (✦) and causal links (⟶), creating a complete navigable history of system intelligence.

## What Makes It Unique
- **Causal chains** - See why things happened, not just what
- **Live reconstruction** - Rebuild exact system state at any moment
- **Cross-domain** - Attestations span code, data, and decisions
- **AI-queryable** - LLMs can understand and navigate the timeline

## Primary Use Case
Watch knowledge evolution - how the system's understanding developed over time through continuous learning cycles.

## Temporal Dimensions
- **System Time** - When events occurred in QNTX's overall timeline
- **Stream Time** - Position within a data stream
- **Window Width** - How much temporal context to show (affects graph filtering)
- **Cumulative State** - Query accumulated values across time windows, not just point-in-time snapshots

## Visual Concept: Z-Axis Time Layering
- Recent attestations render on top, fully opaque
- Older attestations fade into background (z-axis depth)
- Click faded tiles to "jump back" to that time
- Rolling window follows scrubber position

## AI Integration
- AI inference results tied to both stream time and system time
- Graph evolution animated as stream plays
- Future: semantic-aware temporal queries using ML-derived relatedness scores

## Technical Foundation
- History depth managed by bounded storage
- Temporal range queries (`since`, `until`, `between`)
- Alternate timelines through attestation abstraction
- Rolling window filters for continuous monitoring

## Future: Self-Describing Temporal Schemas

**Vision:** Ingesters attest their own temporal structure, making QNTX fully domain-agnostic.

```
# Ingestor declares temporal schema via attestations
ingester:example -> has_temporal_field -> "start_time"
ingester:example -> has_duration_field -> "duration_months"
ingester:example -> temporal_unit -> "months"
ingester:example -> temporal_format -> "RFC3339"
```

**Benefits:**
- **Zero hardcoded conventions** - no assumptions about field names
- **Cross-domain composability** - any temporal data works without code changes
- **Dynamic query planning** - aggregation strategy derived from attestations
- **Self-describing ingesters** - like `node_type` attestations, but for temporal properties

This extends the attestation abstraction to time itself, completing QNTX's domain-agnostic vision.

## Related Vision
- [Continuous Intelligence](./continuous-intelligence.md) - The paradigm that generates the history
- [Glyphs](./glyphs.md) - Attestable glyph state enables time-travel UI
- [Fractal Workspace](./fractal-workspace.md) - Visualize time-travel through glyph manifestations

## Roadmap

**Phase 1: Temporal Range Queries** ✅
- `since`, `until`, `on`, `between` temporal expressions

**Phase 2: Semantic Awareness** (planned)
- Weighted aggregation via relatedness scores
- Combined temporal + semantic filtering

---

> **Reference (XTDB temporal SQL):** XTDB v2's bitemporal query extensions (`FOR VALID_TIME AS OF`, `FOR SYSTEM_TIME AS OF`) are a well-designed ergonomic reference if ATS temporal expressions grow beyond `since`/`until`/`on`/`between`. XTDB distinguishes valid time (when the fact was true) from system time (when the system recorded it) — a distinction that maps to attestation time vs. ingestion time.

> **Footnote (OverFilter):** Duration aggregation (`over 5y`) was explored as a query-time accumulation mechanism — summing duration predicates across attestations per subject to answer "who has over N years of X?" It was removed: the complexity of plugin-provided numeric predicates, unit conversion, and temporal windowing didn't justify itself. If temporal accumulation returns, it belongs in a materialized view or plugin, not the query path.

> **Footnote (QueryExpander):** Natural language semantic expansion (`"is engineer"` → `role=engineer OR title=engineer`) was explored as a Go interface (`QueryExpander`) that domain-specific implementations could use to expand predicates into semantic equivalents. It was removed: the abstraction added complexity without a concrete implementation beyond NoOp. If semantic expansion returns, it belongs as attestation-based expansion rules (expansions are themselves attestations) or as a plugin service (e.g., MeiliSearch via ADR-015), not baked into the query path.