# Glyph Melding with interact.js

## Context
Building on the glyph melding vision in PR #399 (branch: feature/glyph-melding-vision), implement proximity-based glyph melding using interact.js's powerful snapping and gesture system.

## Objective
Create a proof-of-concept where dragging an ax glyph near a prompt glyph causes them to magnetically snap together and move as one unit, leveraging interact.js's built-in physics and snapping modifiers.

## Requirements

### Core Implementation
1. Install interact.js and integrate it with the existing glyph system in `web/ts/components/glyph/`
2. Make ax and prompt glyphs draggable using interact.js
3. Implement magnetic snapping when glyphs are within 100px
4. When snapped, glyphs should visually meld (edges morph together) and move as a single unit
5. Use interact.js's `restrictEdges` and `snap` modifiers for the magnetic behavior

### Key Features to Leverage
- **Snap Modifiers**: Use `interact.modifiers.snap()` with custom targets that dynamically update based on glyph positions
- **Inertia**: Add `inertia: true` for physics-based throwing, making the melding feel more natural
- **Gesture Events**: Use interact's gesture system to detect when glyphs are being pulled apart (pinch/pull gesture)
- **Drop Zones**: Make prompt glyphs act as dropzones that preview the meld during hover

### Implementation Approach
```javascript
interact('.ax-glyph').draggable({
  modifiers: [
    interact.modifiers.snap({
      targets: [
        // Dynamically create snap points based on nearby prompt glyphs
        function (x, y, interaction) {
          const prompts = document.querySelectorAll('.prompt-glyph');
          // Return closest prompt edge as snap target
        }
      ],
      range: 100,  // Magnetic range
      relativePoints: [{ x: 1, y: 0.5 }], // Right edge of ax
      endOnly: false  // Snap during drag, not just on release
    })
  ],
  listeners: {
    move: (event) => {
      // Update visual melding based on snap state
      if (event.modifiers[0].inRange) {
        // Apply melding visual effect
      }
    }
  },
  inertia: {
    resistance: 30,
    minSpeed: 200,
    endSpeed: 100
  }
});
```

### Success Criteria
- Glyphs snap together smoothly when edges are within 100px
- Melded glyphs can be dragged as a single unit
- Pulling with sufficient force (using interact's velocity detection) breaks the meld
- The snap behavior feels magnetic and natural using interact's physics engine

### Why interact.js?
- Built-in snapping system eliminates manual proximity calculations
- Physics engine provides natural magnetic feel
- Gesture system can handle complex unmeld interactions
- Battle-tested with complex drag scenarios

## References
- Original vision: `docs/vision/glyph-melding.md`
- Current implementation: `web/ts/components/glyph/meld-preview.ts`
- interact.js docs: https://interactjs.io/docs/snapping/