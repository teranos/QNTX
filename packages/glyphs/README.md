# @qntx/glyphs

[AXIOMAS.md](AXIOMAS.md) — read it before changing anything here.

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

`title` is plain text — strip any markup before passing. `symbol` is the one symbol field and the package renders it natively: generic title bars, canvas-placed title bars, and the proximity-expanded dot all display it, through `createSymbolSpan`. `symbolElement` is not a second source of truth — it is the element-continuity carrier for the same string across a cursor → placed morph.

## Environment

Browser-only. Assumes `document`, `DOMParser`, Web Animations API, and `ResizeObserver` as globals. Not compatible with Node.js or SSR without a DOM polyfill.

## Configuration

Host apps call `configureGlyphs()` at startup to inject logger, persistence, canvas coordinate bridge, `CanvasHost`, and cleanup callbacks. `CanvasHost` bridges canvas interaction (drag, resize, meld) to host-specific state — persistence, selection, composition CRUD, and sync. See `web/ts/main.ts` for the canonical wiring. Without configuration, safe defaults apply: no-op logger, no-op persistence, no-op canvas host, identity coordinate transforms.

`dotGeometry` is the exception to "host-specific concerns": it is geometry, not a dependency. The proximity engine writes the dot's width, height and border-radius inline on every frame, so no stylesheet can reach it — a host that wants a bigger or smaller dot sets it here.

```typescript
configureGlyphs({
    dotGeometry: { minWidth: 15, minHeight: 15 },  // resting dot; omitted fields keep 10/10/220/32/2
});
```

## Examples

`bun examples/serve.ts` — live specimens, no host required.

## Testing

```bash
cd packages/glyphs
bun test                     # happy-dom (local)
USE_JSDOM=1 bun test         # JSDOM (CI)
```

Tests live with the package source. The web copies that duplicated them are gone — web tests pin host behavior (persistence round-trips, workspace wiring), package tests pin the package.

## Publishing

Published to [JSR](https://jsr.io/@qntx/glyphs) via GitHub Actions. Tests gate the publish — if tests fail, the package is not published. To release: bump `version` in `jsr.json` and merge to main. The workflow runs on any change to `packages/glyphs/` but JSR skips versions that already exist.

## Boundary

Where Glyphs ends and QNTX begins — settled by the same test each time: does it express a glyph, or does it orchestrate host state?

- **Canvas workspace orchestration is QNTX.** Pan, zoom, selection, spawn, and thread state are wired to QNTX persistence, sync, and the glyph registry. The package owns the interaction layer the workspace consumes: drag, resize, meld, placement, z-order, touch browse.
- **GlyphUI's I/O is QNTX.** `pluginFetch`, `pluginWebSocket`, `onMeld`, and config persistence belong to the host factory. The DOM building blocks (`createInput`, `createButton`, `createStatusLine`) are package-owned in `ui-primitives.ts`; the host factory delegates to them.
- **ax is QNTX, not Glyphs.** The `'ax'` manifestation type is dropped from the package. A future integration is deliberately unthought.
- **Titles arrive plain.** Callers strip markup before passing items; the package does not strip.

## Morph classes

A morph class (`glyph-morphing-to-window`, `-to-panel`, `-to-canvas`) belongs to the morph, not the glyph: `prepareMorphTo` adds it beside the glyph's own classes, and the transaction ends it — commit swaps it for the settled class (`glyph-window`, `glyph-panel …`, `canvas-fullscreen-adjusted`), rollback restores exactly the classes the glyph had. No `!important` is needed anywhere: position and stacking during a morph are inline, and `raise()` writes a plain z-index.
