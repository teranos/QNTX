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
- **Left-click 〽 to pick up and extend**: re-enters build mode pre-loaded with the spine; click more symbols, drop to extend; Escape restores. Old spine + 〽 hidden during build; new spine replaces old on drop.
- **Proximity reveal**: 〽 invisible by default, fades in when cursor within 80px (signals pick-up affordance).
- **Drag disabled on 〽**: the needle isn't a draggable glyph — its position is determined by where it's dropped during build/extend.
- **One DOM element across lifecycle**: cursor element from build mode is handed off and reused as the placed 〽 (glyph axiom; see web/CLAUDE.md).

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
- Pickup creates a fresh cursor element while the placed 〽 sits hidden — two DOM elements coexist for the duration of the extend gesture. Strict letter of the axiom is satisfied (only one carries `data-glyph-id`), but the spirit isn't. Proper fix: have `enterThreadBuildingMode` accept an existing element and convert the placed 〽 → cursor on pickup, cursor → placed on drop.
- 〽 anchoring model is fragile (every-frame position override). Consider making 〽 a DOM attachment on the predecessor glyph instead of a separate positioned element.
- Spine opacity is 0.5 for dev visibility. Production target is 0.12.
