# @qntx/glyphs

A glyph is exactly one DOM element for its entire lifetime. It morphs between visual states — dot, proximity-expanded, window, panel, canvas — through smooth animations, but the element identity never changes.

This package is the glyph runtime: the tray, proximity engine, morph transactions, and manifestations. It has zero framework dependencies — pure DOM, Web Animations API, and dependency injection for host-specific concerns.

## Status

Extracting from `web/ts/components/glyph/` into this standalone package. The goal is a reusable runtime that any frontend can consume — QNTX is the first host, not the only one.

### Extraction complete

- [x] **Step 1: Foundation** — Config layer (`configureGlyphs()`), `Glyph` interface, dataset helpers, proximity engine
- [x] **Step 2: Morph infrastructure** — morph-transaction, morphology, stash, title-bar-controls, render-content
- [x] **Step 3: Manifestations + tray** — window, canvas, panel, run.ts (tray), standalone window drag
- [x] **Step 4: Wire QNTX** — `configureGlyphs()` at startup with logger, persistence, stripHtml

### Follow-up

- `REXP` — Eliminate re-exports: move imports in web/ to point directly at `@qntx/glyphs`
- `TEST` — Move tests to live with the package code, not in web/
- `EXAM` — Canonical examples: minimal host app consuming the package
- `DSGN` — Design system integration: make sure the broader design system uses this
- `DOCS` — Better documentation
- `CICD` — Package gets its own CI
- `CONF` — `configureGlyphs()` contract: clarify what's host-specific vs package defaults as more consumers appear
- `AXMT` — Resolve `'ax'` manifestation type tension: inline-on-canvas may be generic, not AX-specific
- `BROW` — DOM environment assumptions: `document`, `DOMParser`, Web Animations API are all assumed globals
- `STRP` — Proximity engine's stripHtml coupling: callers should strip before passing items, not the package

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
manifestations/    — window, canvas, panel morph implementations
run.ts             — GlyphRun tray singleton
```

## The one law

Animation is a state transition of a persistent object, with a begin, an exclusive running period, and a commit or rollback. Only one morph transaction runs per element at a time. If a new transition begins, the existing one is cancelled (rolled back) before the new one starts.
