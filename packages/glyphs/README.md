# @qntx/glyphs

A glyph is exactly one DOM element for its entire lifetime. It morphs between visual states — dot, proximity-expanded, window, panel, canvas — through smooth animations, but the element identity never changes.

This package is the glyph runtime: tray, proximity engine, morph transactions, manifestations, and the canvas interaction layer (drag, resize, meld). It has zero framework dependencies — pure DOM, Web Animations API, and dependency injection via `configureGlyphs()` and `CanvasHost` for host-specific concerns.

## Core pattern

Every glyph renderer follows the same shape: take a `Glyph`, return a DOM element.

```typescript
import type { Glyph } from '@qntx/glyphs';

function createMyGlyph(glyph: Glyph): HTMLElement {
    // build DOM from glyph.id, glyph.title, glyph.content, glyph.symbol
    // return a single element — the glyph's identity for its entire lifetime
}
```

The `Glyph` interface is the universal input contract. 19 renderers in QNTX follow this pattern. The package owns the type; renderers live in the host.

## Environment

Browser-only. Assumes `document`, `DOMParser`, Web Animations API, and `ResizeObserver` as globals. Not compatible with Node.js or SSR without a DOM polyfill.

## Configuration

Host apps call `configureGlyphs()` at startup to inject logger, persistence, canvas coordinate bridge, `CanvasHost`, and cleanup callbacks. `CanvasHost` bridges canvas interaction (drag, resize, meld) to host-specific state — persistence, selection, composition CRUD, and sync. See `web/ts/main.ts` for the canonical wiring. Without configuration, safe defaults apply: no-op logger, no-op persistence, no-op canvas host, identity coordinate transforms.

## Testing

```bash
cd packages/glyphs
bun test                     # happy-dom (local)
USE_JSDOM=1 bun test         # JSDOM (CI)
```

Tests live with the package source. Some tests are duplicated in `web/ts/` where they originated — the web copies may be removed over time.

## Publishing

Published to [JSR](https://jsr.io/@qntx/glyphs) via GitHub Actions. Tests gate the publish — if tests fail, the package is not published. To release: bump `version` in `jsr.json` and merge to main. The workflow runs on any change to `packages/glyphs/` but JSR skips versions that already exist.

## The one law

Animation is a state transition of a persistent object, with a begin, an exclusive running period, and a commit or rollback. Only one morph transaction runs per element at a time. If a new transition begins, the existing one is cancelled (rolled back) before the new one starts.

## Deferred

These items are intentionally deferred — the boundary isn't clear enough yet to extract without creating premature abstractions:

- `CNVWS` — Canvas workspace (pan, zoom, selection, keyboard shortcuts). Some subsystems are host-independent (selection, breadcrumb, keyboard-shortcuts) but the orchestrator is deeply coupled to QNTX state. Needs to mature further inside the host before extraction.
- `GLYUI` — `createGlyphUI` factory + SDK primitives. The DOM building blocks (input, button, statusLine) are pure, but the I/O methods (pluginFetch, pluginWebSocket, onMeld) are host-coupled. Extraction makes sense when a second consumer appears.
- `AXMT` — Resolve `'ax'` manifestation type: inline-on-canvas editing may be a generic behavior, not AX-specific.
- `STRP` — Proximity engine's stripHtml coupling: callers should strip before passing items, not the package.
