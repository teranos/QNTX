# Mobile Canvas UX Analysis

Tauri mobile (WKWebView on iOS, WebView on Android). Primary use case: viewer/monitor. Glyph tray is the main navigation surface.

## Glyph Tray: Touch Browse

The tray's desktop interaction (mouse proximity morphing) has been extended to touch.

### How it works

Touch near the tray enters browse mode. Thumb slides up/down through glyphs — the same proximity morphing pipeline (easing, baseline boost, text fade) drives the visual feedback. Lifting the thumb opens the glyph with highest proximity.

```
touchstart near tray → browse mode, preventDefault (block scroll)
touchmove            → feed coordinates into proximity system
touchend             → find peaked glyph → morphToWindow/Canvas → collapse rest
```

### Implementation

- **`proximity.ts`**: `setPointerPosition(x, y)` feeds touch coords into the same `mouseX`/`mouseY` that desktop uses. `isTouchBrowsing` flag tracks active state.
- **`run.ts:setupTouchBrowse()`**: Document-level touch listeners with a 44px activation margin around the tray. `findPeakedGlyph()` identifies the closest glyph on release. Suppresses the synthetic click that fires ~300ms after touchend to prevent double-open.
- **`run.ts:morphGlyph()`**: Extracted from the duplicated click/reattach handlers. Shared by click (desktop + quick tap) and touch browse release.

### CSS

- `touch-action: none` on `.glyph-run` prevents the browser from intercepting the vertical swipe as a page scroll.
- Mobile dots enlarged to 12×12px (from 8×8px) with 6px gap (from 2px) so the tray column is visible enough to anchor the thumb.

### What still works

- Desktop mouse proximity is unchanged — same `mousemove` → `updateProximity()` path.
- Quick taps on dots still fire the existing click handler (the touch browse only activates when the finger stays down and moves).
- Glyph DOM axiom fully preserved — no element creation, only coordinate feeding.

## Window Manifestation: Already Touch-Aware

`manifestations/window.ts:316-401` handles both mouse and touch for window dragging. No changes needed.

## Canvas Manifestation: Remaining Gaps

For a viewer/monitor, the canvas doesn't need editing. It needs to display the current state legibly.

### No pan or zoom — off-screen glyphs invisible

`canvas-workspace` has `overflow: hidden`. Glyphs at absolute pixel coordinates from a desktop session are beyond the phone viewport with no way to see or reach them. There is no pinch-to-zoom, two-finger pan, scroll mechanism, or minimap. This is the next critical gap after the tray.

### Canvas editing interactions are mouse-only (acceptable for viewer)

Drag, resize, rectangle select, spawn menu, meld — all use `mousedown`/`mousemove`/`mouseup` exclusively. For a viewer this is acceptable.

## Tap Target Inventory

| Element | Desktop | Touch (`pointer: coarse`) | Status |
|---|---|---|---|
| Glyph dot (tray, mobile) | 8×8px + 44px activation zone | 12×12px + 44px zone | **Fixed** — touch browse bypasses dot size |
| Window title bar | 32px tall | 44px tall | **Fixed** |
| Window minimize btn | 24×24px | 44×44px, 20px font | **Fixed** |
| Window close btn | 24×24px | 44×44px, 20px font | **Fixed** |
| Canvas minimize btn | 32×32px | 48×48px, 20px font | **Fixed** |
| Canvas action bar buttons | 22×22px | 40×40px, 16px font | **Fixed** |
| Canvas spawn buttons | 40×40px | 48×48px, 22px font | **Fixed** |
| Window title bar (drag) | 100%×32px | 100%×44px | Works (touch handlers exist) |

All touch sizing is gated behind `@media (pointer: coarse)` — desktop unchanged. Inline `style.width`/`style.height` removed from window button creation in `window.ts` so CSS class rules (and the media query) control sizing.

## Remaining Work

| Gap | Priority | Notes |
|---|---|---|
| Canvas pan/zoom for mobile viewer | **Critical** | Off-screen glyphs invisible on phone |
