# @qntx/glyphs (GlyphUI) — Handover

What was built, what was fixed in review, what needs design decisions before going further.

---

## What shipped

`glyph-ui.ts` (renamed from `plugin-glyph-sdk.ts`) — `GlyphUI` interface injected into plugin render functions. Exposes `container` (canvasPlaced wrapper), `preventDrag`, `pluginFetch` (scoped to `/api/{plugin}/`), structured logging, cleanup registration, and DOM helpers (`input`, `button`, `statusLine`), plus `loadConfig()`/`saveConfig()` for attestation-based config persistence.

`glyph-module-loader.ts` (renamed from `plugin-glyph-module.ts`) — Dynamic import loader. When `GlyphDef` declares a `module_url`, the frontend imports the JS module, creates a `GlyphUI` instance, calls `render(glyph, ui)`, and mounts the returned element. Falls back to error placeholder on failure.

`@qntx/glyphs` — Types-only package at `packages/glyphs/`. Plugin repos import types (`GlyphUI`, `RenderFn`, `Glyph`, etc.) for type-safe glyph development. Runtime is injected by the host via the `ui` parameter.

`GlyphDef.ModulePath` — Added across Go struct, proto, gRPC server/client, HTTP handler. The `module_url` field appears alongside `content_url` in `/api/plugins/glyphs` JSON. Both paths coexist — plugins without `ModulePath` use the legacy HTML pipeline unchanged.

`qntx-ix-json` (v0.4.0) — First consumer. Glyph module authored in TypeScript (`ix-glyph-module.ts`), compiled to JS via `bun build`, embedded via `//go:embed`, served at `GET /ix-glyph-module.js`. Legacy HTML pipeline removed. The Go-side API handlers (test-fetch, update-config, set-mode) are untouched.

## What was fixed in review

**Module cache poisoning.** The original `loadModule` cached the import promise unconditionally. If the import rejected (network error, plugin not ready), every subsequent glyph of that type got the same rejected promise forever with no retry path. Fixed: cache on success, evict on failure so the next spawn retries the import.

**Cleanup drop before container.** `ui.onCleanup(fn)` silently discarded `fn` if called before `ui.container()` because `rootElement` was null. Fixed: cleanups registered before `container()` are queued and flushed when `container()` is called.

**Dead stub.** `loadPersistedConfig` was an empty function with a TODO comment. Removed — dead code with no execution path.

**Bogus JSDoc paths.** `@param` annotations in `ix-glyph-module.js` referenced `./plugin-glyph-sdk` which doesn't exist relative to the served file. Removed the annotations.

---

## Open questions — needs design decisions

### ~~1. Config hydration for module-path glyphs~~ — RESOLVED

Server-owned endpoint `GET/POST /api/glyph-config` queries ATSStore directly using the ix-json convention (subject=`{plugin}-glyph-{glyphID}`, predicate=`configured`). `GlyphUI` exposes `loadConfig()` / `saveConfig()` — plugins call `await ui.loadConfig()` on mount. ix-json is the first consumer.

### ~~2. Hand-edited .pb.go~~ — RESOLVED

Regenerated via `make proto`. The hand-edited version had the correct struct but wrong raw descriptor bytes (wire format).

### ~~3. Plugin module bundling / build pipeline~~ — RESOLVED

Plugin modules are authored in TypeScript (importing types from `@qntx/glyphs`) and compiled to JS via `bun build` before `go build`. The ix-json Makefile has a `build-module` target that compiles `web/ix-glyph-module.ts` → `web/ix-glyph-module.js`. Type-only imports are erased during compilation, producing a self-contained JS module for `go:embed`.

### 4. GlyphUI surface expansion

The current `GlyphUI` exposes a minimal set. Things explicitly *not* included that may be needed:

| Primitive | Why it was left out |
|-----------|-------------------|
| `uiState` subscription | Couples plugins to internal state shape |
| `connectivityManager` | Narrow use case, unclear if plugins need it |
| `tooltip.attach()` | No tooltip system in the GlyphUI yet |
| `subscribe(event, cb)` for WS/SSE | Needs a stable event contract |
| Meld participation | Requires `getMeldOptions` + `getGlyphClass` integration |
| Manifestation switching | Plugin glyphs are canvasPlaced-only today (review doc §3b, vision doc's universal manifestation principle) |

Each of these is a separate decision about what contract plugins get.

### 5. Legacy HTML pipeline deprecation

`content_url` and `module_url` coexist. ix-json uses `module_url` only (legacy pipeline removed). `qntx-atproto` still uses the legacy HTML pipeline. Should `content_url` eventually be removed? The legacy pipeline has known problems (innerHTML XSS surface, script re-execution, no cleanup, global scope pollution), but removing it is a breaking change for any out-of-tree plugins.

### 6. Shared Go `escapeHTML` (review doc §3d)

`qntx-atproto` still has `escapeHTML`/`escapeHTMLAttr` functions. Worth extracting to `plugin/html` if any plugin continues to use the HTML pipeline. ix-json no longer needs them.
