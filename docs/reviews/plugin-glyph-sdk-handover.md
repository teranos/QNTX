# Plugin Glyph SDK — Handover

What was built, what was fixed in review, what needs design decisions before going further.

---

## What shipped

`plugin-glyph-sdk.ts` — SDK interface injected into plugin render functions. Exposes `container` (canvasPlaced wrapper), `preventDrag`, `pluginFetch` (scoped to `/api/{plugin}/`), structured logging, cleanup registration, and DOM helpers (`input`, `button`, `statusLine`).

`plugin-glyph-module.ts` — Dynamic import loader. When `GlyphDef` declares a `module_url`, the frontend imports the JS module, creates an SDK instance, calls `render(glyph, sdk)`, and mounts the returned element. Falls back to error placeholder on failure.

`GlyphDef.ModulePath` — Added across Go struct, proto, gRPC server/client, HTTP handler. The `module_url` field appears alongside `content_url` in `/api/plugins/glyphs` JSON. Both paths coexist — plugins without `ModulePath` use the legacy HTML pipeline unchanged.

`qntx-ix-json` (v0.4.0) — First consumer. Glyph module embedded via `//go:embed`, served at `GET /ix-glyph-module.js`. The Go-side API handlers (test-fetch, update-config, set-mode) are untouched.

## What was fixed in review

**Module cache poisoning.** The original `loadModule` cached the import promise unconditionally. If the import rejected (network error, plugin not ready), every subsequent glyph of that type got the same rejected promise forever with no retry path. Fixed: cache on success, evict on failure so the next spawn retries the import.

**Cleanup drop before container.** `sdk.onCleanup(fn)` silently discarded `fn` if called before `sdk.container()` because `rootElement` was null. Fixed: cleanups registered before `container()` are queued and flushed when `container()` is called.

**Dead stub.** `loadPersistedConfig` was an empty function with a TODO comment. Removed — dead code with no execution path.

**Bogus JSDoc paths.** `@param` annotations in `ix-glyph-module.js` referenced `./plugin-glyph-sdk` which doesn't exist relative to the served file. Removed the annotations.

---

## Open questions — needs design decisions

### 1. Config hydration for module-path glyphs

The legacy HTML pipeline hydrates glyph config by baking attestation data into the server-rendered HTML template (Go reads attestations, injects values into `<input value="...">`). Module-path glyphs don't have this — they start empty.

**Options:**
- **(a)** Add a `GET /config?glyph_id=X` endpoint to each plugin. The module calls `sdk.pluginFetch('/config?glyph_id=...')` on mount. Simple, plugin-specific, but every plugin re-implements the same pattern.
- **(b)** Pass glyph config via the SDK. The frontend reads the glyph's attestation content before calling `render()` and injects it: `sdk.config` or as a third arg. Centralizes the pattern but requires the frontend to know how to read plugin-specific attestation data.
- **(c)** Add `sdk.loadConfig()` to the SDK itself — it calls a standardized endpoint the plugin framework provides, not plugin-specific handlers. Cleanest, but requires a config storage contract in the plugin framework.

### 2. Hand-edited .pb.go

The `domain.pb.go` file was hand-edited to add `ModulePath` field 8. This will be overwritten next time `protoc` is run. The `.proto` file has the correct definition. Run `make proto` (or equivalent) to regenerate from the `.proto` source when ready.

### 3. Plugin module bundling / build pipeline

The ix-json module is plain JS embedded via `go:embed`. This works but has limitations:
- No TypeScript type checking in the plugin module
- No imports — the module must be self-contained
- No tree-shaking or minification

**For later:** Should plugins have a build step (esbuild/vite) that bundles their TS module into a single JS file? Or should the QNTX frontend's vite config discover plugin modules via convention?

### 4. SDK surface expansion

The current SDK exposes a minimal set. Things explicitly *not* included that may be needed:

| Primitive | Why it was left out |
|-----------|-------------------|
| `uiState` subscription | Couples plugins to internal state shape |
| `connectivityManager` | Narrow use case, unclear if plugins need it |
| `tooltip.attach()` | No tooltip system in the SDK yet |
| `subscribe(event, cb)` for WS/SSE | Needs a stable event contract |
| Meld participation | Requires `getMeldOptions` + `getGlyphClass` integration |
| Manifestation switching | Plugin glyphs are canvasPlaced-only today (review doc §3b, vision doc's universal manifestation principle) |

Each of these is a separate decision about what contract plugins get.

### 5. Legacy HTML pipeline deprecation

`content_url` and `module_url` coexist. ix-json now declares both — the frontend prefers `module_url` when present. Should `content_url` eventually be removed? The legacy pipeline has known problems (innerHTML XSS surface, script re-execution, no cleanup, global scope pollution), but removing it is a breaking change for any out-of-tree plugins.

### 6. Shared Go `escapeHTML` (review doc §3d)

Both `qntx-atproto` and `qntx-ix-json` have identical `escapeHTML`/`escapeHTMLAttr` functions. Even with the SDK, the legacy HTML path still exists. Worth extracting to `plugin/html` if any plugin continues to use the HTML pipeline.
