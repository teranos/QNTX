# Navigational Threads

Thread (user-facing) / Spine (code). Ordered path through glyph symbols on a canvas. Always visible, whisper-quiet. First thread is red.

## Creating

Right-click glyph symbol -> spawn menu shows 〽 (spawn menu is context-aware: background shows glyph types, symbol shows 〽). Click 〽 -> placement mode (scrim, 〽 cursor, symbols glow). Click symbols along the way to build the path. Place 〽 on the canvas to finish — it becomes a glyph, the thread's end marker. To extend later: pick up the 〽 glyph, connect more symbols, drop it again.

## Visual

Flowy Bezier curves between symbol positions. ~12% opacity, always red — different hues of red per thread to distinguish them. Hover brightens. Multiple threads avoid overlap.

## Navigation

Two-axis arrow nav when the selected glyph is on a thread.

- **←/→** — move selection to the prev/next glyph along the **active thread** for the current glyph. Pan animates to target. No-op at the first/last glyph (no wrap, no bump).
- **↑/↓** — selection stays put; rotate which of the current glyph's threads is **active**. Ordering is thread creation order. No-op when the glyph belongs to only one thread.

〽 is skipped during navigation — arrows step through the real glyphs in the spine, not the end marker.

The **active thread** is per-glyph state. When you return to a multi-thread glyph, the last-active thread on that glyph is restored. Visually, the active thread's curve is brightened; other threads passing through the current glyph are dimmed.

When the selected glyph is not on a thread: spatial nav (unchanged).

## Presentation

Select a thread line -> fullscreen -> ArrowLeft/ArrowRight step through -> ESC exits.

## Data

```typescript
interface Spine { id: string; color: string; nodes: string[] }
```

Frontend-only, persisted in uiState per canvas.

## Status (2026-05-27)

Working:
- Context-aware spawn menu (right-click symbol shows 〽, background shows glyph types)
- Thread building mode: scrim, 〽 cursor (no system mouse), symbol glow on targets
- Multi-click path construction: click symbols to add nodes, click empty canvas to finish
- Snap-to-symbol: clicks within 40px snap to nearest symbol, visual preview (scale + glow + line snaps)
- Live Bezier from origin symbol to cursor during building, extends with each node
- Permanent spine renderer: SVG Bezier curves between connected symbols, above glyphs
- Curves bow perpendicular to the line, alternating sides
- Hover brightens spine lines
- 〽 registered as glyph type, placed on canvas as thread end marker
- `.glyph-symbol` class on all glyph symbol spans for targeting
- Spine persistence: saved to uiState (IndexedDB), restored on canvas load, stale spines cleaned up
- Red hues palette (8 shades: crimson, dark red, salmon, maroon, bright red, brick, vermillion, wine)
- Thread deletion: deleting any glyph on a spine removes the entire thread (spine + 〽 end marker)
- 〽 glows in thread color when selected (no border — symbol-only glyph)
- **Left-click 〽 to pick up and extend**: the placed 〽 itself becomes the cursor (reparented to body, classes/styles swapped, attached to mouse). Click more symbols, drop to pin it back at the new endpoint; old spine replaced by new. Escape pins it back at the original position and restores the original spine.
- **Proximity reveal**: 〽 invisible by default, fades in when cursor within 80px (signals pick-up affordance).
- **Drag disabled on 〽**: the needle isn't a draggable glyph — its position is determined by where it's dropped during build/extend.
- **One DOM element across the entire lifecycle**: from initial cursor → placed → cursor (pickup) → placed (drop), the same `HTMLElement` is mutated through `pinThreadGlyph` / `unpinThreadGlyph`. No new element is ever created to represent the same needle. (Glyph axiom — web/CLAUDE.md.)

Not working:
- 〽 anchoring to predecessor glyph (removed — position override fights with drag system)

Not started:
- Right-click first glyph's symbol (thread origin): spawn menu shows presentation mode entry
- ArrowLeft/ArrowRight navigation along thread
- Presentation mode (fullscreen, step through)
- Multi-thread support (color cycling through red hues)
- Thread editing (insert/remove nodes)
- First-thread onboarding message

Known issues:
- 〽 anchoring model is fragile (every-frame position override). Consider making 〽 a DOM attachment on the predecessor glyph instead of a separate positioned element.
- Spine opacity is 0.5 for dev visibility. Production target is 0.12.
