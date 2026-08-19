# Mobile Canvas UX Analysis

Tauri mobile (WKWebView on iOS, WebView on Android). A QNTX mobile app is a node in a decentralised mesh network — it can operate offline via WASM (query parsing, attestation storage in IndexedDB) and gains more capabilities by connecting to other QNTX nodes. The canvas is the primary workspace; the glyph tray is the main navigation surface.

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

- **`packages/glyphs/proximity.ts`**: `setPointerPosition(x, y)` feeds touch coords into the same `mouseX`/`mouseY` that desktop uses. `isTouchBrowsing` flag tracks active state.
- **`packages/glyphs/touch-browse.ts`**: Document-level touch listeners with an activation margin around the tray. `findPeakedGlyph()` identifies the closest glyph on release. Suppresses the synthetic click that fires after touchend to prevent double-open.
- **`packages/glyphs/run.ts:morphGlyph()`**: Extracted from the duplicated click/reattach handlers. Shared by click (desktop + quick tap) and touch browse release.

### CSS

- `touch-action: none` on `.glyph-run` prevents the browser from intercepting the vertical swipe as a page scroll.
- Mobile dots, and the gap between them, are enlarged so the tray column is visible enough to anchor the thumb.

### What still works

- Desktop mouse proximity is unchanged — same `mousemove` → `updateProximity()` path.
- Quick taps on dots still fire the existing click handler (the touch browse only activates when the finger stays down and moves).
- Glyph DOM axiom fully preserved — no element creation, only coordinate feeding.

## Window Manifestation: Already Touch-Aware

`packages/glyphs/window-drag.ts` handles both mouse and touch for window dragging. No changes needed.

## Canvas Manifestation

### Canvas Pan — Single finger drag (mobile/touch)

`canvas/canvas-pan.ts` implements touch-based panning for mobile and responsive design mode. Single finger drag anywhere on the canvas (including on glyphs) pans the viewport. Desktop uses two-finger trackpad scroll and middle mouse button drag.

Touch handlers are always set up (even on desktop) to support browser responsive design mode testing. Pan and zoom persist per-canvas through `uiState` into localStorage; stale `isPanning`/`isPinching` is reset on canvas setup.

### Canvas zoom — Pinch-to-zoom (mobile/touch)

Two-finger pinch gesture zooms the canvas. Zoom origin tracks the pinch center so the point between your fingers stays stationary. Desktop uses Ctrl+wheel / Cmd+wheel. Both touch and desktop handlers are always registered regardless of viewport width.

### Rectangle selection — Registered at all viewport widths, mouse only

Rectangle selection (click-drag on canvas background) is registered unconditionally by `canvas/canvas-workspace-builder.ts`. The one-time `isMobile` gate is gone, but `rectangle-selection.ts` binds `mousedown`/`mousemove`/`mouseup` and no touch equivalent.

### Canvas editing interactions are mouse-only

**Glyph drag, resize, spawn menu, meld** — all use `mousedown`/`mousemove`/`mouseup` exclusively. On mobile/touch devices, these interactions are not currently available. Glyphs can be viewed, tapped to select, and the canvas panned and zoomed, but dragging a glyph requires desktop.

Future work could add touch-based glyph editing via long-press, dedicated edit mode toggle, or gesture-based interactions.

## Tap Target Inventory

| Element | Status |
|---|---|
| Glyph dot (tray, mobile) | **Fixed** — sized by `restingDotSize()`, not CSS |
| Window title bar | **Fixed** |
| Window minimize btn | **Fixed** |
| Window close btn | **Fixed** |
| Canvas minimize btn | **Fixed** |
| Canvas action bar buttons | **Fixed** |
| Canvas spawn buttons | **Fixed** |
| Window title bar (drag) | Works (touch handlers exist) |

Canvas and title-bar sizing is gated behind `@media (pointer: coarse)`; the tray and palette use `max-width` queries; the dot is sized in JS. Inline `style.width`/`style.height` removed from window button creation in `window.ts` so CSS class rules (and the media query) control sizing.

## Recent Fixes

### Status Indicators (`status-indicators.ts`)
- **Fixed**: Pulse daemon touch interactions disabled on mobile
- Prevents accidental daemon stops/starts when scrolling or browsing on mobile
- Desktop click behavior unchanged

### Command Palette (`symbol-palette.css`)
- **Fixed**: Mobile command palette uses horizontal scroll instead of grid layout
- Prevents balloon sizing and lost scroll on small screens
- Cells are `flex: 0 0 auto` with `min-width`/`min-height` for touch targets
- `-webkit-overflow-scrolling: touch` for smooth momentum scrolling

### Layout (`core.css`)
- **Fixed**: White left bar artifact removed on mobile
- `#left-panel` set to `width: 0` with `overflow: visible` on mobile
- `#container` changed to `display: block` for single-column mobile layout

### Canvas auto-open (`main.ts`)
- **Fixed**: Canvas workspace opens immediately on app startup (desktop + mobile)
- Canvas is the primary workspace — no manual click required to enter it

### Safe areas (`responsive.css`)
- **Fixed**: iOS notch/Dynamic Island handled via `env(safe-area-inset-top)` on system drawer, canvas, and minimize button

## Offline Capability (WASM)

The browser WASM module (`web/wasm/`) provides local compute without a server connection. See [wasm-capabilities.md](../wasm-capabilities.md) for the full capability matrix and migration candidates.

## Remaining Work

| Gap | Priority | Notes |
|---|---|---|
| Remote node URL | High | Mobile builds no sidecar and injects no `__BACKEND_URL__`; `backendUrl()` falls back to the app's own origin |
| Unified search (SPACE to open) | High | Replace left-panel query bar with floating search overlay on canvas |
| Light mode (#221) | Medium | UI is dark-mode first; light mode is a large feature |
| Touch-based glyph editing | Low | Glyph manipulation currently desktop-only; acceptable for now |
| Remove root canvas minimize | Low | Blocked on unified search — canvas becomes permanent background |
| App Store packaging | Low | Icons, launch screen, privacy manifest — none in `web/src-tauri/` |
