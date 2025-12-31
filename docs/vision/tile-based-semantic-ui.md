# Tile-Based Semantic UI - Vision

**Status:** Aspirational - Core concepts for future UI evolution

## Core Concept

Transform entity visualization from **nodes as dots** to **tiles as always-on surfaces** - a paradigm shift toward pane-based semantic computing where data is visible without interaction.

## Key Principles

### 1. Tiles as Surfaces, Not Tooltips

**Current Pattern:** Hover to reveal entity details
**Vision:** Data always visible on tile surface

Tiles are **persistent information surfaces** displaying contextual data at all zoom levels. No interaction required to see entity state.

### 2. Semantic Zoom (5 Discrete Levels)

Progressive detail disclosure through **meaning density**, not pixel scaling:

| Level | Scale | Display | Use Case |
|-------|-------|---------|----------|
| **0** | < 50% | Entity type symbol only (⌬ ≡ ꩜) | High-level landscape |
| **1** | 50-100% | Type + label | Entity identification |
| **2** | 100-150% | Label + key fields (2-3) | Working context |
| **3** | 150-200% | Detailed metadata (5-7 fields) | Deep inspection |
| **4** | > 200% | Graph slice (relationships embedded) | Entity neighborhood |

**Philosophy:** Each zoom level reveals **semantically meaningful** information, not just larger pixels.

### 3. Pane-Based Computing

Inspired by Smalltalk's pane model - tiles are **compositional surfaces** that can be:
- Arranged in layouts (grid, hierarchy, timeline, pipeline)
- Configured per entity type
- Stacked and organized by user

### 4. Config-Driven Field Display

Define what data appears at each zoom level:

```toml
# tiles/contact.tile.toml
[fields]
zoom_0 = ["⌬"]  # Symbol only
zoom_50 = ["⌬", "name"]
zoom_100 = ["⌬", "name", "org", "role"]
zoom_150 = ["⌬", "name", "org", "role", "last_contact", "status"]
zoom_200 = ["relationships"]  # Embed graph slice
```

Non-technical users (recruiters, analysts) can customize views by editing TOML configs.

## Design Goals

### Visual Hierarchy
- **Type → Label → Fields → Detail → Context**
- Each zoom level serves distinct use case
- Clear visual differentiation between levels

### Layout Flexibility
- **Views (⧉) as Layout Modes:**
  - Grid: Alphabetical/chronological arrangement
  - Hierarchy: Parent-child tree structure
  - Timeline: Temporal progression
  - Pipeline: Workflow stages
  - Graph: Force-directed relationships (current default)

### Always-On Data
- Most important fields visible without interaction
- Hover/click for supplementary actions, not primary data
- Tile surface = **first-class information display**

## Technical Approach

### Progressive Enhancement Path

1. **Phase 1:** Render entities as rectangles (vs circles)
2. **Phase 2:** Add multi-line text to tile surface
3. **Phase 3:** Implement semantic zoom thresholds
4. **Phase 4:** Add layout modes (grid, hierarchy)
5. **Phase 5:** Config-driven field selection

### Implementation Considerations

- Compatible with existing D3/graph visualization
- Works with WebSocket real-time updates
- Responsive to different screen sizes
- Smooth transitions between zoom levels
- Tile size adapts to content (min/max bounds)

## Symbols at Different Zoom Levels

Core QNTX symbols appear progressively:

- **⌬** (Actor/Agent) - Zoom level 0+
- **≡** (Configuration) - Zoom level 2+ (if entity has config)
- **꩜** (Pulse) - Zoom level 2+ (if entity has async jobs)
- **⧉** (View) - Controls layout mode switching

## Use Cases

### Landscape Overview (Zoom 0)
Scan 100+ entities by type symbols only - quick pattern recognition.

### Working Context (Zoom 2)
See 10-20 entities with key fields visible - make decisions without clicking.

### Deep Inspection (Zoom 3)
Focus on 2-5 entities with full metadata - detailed comparison.

### Neighborhood Exploration (Zoom 4)
Single entity with embedded relationship graph - understand context.

## Comparison to Current UI

| Aspect | Current (Nodes) | Vision (Tiles) |
|--------|----------------|----------------|
| **Default state** | Label on hover | Key fields always visible |
| **Zoom behavior** | Pixel scaling | Semantic detail levels |
| **Layout** | Force-directed physics | Intentional layouts (grid/tree/timeline) |
| **Customization** | Hardcoded | Config-driven per entity type |
| **Information density** | Low (interaction required) | High (data always on) |

## Open Questions

1. **Tile sizing:** Fixed dimensions vs dynamic based on content?
2. **Transition animations:** Smooth zoom interpolation or discrete snapping?
3. **Mobile experience:** How do semantic zoom levels work on touch devices?
4. **Performance:** Can we render 1000+ tiles without degradation?
5. **Hybrid mode:** Support both graph and tile views simultaneously?

## Success Criteria

A successful tile-based UI should:
- ✅ Show more information without user interaction
- ✅ Support multiple layout modes for different workflows
- ✅ Enable non-technical users to customize via config
- ✅ Scale from 10 to 1000+ entities
- ✅ Maintain real-time update responsiveness
- ✅ Preserve existing graph capabilities where valuable

## Related Concepts

- **Semantic Zoom** - Progressive detail disclosure
- **Pane-Based Computing** - Smalltalk's compositional window model
- **Information Density** - Tufte's principles of data visualization
- **Always-On Interfaces** - Data visibility without interaction

## When to Build This

**Prerequisites:**
1. Stable entity model and attestation system
2. Clear use cases demanding more visible data
3. User feedback that current graph UI is limiting
4. Resources for UI/UX iteration

**Trigger conditions:**
- Users frequently hover to see data (interaction overhead)
- Need to compare multiple entities visually
- Different workflows require different layouts (not just force-directed)
- Non-technical users want to customize views

## Status

**Current:** Vision document - concepts not yet implemented

**Future:** Consider prototype when core attestation system stabilizes and user feedback indicates need for enhanced visualization.

## Key Architectural Patterns

### Real-Time Updates via WebSocket

**Delta update protocol** for live tile changes:
- Client subscribes to view updates
- Server pushes only changed tiles (not full re-query)
- Client merges updates into existing tile set
- Update types: `tile_update`, `tile_added`, `tile_removed`

**Benefits:** Efficient bandwidth usage, smooth live updates, maintains user context.

### Result Limiting Strategy

**Hard cap at 500 tiles per view:**
- Backend returns `limited: true` flag when capped
- Frontend shows warning banner
- User prompted to refine query or switch to more specific view

**Rationale:** Prevent performance degradation, encourage focused views over "show everything."

### Fail-Fast Config Validation

**System refuses to start with invalid configs:**
- TOML syntax validation at parse time
- Schema validation for required fields
- Reference validation (tile types must exist)
- Clear error messages with line numbers

**Philosophy:** Runtime reliability over permissive startup. Better to fail immediately than serve broken views.

### Per-View Zoom State Persistence

Each view maintains independent zoom level:
- Saved to session storage
- Restored when switching between views
- Users can zoom in on detail view, zoom out on landscape view
- State lost on browser close (intentional - fresh start each session)

### Orthogonal Edge Rendering

**L-shaped connectors** (not straight lines):
- Vertical then horizontal paths between tiles
- Clearer visual hierarchy in grid/tree layouts
- Edges hidden by default, appear on hover
- Labels shown only when edges visible

### Responsive Column Layout

**Viewport-aware grid:**
- Mobile (< 640px): 1 column
- Tablet (640-1024px): 2 columns
- Desktop (1024-1536px): 3 columns
- Wide (> 1536px): 4 columns

**Hierarchy respect:** Grid arrangement maintains parent-child relationships across column breaks.

### Field Visibility Based on Rendered Size

**Not just zoom level - actual pixel dimensions:**
- Each field has `min_size` threshold (e.g., 200px)
- Fields appear when tile width exceeds threshold
- Allows different tile types to have different size requirements
- Smooth progressive disclosure as tiles grow

### Caching Strategy

**Session cache only:**
- Tile data cached in memory during session
- Cleared on page reload (no persistent cache)
- WebSocket updates keep cache fresh
- Avoids cache invalidation complexity

**Trade-off:** Slight reload penalty for simpler architecture and no stale data risk.

## Related Documentation

- **Symbols System:** [`sym/README.md`](../../sym/README.md) - QNTX symbol definitions
