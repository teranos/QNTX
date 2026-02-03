# Glyph Melding with GridStack.js

## Context
Building on the glyph melding vision in PR #399 (branch: feature/glyph-melding-vision), implement proximity-based glyph melding using GridStack's grid-based positioning and widget merging capabilities.

## Objective
Transform the canvas into an intelligent grid where glyphs can occupy multiple cells when melded, leveraging GridStack's native support for resizable, nested widgets.

## Requirements

### Core Implementation
1. Install GridStack.js and integrate it with the existing glyph system in `web/ts/components/glyph/`
2. Configure canvas as a GridStack grid with fine granularity (50x50 cells)
3. Glyphs start as 1-cell widgets that can merge into multi-cell compositions
4. Use GridStack's collision detection for proximity-based melding
5. Leverage float mode for free positioning while maintaining grid benefits

### Key Features to Leverage
- **Dynamic Grid Resizing**: Melded glyphs automatically expand to occupy multiple grid cells
- **Nested Grids**: Melded compositions become sub-grids containing individual glyphs
- **Collision Detection**: Built-in detection of widget overlap and proximity
- **Magnetic Snapping**: Native grid snapping provides magnetic feel
- **Animate API**: Smooth transitions during meld/unmeld operations

### Implementation Approach
```javascript
// Initialize GridStack with fine grid for smooth movement
const grid = GridStack.init({
  cellHeight: 10,
  cellWidth: 10,
  float: true, // Allow free positioning
  animate: true,
  resizable: {
    handles: 'none' // We handle melding, not manual resize
  },
  dragIn: '.glyph',
  dragInOptions: {
    appendTo: 'body'
  }
});

// Custom meld detection during drag
grid.on('dragstop', (event, el) => {
  const widget = el.gridstackNode;
  const nearby = findNearbyWidgets(widget, 10); // Within 10 grid cells

  nearby.forEach(target => {
    if (shouldMeld(widget, target)) {
      meldWidgets(widget, target);
    }
  });
});

function meldWidgets(widget1, widget2) {
  // Create nested grid for composition
  const composition = grid.addWidget({
    x: Math.min(widget1.x, widget2.x),
    y: Math.min(widget1.y, widget2.y),
    w: widget1.w + widget2.w,
    h: Math.max(widget1.h, widget2.h),
    subGrid: {
      children: [widget1, widget2]
    }
  });

  // Animate the melding
  grid.update(composition, {
    x: composition.x,
    y: composition.y
  });

  // Remove original widgets
  grid.removeWidget(widget1.el, false);
  grid.removeWidget(widget2.el, false);
}

// Enable magnetic edges
grid.on('drag', (event, el) => {
  const widget = el.gridstackNode;
  const nearby = findNearbyWidgets(widget, 10);

  if (nearby.length > 0) {
    // Apply magnetic force by adjusting position
    const target = nearby[0];
    if (Math.abs(widget.x + widget.w - target.x) < 3) {
      // Snap right edge to left edge
      grid.update(el, { x: target.x - widget.w });
    }
  }
});

// Unmeld on force pull
grid.on('drag', (event, el) => {
  if (el.gridstackNode.subGrid) {
    const velocity = calculateDragVelocity(event);
    if (velocity > threshold) {
      unmeldComposition(el);
    }
  }
});
```

### Success Criteria
- Grid provides natural snapping points for glyph alignment
- Melded glyphs form proper multi-cell widgets
- Nested grids maintain individual glyph identity within compositions
- Magnetic snapping happens automatically through grid system
- Animations are smooth using GridStack's built-in transitions

### Why GridStack.js?
- Grid system naturally handles alignment and spacing
- Built-in support for nested widgets (perfect for compositions)
- Collision detection works out-of-the-box
- Magnetic snapping is a core feature
- Resizing/repositioning animations are optimized

## References
- Original vision: `docs/vision/glyph-melding.md`
- Current implementation: `web/ts/components/glyph/meld-preview.ts`
- GridStack docs: https://gridstackjs.com/