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

## Related Vision
- [Continuous Intelligence](./continuous-intelligence.md) - The paradigm that generates the history
- [Tile-Based Semantic UI](./tile-based-semantic-ui.md) - Visualize time-travel through tile evolution

## Implementation Roadmap

**Phase 1: Temporal Aggregation** ✅
- Duration accumulation across time windows ([test](https://github.com/teranos/QNTX/blob/55ba8b77011665f12fdd47b846d7760165429557/ats/storage/temporal_aggregation_test.go#L79))
- Metadata-based temporal filtering ([test](https://github.com/teranos/QNTX/blob/55ba8b77011665f12fdd47b846d7760165429557/ats/storage/temporal_aggregation_test.go#L117))
- Domain-agnostic query predicates

**Phase 2: Semantic Awareness** (planned)
- Weighted aggregation via relatedness scores ([test](https://github.com/teranos/QNTX/blob/55ba8b77011665f12fdd47b846d7760165429557/ats/storage/temporal_aggregation_test.go#L152))
- Embedding-based semantic distance ([test](https://github.com/teranos/QNTX/blob/55ba8b77011665f12fdd47b846d7760165429557/ats/storage/temporal_aggregation_test.go#L202))
- ML-derived contribution factors ([test](https://github.com/teranos/QNTX/blob/55ba8b77011665f12fdd47b846d7760165429557/ats/storage/temporal_aggregation_test.go#L259))

**Phase 3: Temporal Overlap Detection** (planned)
- Concurrent period merging ([test](https://github.com/teranos/QNTX/blob/55ba8b77011665f12fdd47b846d7760165429557/ats/storage/temporal_aggregation_test.go#L322))
- Ongoing activity duration calculation ([test](https://github.com/teranos/QNTX/blob/55ba8b77011665f12fdd47b846d7760165429557/ats/storage/temporal_aggregation_test.go#L381))
- Conservative double-count prevention

## Prerequisites

**Implemented:**
- ✅ Stable attestation system
- ✅ Temporal indexing via timestamp fields
- ✅ Metadata query infrastructure (JSON extraction)

**Planned:**
- 🚧 Semantic embedding models (for Phase 2)
- 🚧 Visual timeline scrubbing UI
- 🚧 Z-axis temporal layering