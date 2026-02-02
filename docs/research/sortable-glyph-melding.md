# Glyph Melding with Sortable.js

## Context
Building on the glyph melding vision in PR #399 (branch: feature/glyph-melding-vision), implement proximity-based glyph melding using Sortable.js's group connection and shared list capabilities.

## Objective
Transform the canvas into a special sortable container where glyphs can form connected groups, leveraging Sortable.js's ability to detect and merge items between lists dynamically.

## Requirements

### Core Implementation
1. Install Sortable.js and integrate it with the existing glyph system in `web/ts/components/glyph/`
2. Treat each glyph as its own sortable "group" initially
3. When glyphs get close, dynamically merge their groups to create a melded unit
4. Use Sortable's `onMove` event to detect proximity and trigger melding
5. Implement custom animation using Sortable's animation API for the melding effect

### Key Features to Leverage
- **Dynamic Groups**: Use `group.put` and `group.pull` functions to dynamically allow/prevent melding based on proximity
- **Shared Lists**: Transform individual glyphs into shared sortable lists when they meld
- **Animation API**: Use Sortable's built-in animation system for smooth melding transitions
- **MultiDrag Plugin**: Enable dragging melded compositions as a unit

### Implementation Approach
```javascript
// Each glyph starts as its own sortable container
const axGlyph = Sortable.create(axElement, {
  group: {
    name: `glyph-${axId}`,
    put: function(to, from, dragEl, event) {
      // Allow melding if prompt glyph is within range
      const distance = calculateDistance(dragEl, to.el);
      return distance < 100 && isCompatible(dragEl, to.el);
    }
  },
  animation: 150,
  ghostClass: 'glyph-melding',
  onMove: function(evt) {
    const distance = calculateDistance(evt.dragged, evt.related);
    if (distance < 100) {
      // Apply melding preview effect
      evt.dragged.classList.add('melding');
      evt.related.classList.add('meld-target');
    }
  },
  onEnd: function(evt) {
    if (shouldMeld(evt.from, evt.to)) {
      // Create a new merged sortable group
      mergeSortableGroups(evt.from, evt.to);
    }
  }
});

// MultiDrag for melded compositions
Sortable.mount(new MultiDrag());
```

### Success Criteria
- Glyphs detect proximity using Sortable's native collision detection
- When close enough, glyphs merge into a single sortable group
- The merged group can be dragged as one unit using MultiDrag
- Pulling glyphs apart splits the sortable group back into individuals
- Transitions use Sortable's smooth animation system

### Why Sortable.js?
- Built-in group merging/splitting logic perfect for melding
- Collision detection eliminates manual proximity calculations
- MultiDrag plugin handles melded compositions naturally
- Animation system designed for smooth element transitions

## References
- Original vision: `docs/vision/glyph-melding.md`
- Current implementation: `web/ts/components/glyph/meld-preview.ts`
- Sortable.js docs: https://sortablejs.github.io/Sortable/