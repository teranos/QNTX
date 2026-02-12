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

## Canvas Manifestation

For a viewer/monitor, the canvas needs to display the current state legibly and allow navigation.

### Canvas Pan — Single finger drag (mobile/touch)

`canvas-pan.ts` implements touch-based panning for mobile and responsive design mode. Single finger drag anywhere on the canvas (including on glyphs) pans the viewport. Desktop uses two-finger trackpad scroll and middle mouse button drag.

Touch handlers are always set up (even on desktop) to support browser responsive design mode testing.

### Canvas editing interactions are mouse-only

**Glyph drag, resize, rectangle select, spawn menu, meld** — all use `mousedown`/`mousemove`/`mouseup` exclusively. On mobile/touch devices, these interactions are not currently available. Glyphs can be viewed and the canvas panned, but glyph manipulation requires desktop.

Future work could add touch-based glyph editing via long-press, dedicated edit mode toggle, or gesture-based interactions.

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

## Recent Fixes

### Status Indicators (`status-indicators.ts`)
- **Fixed**: Pulse daemon touch interactions disabled on mobile (`max-width: 768px`)
- Prevents accidental daemon stops/starts when scrolling or browsing on mobile
- Desktop click behavior unchanged

### Command Palette (`symbol-palette.css`)
- **Fixed**: Mobile command palette uses horizontal scroll instead of grid layout
- Prevents balloon sizing and lost scroll on small screens
- Cells are `flex: 0 0 auto` with `min-width/height: 48px` for touch targets
- `-webkit-overflow-scrolling: touch` for smooth momentum scrolling

### Layout (`core.css`)
- **Fixed**: White left bar artifact removed on mobile
- `#left-panel` set to `width: 0` with `overflow: visible` on mobile
- `#container` changed to `display: block` for single-column mobile layout

### Canvas Pan (`canvas-pan.ts`)
- **Fixed**: Touch-based canvas panning for mobile and responsive design mode
- Single finger drag anywhere on canvas pans the viewport
- Desktop uses two-finger trackpad scroll and middle mouse button drag
- Touch handlers always active to support responsive design mode testing
- Pan state persists per-canvas in localStorage

## Remaining Work

| Gap | Priority | Notes |
|---|---|---|
| Canvas zoom for mobile viewer | Medium | Pan implemented; pinch-to-zoom would improve navigation |
| Touch-based glyph editing | Low | Glyph manipulation currently desktop-only; acceptable for viewer use case |
