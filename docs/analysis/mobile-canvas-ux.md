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

### What still works

- Quick taps on dots still fire the existing click handler (the touch browse only activates when the finger stays down and moves).
- Glyph DOM axiom fully preserved — no element creation, only coordinate feeding.

## Window Manifestation: Already Touch-Aware

`packages/glyphs/window-drag.ts` handles both mouse and touch for window dragging.

## Canvas Manifestation

### Canvas Pan — Single finger drag (mobile/touch)

`canvas/canvas-pan.ts` implements touch-based panning for mobile and responsive design mode. Single finger drag anywhere on the canvas (including on glyphs) pans the viewport. Desktop uses two-finger trackpad scroll and middle mouse button drag.

Touch handlers are always set up (even on desktop) to support browser responsive design mode testing.

### Canvas zoom — Pinch-to-zoom (mobile/touch)

Two-finger pinch gesture zooms the canvas. Zoom origin tracks the pinch center so the point between your fingers stays stationary. Desktop uses Ctrl+wheel / Cmd+wheel. Both touch and desktop handlers are always registered regardless of viewport width.

### Rectangle selection — Registered at all viewport widths, mouse only

Rectangle selection (click-drag on canvas background) is registered unconditionally by `canvas/canvas-workspace-builder.ts`. `rectangle-selection.ts` binds `mousedown`/`mousemove`/`mouseup` and no touch equivalent.

### Canvas editing interactions are mouse-only

**Glyph drag, resize, spawn menu, meld** — all use `mousedown`/`mousemove`/`mouseup` exclusively. On mobile/touch devices, these interactions are not available. Glyphs can be viewed, tapped to select, and the canvas panned and zoomed.

## Offline Capability (WASM)

The browser WASM module (`web/wasm/`) provides local compute without a server connection. See [wasm-capabilities.md](../wasm-capabilities.md) for the full capability matrix and migration candidates.

## Remaining Work

| Gap | Priority | Notes |
|---|---|---|
| Unified search (SPACE to open) | High | Replace left-panel query bar with floating search overlay on canvas |
| Light mode (#221) | Medium | UI is dark-mode first |
| Touch-based glyph editing | Low | Glyph manipulation currently desktop-only |
| Remove root canvas minimize | Low | Blocked on unified search — canvas becomes permanent background |
| App Store packaging | Low | Icons, launch screen, privacy manifest — [QNTX-App](https://github.com/teranos/QNTX-App) |
