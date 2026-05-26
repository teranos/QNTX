# Navigational Threads

Thread (user-facing) / Spine (code). Ordered path through glyph symbols on a canvas. Always visible, whisper-quiet. First thread is red.

## Creating

Right-click glyph symbol -> spawn menu shows 〽 (spawn menu is context-aware: background shows glyph types, symbol shows 〽). Click 〽 -> placement mode (scrim, 〽 cursor, symbols glow). Click symbols along the way to build the path. Place 〽 on the canvas to finish — it becomes a glyph, the thread's end marker. To extend later: pick up the 〽 glyph, connect more symbols, drop it again.

## Visual

Flowy Bezier curves between symbol positions. ~12% opacity, always red — different hues of red per thread to distinguish them. Hover brightens. Multiple threads avoid overlap.

## Navigation

Glyph on a thread selected: ArrowLeft/ArrowRight follow thread order. Not on a thread: spatial (unchanged). Pan animates to target.

## Presentation

Select a thread line -> fullscreen -> ArrowLeft/ArrowRight step through -> ESC exits.

## Data

```typescript
interface Spine { id: string; color: string; nodes: string[] }
```

Frontend-only, persisted in uiState per canvas.

## Status (2026-05-26)

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

Not working:
- 〽 anchoring to predecessor glyph (removed — position override fights with drag system)

Not started:
- Removing a glyph from a thread
- Right-click 〽 end marker: spawn menu shows delete thread option
- Right-click first glyph's symbol (thread origin): spawn menu shows presentation mode entry
- ArrowLeft/ArrowRight navigation along thread
- Presentation mode (fullscreen, step through)
- Multi-thread support (color cycling through red hues)
- Thread editing (insert/remove nodes)
- Left-click 〽 to pick up and extend thread (currently drag-only, does nothing useful)
- First-thread onboarding message

Known issues:
- 〽 anchoring model is fragile (every-frame position override). Consider making 〽 a DOM attachment on the predecessor glyph instead of a separate positioned element.
- Spine opacity is 0.5 for dev visibility. Production target is 0.12.
