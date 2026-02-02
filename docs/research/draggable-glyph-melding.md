# Glyph Melding with Draggable (Shopify)

## Context
Building on the glyph melding vision in PR #399 (branch: feature/glyph-melding-vision), implement proximity-based glyph melding using Draggable's sensor system and collision detection plugins.

## Objective
Create a physics-based melding system where glyphs naturally attract and repel using Draggable's advanced sensor abstraction and plugin architecture.

## Requirements

### Core Implementation
1. Install @shopify/draggable and integrate it with the existing glyph system in `web/ts/components/glyph/`
2. Use Draggable's sensor system to detect proximity during drag
3. Implement custom Collidable plugin for meld detection
4. Create a Snappable plugin that handles the magnetic attraction
5. Use Mirror plugin for visual feedback during melding

### Key Features to Leverage
- **Sensor System**: Abstract touch/mouse/keyboard with unified drag events
- **Collidable Plugin**: Built-in collision detection with customizable thresholds
- **Mirror Plugin**: Create visual previews of melded state during drag
- **Physics Module**: Natural acceleration/deceleration for magnetic feel
- **Custom Plugins**: Build a MeldPlugin that combines Draggable's capabilities

### Implementation Approach
```javascript
import { Draggable, Plugins } from '@shopify/draggable';

// Custom Meld Plugin
class MeldPlugin extends Draggable.Plugin {
  attach() {
    this.draggable.on('drag:move', this.onDragMove.bind(this));
    this.draggable.on('drag:stop', this.onDragStop.bind(this));
  }

  onDragMove(event) {
    const draggedRect = event.source.getBoundingClientRect();
    const targets = document.querySelectorAll('.prompt-glyph');

    targets.forEach(target => {
      const distance = this.calculateDistance(draggedRect, target);
      if (distance < 100) {
        // Apply magnetic force
        const force = this.calculateMagneticForce(distance);
        event.mirror.style.transform = `translateX(${force}px)`;

        // Visual melding effect
        this.applyMeldEffect(event.source, target, distance);
      }
    });
  }

  calculateMagneticForce(distance) {
    // Inverse square law for realistic magnetic feel
    return (100 - distance) * 0.3;
  }
}

// Initialize with plugins
const draggable = new Draggable(document.querySelectorAll('.glyph'), {
  draggable: '.ax-glyph, .prompt-glyph',
  plugins: [
    Plugins.Collidable,
    Plugins.Mirror,
    MeldPlugin
  ],
  collidable: {
    range: 100
  },
  mirror: {
    constrainDimensions: true,
    cursorOffsetX: 0,
    cursorOffsetY: 0
  }
});

// Handle meld/unmeld
draggable.on('collidable:in', ({collidableEvent}) => {
  if (compatibleGlyphs(collidableEvent.collidableElement)) {
    createMeldedComposition();
  }
});
```

### Success Criteria
- Sensor system provides smooth, unified drag behavior across devices
- Magnetic attraction increases naturally as glyphs approach
- Mirror plugin shows preview of melded state before release
- Collidable plugin handles all proximity detection automatically
- Physics feel natural and responsive

### Why Draggable?
- Sensor abstraction handles all input methods uniformly
- Plugin architecture allows clean separation of melding logic
- Mirror plugin perfect for showing meld previews
- Collision detection is highly optimized
- Physics system creates natural magnetic behavior

## References
- Original vision: `docs/vision/glyph-melding.md`
- Current implementation: `web/ts/components/glyph/meld-preview.ts`
- Draggable docs: https://github.com/Shopify/draggable