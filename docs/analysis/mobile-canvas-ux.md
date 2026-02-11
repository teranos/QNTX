# Mobile Canvas UX Analysis

Tauri mobile (WKWebView on iOS, WebView on Android). Primary use case: viewer/monitor. Glyph tray is the main navigation surface.

## Glyph Tray: The Critical Path

Mobile users live in the glyph tray, switching between window manifestations. This is where the biggest gaps are.

### 8px dots are untappable

Collapsed glyphs are 8×8px (`css/glyph/states/dot.css:9-10`). Apple HIG minimum is 44px, Material Design is 48dp. These dots are physically impossible to tap reliably on a phone screen.

### Proximity morphing is mouse-only — invisible glyphs on mobile

`proximity.ts:48-53` tracks mouse position via `document.addEventListener('mousemove')`. The entire proximity expansion system (8px dot → 220px labeled bar) depends on cursor hover distance. On touch devices there is no cursor — glyphs **never expand**, labels **never appear**. The user sees a column of identical 8px dots with no way to tell which is which before tapping.

`run.ts:187` pipes `mousemove` into `updateProximity()`. No `touchstart`/`touchmove` equivalent exists.

### Click-to-morph works but is blind

The click handler on each glyph element (`run.ts:89-121`) fires correctly on tap (synthetic click). Once tapped, `morphToWindow` or `morphToCanvas` executes. The morph animation and window rendering both work. But the user taps blind — they can't preview which glyph they're opening.

## Window Manifestation: Already Touch-Aware

Good news — `manifestations/window.ts:316-401` already handles both mouse and touch:

```typescript
const startDrag = (e: MouseEvent | TouchEvent) => {
    const clientX = e instanceof MouseEvent ? e.clientX : e.touches[0]?.clientX;
```

Touch listeners are registered on the title bar handle (`window.ts:399-400`):
```typescript
handle.addEventListener('mousedown', startDrag);
handle.addEventListener('touchstart', startDrag, { passive: false });
```

Window dragging, viewport clamping, and position persistence all work on touch. This is the one interaction layer that's mobile-ready.

## Canvas Manifestation: View-Only Gaps

For a viewer/monitor, the canvas doesn't need editing (drag, resize, spawn, meld). It needs to **display the current state legibly**.

### No pan or zoom — off-screen glyphs invisible

`canvas-workspace` has `overflow: hidden` (`canvas.css:10`, `canvas-glyph.ts:318`). Glyphs are positioned at absolute pixel coordinates. A canvas built on a 1920px desktop will have most glyphs beyond the 375px phone viewport with no way to see or reach them. There is no:

- Pinch-to-zoom
- Two-finger pan
- Scroll mechanism
- Minimap/overview

### Canvas glyphs are read-only anyway for a viewer — but they're off-screen

The canvas interaction layer (drag, resize, rectangle select, spawn menu) is entirely mouse-only (`glyph-interaction.ts` uses `mousedown`/`mousemove`/`mouseup` exclusively). For a viewer this is acceptable — the problem isn't that you can't edit, it's that you can't **see**.

### Spawn menu requires right-click — no mobile fallback

`canvas-glyph.ts:328` uses `contextmenu` only. Irrelevant for a viewer, but worth noting.

## Tap Target Inventory

| Element | Size | Min Required | Status |
|---|---|---|---|
| Glyph dot (tray) | 8×8px | 44×44px | **Broken** — primary nav element |
| Window minimize btn | 24×24px | 44×44px | Undersized |
| Window close btn | 24×24px | 44×44px | Undersized |
| Window title bar (drag) | 100%×36px | — | Works (touch handlers exist) |
| Canvas minimize btn | 32×32px | 44×44px | Undersized |
| Canvas action buttons | 22×22px | 44×44px | Undersized (viewer: low priority) |
| Canvas spawn buttons | 40×40px | 44×44px | Borderline (viewer: N/A) |
| Canvas resize handle | 16×16px | 44×44px | Undersized (viewer: N/A) |

## What Works on Mobile Today

| Feature | Status | Why |
|---|---|---|
| Window drag via title bar | **Works** | Touch events in `window.ts:316-401` |
| Window content display | **Works** | Flex layout, auto-resize observer |
| Canvas fullscreen morph | **Works** | Targets `window.innerWidth/Height` |
| Canvas grid overlay | **Works** | Pure CSS backgrounds |
| Glyph selection (tap) | **Works** | Synthetic click fires |
| WebSocket live updates | **Works** | Platform-independent |
| Tauri notifications | **Works** | Plugin registered in `lib.rs:29-30` |

## What Breaks on Mobile

| Feature | Impact for Viewer | Root Cause |
|---|---|---|
| Glyph tray identification | **Critical** — can't tell glyphs apart | Proximity is mouse-only, dots are 8px |
| Canvas pan/zoom | **Critical** — can't see off-screen glyphs | `overflow: hidden`, no gesture handling |
| Glyph dot tap targets | **Critical** — can't reliably tap | 8px is 5.5× below minimum |
| Window button tap targets | **Medium** — minimize/close are small | 24px buttons, no padding |
| Canvas minimize button | **Medium** — exiting canvas is awkward | 32px, positioned at top-right corner |

## Architecture Notes

- Mobile Tauri has no sidecar (`tauri.ios.conf.json` / `tauri.android.conf.json` both set `externalBin: null`). The mobile app connects to a remote QNTX server — it's inherently a viewer.
- `lib.rs:26-55` registers biometric and permission stubs for iOS/Android but no mobile-specific UI commands.
- The `responsive.css` breakpoints (768/900/1200px) handle panels and symbol palette but have **zero rules targeting the canvas workspace or glyph tray**.
- No `touch-action` CSS anywhere on canvas or tray elements — browser will intercept gestures.
