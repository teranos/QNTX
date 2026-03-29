# @qntx/glyphs

A glyph is exactly one DOM element for its entire lifetime. It morphs between visual states — dot, proximity-expanded, window, panel, fullscreen — through smooth animations, but the element identity never changes.

This package is the glyph runtime: the tray, proximity engine, morph transactions, and manifestations. It has zero framework dependencies — pure DOM, Web Animations API, and dependency injection for host-specific concerns.

## Status

Extracting from `web/ts/components/glyph/` into this standalone package. The goal is a reusable runtime that any frontend can consume — QNTX is the first host, not the only one.

### Extraction progress

- [x] **Step 1: Foundation** — Config layer (`configureGlyphs()`), `Glyph` interface, dataset helpers, proximity engine. Web re-exports from package.
- [x] **Step 2: Morph infrastructure** — `morph-transaction.ts` (Web Animations API with commit/rollback), `morphology.ts` (axiom verification, morph lifecycle), `stash.ts` (DOM content preservation across morph cycles), `title-bar-controls.ts`, `render-content.ts`. Cut logger and stripHtml deps via config.
- [ ] **Step 3: Manifestations + tray** — `window.ts`, `canvas.ts` (fullscreen), `panel.ts` (resizable full-width), `run.ts` (the tray singleton). Extract standalone window drag (currently coupled to canvas-pan). Cut uiState dep via persistence config.
- [ ] **Step 4: Wire QNTX** — Call `configureGlyphs()` at app startup with QNTX's logger, persistence (uiState), and stripHtml. Update remaining import sites. Verify all morph paths end-to-end.

## Configuration

Host apps wire in their implementations at startup:

```ts
import { configureGlyphs } from '@qntx/glyphs';

configureGlyphs({
    logger: myLogger,
    logSegment: 'GLYPH',
    persistence: {
        getMinimizedGlyphs: () => state.minimizedGlyphs,
        addMinimizedGlyph: (id) => state.addMinimized(id),
        removeMinimizedGlyph: (id) => state.removeMinimized(id),
    },
    stripHtml: myStripHtml,
});
```

Without configuration, safe defaults apply: no-op logger, no-op persistence, DOMParser-based HTML stripping.

## Architecture

```
config.ts          — Dependency injection (logger, persistence, stripHtml)
glyph.ts           — Glyph interface + animation/manifestation constants
dataset.ts         — Type-safe DOM dataset attribute helpers
proximity.ts       — Pointer-distance morphing (8px dot → 220px expanded)
morph-transaction.ts  — Web Animations API with exclusivity + commit/rollback
manifestations/    — window, canvas (fullscreen), panel morph implementations
run.ts             — GlyphRun tray singleton
```

## The one law

Animation is a state transition of a persistent object, with a begin, an exclusive running period, and a commit or rollback. Only one morph transaction runs per element at a time. If a new transition begins, the existing one is cancelled (rolled back) before the new one starts.
