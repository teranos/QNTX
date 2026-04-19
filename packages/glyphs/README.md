# @qntx/glyphs

A glyph is exactly one DOM element for its entire lifetime. It morphs between visual states — dot, proximity-expanded, window, panel, canvas — through smooth animations, but the element identity never changes.

This package is the glyph runtime: tray, proximity engine, morph transactions, manifestations, and the canvas interaction layer (drag, resize, meld). It has zero framework dependencies — pure DOM, Web Animations API, and dependency injection via `configureGlyphs()` and `CanvasHost` for host-specific concerns.

## Status

Extracting from `web/ts/components/glyph/` into this standalone package. The goal is a reusable runtime that any frontend can consume — QNTX is the first host, not the only one.

### Extraction complete

- [x] **Step 1: Foundation** — Config layer (`configureGlyphs()`), `Glyph` interface, dataset helpers, proximity engine
- [x] **Step 2: Morph infrastructure** — morph-transaction, morphology, stash, title-bar-controls, render-content
- [x] **Step 3: Manifestations + tray** — window, canvas, panel, run.ts (tray), standalone window drag
- [x] **Step 4: Wire QNTX** — `configureGlyphs()` at startup with logger, persistence, stripHtml

### Interaction extraction

Drag, resize, and the full meld pipeline are in the package. Hosts implement `CanvasHost` to bridge persistence, selection, and composition state. Remaining steps:

- [x] `CTYPE` — Composition types: `CompositionEdge`, `CompositionState` as package-native types
- [x] `EWALK` — Generic edge walker: takes edges + glyph ID, walks the DAG, returns focus/navigation graph
- [x] `GRDLP` — Grid layout from edges: `computeGridPositions` and layout application, pure geometry
- [x] `TYPS` — Extract pure type definitions (`GlyphUI`, `RenderFn`, `GlyphDef`, etc.) from web/ into package — no logic, just interfaces
- [x] `JSRP` — First JSR publish: `jsr.json`, self-contained package, `@qntx/glyphs` on jsr.io
- [x] `CPLCD` — Canvas-placed: `canvasPlaced()` factory, `applyCanvasGlyphLayout`, positioning logic
- [x] `DRAGR` — Canvas drag/resize: `makeDraggable`, `makeResizable`, z-index stacking
- [x] `MELDT` — Meld detection: proximity-based meld triggering during drag
- [x] `MELDF` — Meld feedback: visual indicators (glow, snap) during meld approach
- [x] `MELDX` — Meld execution: `performMeld`, `extendComposition`, `unmeldComposition`, `detachGlyph`
- [ ] `CNVWS` — Canvas workspace: pan, zoom, selection, rectangle select, spawn, keyboard shortcuts — generic glyph hosting
- [ ] `GLYUI` — `createGlyphUI` + SDK primitives (glyph, input, button, statusLine) into package
- [ ] `DSDMO` — Design system demo: mini-canvas with real drag-to-meld, compositions from edge data

### Cleanup

- [x] `REXP` — Eliminate re-exports: move imports in web/ to point directly at `@qntx/glyphs`
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

Host apps call `configureGlyphs()` at startup to inject logger, persistence, canvas coordinate bridge, `CanvasHost`, and cleanup callbacks. `CanvasHost` bridges canvas interaction (drag, resize, meld) to host-specific state — persistence, selection, composition CRUD, and sync. See `web/ts/main.ts` for the canonical wiring. Without configuration, safe defaults apply: no-op logger, no-op persistence, no-op canvas host, identity coordinate transforms.

## Publishing

Published to [JSR](https://jsr.io/@qntx/glyphs) via GitHub Actions. To release: bump `version` in `jsr.json` and merge to main. The workflow runs on any change to `packages/glyphs/` but JSR skips versions that already exist.

## The one law

Animation is a state transition of a persistent object, with a begin, an exclusive running period, and a commit or rollback. Only one morph transaction runs per element at a time. If a new transition begins, the existing one is cancelled (rolled back) before the new one starts.
