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
- **Stream Time** - Position within a video/data stream (e.g., VidStream frame timestamps)
- **Window Width** - How much temporal context to show (affects graph filtering)
- **Cumulative State** - Query accumulated values across time windows, not just point-in-time snapshots

## Visual Concept: Z-Axis Time Layering
- Recent attestations render on top, fully opaque
- Older attestations fade into background (z-axis depth)
- Click faded tiles to "jump back" to that time
- Rolling window follows scrubber position

## AI Integration
- VidStream replay synchronized with attestation timeline
- AI inference results tied to both stream time and system time
- Graph evolution animated as video plays
- All three layers (video, AI predictions, graph) move together
- Future: semantic-aware temporal queries using ML-derived relatedness scores

## Technical Foundation
- History depth managed by bounded storage (already implemented)
- Temporal aggregation via GROUP BY queries over attestation metadata
- Duration predicates enable accumulation across time windows
- Alternate timelines possible through attestation abstraction
- Navigate via timestamp indexes and parent_id chains
- Integrates with time-series data management for metrics and performance data
- Rolling window filters for continuous monitoring

## Future: Self-Describing Temporal Schemas

**Vision:** Ingesters attest their own temporal structure, making QNTX fully domain-agnostic.

**Current (hardcoded):**
```
# Code knows temporal field names
metadata: { "start_time": "2020-01-01", "duration_months": "36" }
```

**Future (attested):**
```
# Ingestor declares temporal schema via attestations
ingester:example -> has_temporal_field -> "start_time"
ingester:example -> has_duration_field -> "duration_months"
ingester:example -> temporal_unit -> "months"
ingester:example -> temporal_format -> "RFC3339"

# Queries discover structure dynamically
ax * over 5y since "2020"  # System reads attestations to know HOW to aggregate
```

**Benefits:**
- **Zero hardcoded conventions** - no assumptions about field names
- **Cross-domain composability** - any temporal data works without code changes
- **Dynamic query planning** - aggregation strategy derived from attestations
- **Self-describing ingesters** - like `node_type` attestations, but for temporal properties

This extends the attestation abstraction to time itself, completing QNTX's domain-agnostic vision.

## Related Vision
- [Continuous Intelligence](./continuous-intelligence.md) - The paradigm that generates the history
- [Tile-Based Semantic UI](./tile-based-semantic-ui.md) - Visualize time-travel through tile evolution

## Implementation Roadmap

**Phase 1: Temporal Aggregation** ✅ **COMPLETE**
- Duration accumulation across time windows
  - Test: [TestTemporalAggregation_SimpleSum](../../ats/storage/temporal_aggregation_test.go#L241)
- Metadata-based temporal filtering with `since`
  - Test: [TestTemporalAggregation_WithSinceFilter](../../ats/storage/temporal_aggregation_test.go#L370)
- Domain-agnostic query predicates (seconds, minutes, hours, months, years)
- SQL injection protection via whitelisted duration fields
- Multiple test domains: [neural activity](../../ats/storage/temporal_aggregation_test.go), [meetings](../../ats/storage/temporal_aggregation_meetings_test.go)

**Phase 2: Semantic Awareness** (planned)
- Weighted aggregation via relatedness scores
  - Test: [TestTemporalAggregation_SemanticWeightedSum](../../ats/storage/temporal_aggregation_test.go#L497) (skipped)
- Combined temporal + semantic filtering
  - Test: [TestTemporalAggregation_CombinedTemporalAndSemantic](../../ats/storage/temporal_aggregation_test.go#L638) (skipped)
- Multiple predicate AND logic
  - Test: [TestTemporalAggregation_MultiplePredicatesAND](../../ats/storage/temporal_aggregation_test.go#L1061) (skipped)

**Phase 3: Temporal Overlap Detection** (planned)
- Concurrent period merging to prevent double-counting
  - Test: [TestTemporalAggregation_OverlapDetection](../../ats/storage/temporal_aggregation_test.go#L781) (skipped)
  - Meeting example: [TestMeetingTemporalAggregation_OverlapDetection](../../ats/storage/temporal_aggregation_meetings_test.go#L398) (skipped)
- Ongoing activity duration calculation (missing end_time)
  - Test: [TestTemporalAggregation_OngoingActivity](../../ats/storage/temporal_aggregation_test.go#L922) (skipped)

## Prerequisites

**Implemented:**
- ✅ Stable attestation system
- ✅ Temporal indexing via timestamp fields
- ✅ Metadata query infrastructure (JSON extraction)

**Planned:**
- 🚧 Semantic embedding models (for Phase 2)
- 🚧 Visual timeline scrubbing UI
- 🚧 Z-axis temporal layering