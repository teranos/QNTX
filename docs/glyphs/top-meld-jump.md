# Top Direction Meld: Composition Jump Bug

## Problem

When melding a chart glyph above an ax glyph (`top` direction), the composition jumps — the ax glyph shifts down from its original position after the meld completes. Detection and glow work correctly; the bug is in composition positioning.

## Root Cause

`performMeld` in `packages/glyphs/meld/meld-composition.ts` positions the composition at the initiator's (ax's) original canvas position:

```typescript
composition.style.left = initiatorElement.style.left;
composition.style.top = initiatorElement.style.top;
```

Then `applyColumnLayout` computes a grid from the edge DAG. For `right` and `bottom` edges, the initiator is always at grid position (1,1) with internal offset (0, 0) — it stays at the composition origin, matching its original canvas position. No jump.

For `top` edges, the initiator lands at row 2 (row 1 is the target above). `computeGridPositions` normalizes so min row = 1, pushing the initiator to `style.top = targetHeight`. The initiator's canvas position becomes `composition.top + targetHeight` instead of `composition.top + 0`. It shifts down by exactly the target's measured height.

The same issue exists in `extendComposition` when adding a `top` edge to an existing composition — existing glyphs shift to later rows.

## What Was Tried

### Attempt 1: parseInt compensation
After `applyColumnLayout`, read initiator's internal offset via `parseInt(initiatorElement.style.top)`, subtract from composition position. Result: still jumps, "not as much." `parseInt` truncates fractional pixels from `getBoundingClientRect / scale` measurements.

### Attempt 2: getBoundingClientRect screen-coordinate compensation
Snapshot initiator's screen position before reparenting, compare after layout, convert delta to canvas coordinates via `/ scale`. Result: still jumps. The delta computation is correct in theory but the composition is at (0,0) during layout (position not yet set), which may affect the screen rect comparison after the composition is repositioned.

### Attempt 3: parseFloat canvas-coordinate compensation
Read initiator's original `style.left/top` with `parseFloat` before clearing, read internal offset after layout with `parseFloat`, subtract. Simpler than attempt 2 — stays in canvas coordinate space, no scale conversion needed. Not yet tested at runtime.

## Why It's Hard

The values involved (`origY`, `internalY`, `scale`) are all individually correct in unit tests. The test for `checkDirectionalProximity` with `top` direction passes. The bug manifests only at runtime where:

- Canvas zoom/pan transforms affect `getBoundingClientRect` measurements
- `applyColumnLayout` measures via `getBoundingClientRect / scale` but sets `style.top` directly
- The composition position, element reparenting, and forced reflow interact in sequence
- `right`/`bottom` never exposed this because the initiator always lands at offset 0

## Files

- `packages/glyphs/meld/meld-composition.ts` — `performMeld` (line ~209) and `extendComposition` (line ~349): composition positioning after layout
- `packages/glyphs/edge-graph.ts` — `computeGridPositions`: grid normalization that pushes initiator to row 2 for `top` edges
- `packages/glyphs/meld/meld-composition.ts` — `applyColumnLayout`: element measurement and pixel offset computation

## Next Step

Add `console.log` to `performMeld` dumping `origY`, `internalY`, `scale`, `composition.style.top` before and after compensation. Compare against the visible jump in pixels. The math is straightforward — the bug is a value being wrong at runtime, and we need to see which one.
