# Navigational Threads

Thread (user-facing) / Spine (code). Ordered path through element symbols on a canvas. Always visible, whisper-quiet. First thread is red.

## Creating

Right-click element symbol -> spawn menu shows 〽 (spawn menu is context-aware: background shows element types, symbol shows 〽). Click 〽 -> placement mode (scrim, 〽 cursor, symbols glow). Click symbols along the way to build the path. Place 〽 on the canvas to finish — it becomes an element, the thread's end marker. To extend later: pick up the 〽 element, connect more symbols, drop it again.

## Visual

Flowy Bezier curves between symbol positions. ~12% opacity, always red — different hues of red per thread to distinguish them. Hover brightens. Multiple threads avoid overlap.

## Navigation

Two-axis arrow nav when the selected element is on a thread.

- **←/→** — move selection to the prev/next element along the **active thread** for the current element. Pan animates to target. No-op at the first/last element (no wrap, no bump).
- **↑/↓** — selection stays put; rotate which of the current element's threads is **active**. Ordering is thread creation order. No-op when the element belongs to only one thread.

〽 is skipped during navigation — arrows step through the real elements in the spine, not the end marker.

The **active thread** is per-element state. When you return to a multi-thread element, the last-active thread on that element is restored. Visually, the active thread's curve is brightened; other threads passing through the current element are dimmed.

When the selected element is not on a thread: spatial nav (unchanged).

## Presentation

Select a thread line -> fullscreen -> ArrowLeft/ArrowRight step through -> ESC exits.

## Data

```typescript
interface Spine { id: string; color: string; nodes: string[] }
```

Frontend-only, persisted in uiState per canvas.

## Status (2026-05-27)

Working:
- Context-aware spawn menu (right-click symbol shows 〽, background shows element types)
- Thread building mode: scrim, 〽 cursor (no system mouse), symbol glow on targets
- Multi-click path construction: click symbols to add nodes, click empty canvas to finish
- Snap-to-symbol: clicks within 40px snap to nearest symbol, visual preview (scale + glow + line snaps)
- Live Bezier from origin symbol to cursor during building, extends with each node
- Permanent spine renderer: SVG Bezier curves between connected symbols, above elements
- Curves bow perpendicular to the line, alternating sides
- Hover brightens spine lines
- 〽 registered as element type, placed on canvas as thread end marker
- `.symbol` class on all element symbol spans for targeting
- Spine persistence: saved to uiState (IndexedDB), restored on canvas load, stale spines cleaned up
- Red hues palette (8 shades: crimson, dark red, salmon, maroon, bright red, brick, vermillion, wine)
- Thread deletion: deleting any element on a spine removes the entire thread (spine + 〽 end marker)
- 〽 glows in thread color when selected (no border — symbol-only element)
- **Left-click 〽 to pick up and extend**: the placed 〽 itself becomes the cursor (reparented to body, classes/styles swapped, attached to mouse). Click more symbols, drop to pin it back at the new endpoint; old spine replaced by new. Escape pins it back at the original position and restores the original spine.
- **Proximity reveal**: 〽 invisible by default, fades in when cursor within 80px (signals pick-up affordance).
- **Drag disabled on 〽**: the needle isn't a draggable element — its position is determined by where it's dropped during build/extend.
- **One DOM element across the entire lifecycle**: from initial cursor → placed → cursor (pickup) → placed (drop), the same `HTMLElement` is mutated through `pinThreadElement` / `unpinThreadElement`. No new element is ever created to represent the same needle. (Element axiom — web/CLAUDE.md.)
- **Arrow-key thread navigation** (`thread-navigation.ts`): ←/→ steps prev/next along the active spine (〽 skipped, no-op at ends); ↑/↓ rotates the active spine on multi-thread elements (wraps creation-order). Active spine is per-element state and carries forward to the target element when ←/→ moves. Off-thread arrows fall through to spatial nav; hjkl is always spatial.
- **Thread-nav camera framing** (`centerOnElementSymbol`): horizontally centers the element wrapper (so the element reads balanced); vertically places the symbol at 1/3 from the top (eye lands on the thread crossing, element body extends into visible space below). Always pans, no "already visible" margin.

Not working:
- 〽 anchoring to predecessor element (removed — position override fights with drag system)

Not started:
- Visual indicator of the **active thread** on a multi-thread element (active spine brightened, others passing through the current element dimmed). Currently ↑/↓ rotates state silently — you only see the effect on the next ←/→.
- Right-click first element's symbol (thread origin): spawn menu shows presentation mode entry
- Presentation mode (fullscreen, step through)
- Multi-thread color cycling through red hues
- Thread editing (insert/remove individual nodes mid-thread)
- First-thread onboarding message

Known issues:
- 〽 anchoring model is fragile (every-frame position override). Consider making 〽 a DOM attachment on the predecessor element instead of a separate positioned element.
- Spine opacity is 0.5 for dev visibility. Production target is 0.12.
