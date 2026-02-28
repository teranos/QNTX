# Frontend Review: Glyphs, Manifestations, and Plugin Code Sharing

## Scope

Review of the glyph system, manifestation architecture, and opportunities to share code between `web/ts` and plugins providing their own UI.

---

## 1. Architecture Summary

### What exists today

**Three layers:**

| Layer | Where | What |
|-------|-------|------|
| **Glyph primitive** | `web/ts/components/glyph/glyph.ts` | `Glyph` interface — id, title, renderContent, manifestationType, position, size, content |
| **Manifestations** | `web/ts/components/glyph/manifestations/` | window, canvas (fullscreen), panel (slide-in), canvas-placed (inline on workspace), canvas-expanded (placed→fullscreen) |
| **Glyph types** | `web/ts/components/glyph/{ax,py,ix,prompt,note,...}-glyph.ts` + `glyph-registry.ts` | Per-symbol factories that create specific DOM structures |

**Plugin glyphs sit outside this stack.** Plugins return raw HTML from Go handlers (`handleFeedGlyph`, `handleIXGlyph`) that gets mounted via `innerHTML`. The frontend wrapper (`plugin-glyph.ts`) creates a `canvasPlaced` shell and fetches the HTML from the plugin's content endpoint.

### The divide

| | Built-in glyphs | Plugin glyphs |
|---|---|---|
| **Language** | TypeScript | Go (HTML string builders) |
| **Rendering** | Direct DOM construction | `innerHTML` from HTTP response |
| **Interactivity** | Event listeners, state subscriptions, WASM queries | Inline `<script>` tags, global `window.ixSaveConfig` |
| **Styling** | CSS classes + inline styles sharing CSS variables | Plugin-served CSS + inline styles, re-declaring same variables |
| **State** | `uiState` singleton, pub/sub, localStorage + backend sync | Attestation-based per-glyph config, no client state primitives |
| **Title bar** | Shared `glyph-title-bar` class, `addWindowControls()` | Manually reconstructed `<div class="glyph-title-bar">` per plugin |
| **Error handling** | Structured (color states, error glyphs, sync badges) | `container.innerHTML = '<div class="plugin-error">...'` |
| **Script execution** | N/A — native DOM | `container.querySelectorAll('script')` re-creation loop |

---

## 2. Findings

### 2a. Manifestation code duplication

`window.ts` and `panel.ts` share nearly identical patterns:

1. Axiom check → get rect → reparent to body → clear proximity text
2. `beginMaximizeMorph()` with same keyframe structure
3. On commit: restore stashed content OR create title bar + `renderContent()` + error boundary
4. `addWindowControls()` with minimize/close callbacks
5. Minimize: stash content → `beginMinimizeMorph()` → re-attach to tray

The content-rendering block (lines ~130-195 in both files) is copy-pasted. Same error HTML template, same `try/catch` around `renderContent()`, same stash restore logic. `canvas.ts` is simpler (no stash, no title bar) but repeats the same morph→commit→rollback skeleton.

### 2b. Plugin glyphs are HTML islands

Plugin glyphs (`qntx-atproto/handlers.go:484`, `qntx-ix-json/handlers.go:329`) build complete HTML documents with Go string builders. Each plugin:

- Re-implements `escapeHTML` / `escapeHTMLAttr` (identical functions in both plugins)
- Inlines its own CSS (declares its own `var(--background)`, `var(--border-color)` references that must match the host)
- Writes interactive JavaScript as inline strings (no type checking, no imports, no dev tooling)
- Has no access to QNTX's `uiState`, `connectivityManager`, `log`, `apiFetch`, or any frontend primitives
- Cannot participate in the meld system, proximity morphing, or attestation-backed state

The `ix-json` plugin's inline script (~90 lines) re-implements `fetch()` calls to its own backend, status display logic, and DOM traversal — all patterns that already exist in the frontend codebase.

### 2c. The `innerHTML` + script re-execution pattern

`plugin-glyph.ts:142-152` re-creates `<script>` tags to make inline scripts execute after `innerHTML` assignment. This is fragile:

- Scripts execute in global scope (no module isolation)
- Functions must be attached to `window` (e.g., `window.ixSaveConfig`)
- No cleanup on glyph removal — handlers leak
- XSS surface if plugin content is not carefully escaped (each plugin rolls its own escaping)

### 2d. Strengths of the current glyph system

- **Single-element axiom** is well enforced — factory, tracking, verification, hard errors on violation
- **`canvasPlaced()` is already a good shared primitive** — drag, resize, layout, title bar, cleanup registry
- **Glyph registry** enables runtime extension — `registerGlyphType()` is clean
- **Morph transaction model** (begin/commit/rollback with Web Animations API) is solid
- **Proximity system** handles both desktop (mousemove) and mobile (touch browse) correctly

---

## 3. Code Sharing Opportunities

### 3a. Extract shared manifestation skeleton

The morph→content→controls pattern in `window.ts` and `panel.ts` should be a shared function:

```
morphToManifested(element, glyph, {
    target: { x, y, width, height },
    className: 'glyph-panel' | 'glyph-window',
    setupChrome: (element, titleBar) => void,  // panel adds overlay + escape; window adds drag
    verify, onRemove, onMinimize
})
```

The content-rendering block (stash restore OR renderContent + error boundary + addWindowControls) is identical and should be extracted. `canvas.ts` would use a simpler variant without stash/chrome.

### 3b. Plugin SDK: TypeScript-first glyph authoring

The biggest code-sharing opportunity is **letting plugins author their glyphs in TypeScript** that runs in the host frontend, rather than serving HTML from Go.

**Current flow:**
```
Plugin (Go) → HTML string → HTTP → innerHTML → script re-execution
```

**Proposed flow:**
```
Plugin (Go) → GlyphDef { symbol, title, ... } → gRPC
                                                    ↓
Plugin (TS) → render(glyph, sdk) → HTMLElement → registry
```

A plugin would ship a small TypeScript module alongside its Go process. The Go side handles domain logic (API calls, attestations, scheduling). The TS side handles rendering, using a provided SDK:

```typescript
// qntx-ix-json/web/ix-glyph.ts
import type { PluginGlyphSDK } from '@qntx/glyph-sdk';

export function render(glyph: Glyph, sdk: PluginGlyphSDK): HTMLElement {
    const { element, titleBar } = sdk.canvasPlaced({
        glyph,
        className: 'canvas-ix-json-glyph',
        defaults: { x: 200, y: 200, width: 600, height: 700 },
        titleBar: { label: 'JSON API Ingestor' },
        resizable: true,
    });

    const apiUrl = sdk.input({ label: 'API URL', placeholder: 'https://...' });
    sdk.preventDrag(apiUrl);
    element.appendChild(apiUrl);

    const fetchBtn = sdk.button({
        label: 'Test Fetch',
        onClick: async () => {
            const resp = await sdk.pluginFetch('/test-fetch', {
                method: 'POST',
                body: { glyph_id: glyph.id, api_url: apiUrl.value }
            });
            // ...
        }
    });
    element.appendChild(fetchBtn);

    return element;
}
```

**What the SDK would expose:**

| Primitive | Source | Purpose |
|-----------|--------|---------|
| `canvasPlaced()` | `manifestations/canvas-placed.ts` | Positioned, draggable, resizable container |
| `button()` | `components/button.ts` | Stateful button with loading/error/confirm |
| `preventDrag()` | `glyph-interaction.ts` | Protect interactive children from drag |
| `pluginFetch()` | New wrapper around `apiFetch` | `POST /api/{plugin}/{path}` with auth |
| `subscribe(event, cb)` | `uiState` / `connectivityManager` | React to state changes |
| `log` | `logger.ts` | Structured logging with SEG prefix |
| `tooltip.attach()` | `components/tooltip.ts` | Attach tooltip to container |

**Delivery mechanism:** Plugin declares a `glyph_module` path in `GlyphDef`. The frontend dynamically imports it. The Go `ContentPath` field becomes optional (used only for server-rendered fallback or static content).

This eliminates:
- HTML string building in Go
- Duplicated `escapeHTML` functions
- Inline `<script>` execution
- Global `window.*` function pollution
- CSS duplication
- The entire `fetchPluginContent` + script re-creation pipeline

### 3c. Shared CSS token contract

Today, plugin CSS references `var(--background)`, `var(--border-color)`, `var(--foreground)`, etc. — but these are informal. The host defines `var(--bg-primary)`, `var(--text-on-dark)`, `var(--border-color)`.

Document and stabilize the CSS custom property contract that plugins can rely on. A `plugin-theme.css` or a section in `core.css` that explicitly lists the properties available to plugin glyphs. This is needed regardless of whether plugins move to TS rendering.

### 3d. Shared Go utilities for HTML rendering (interim)

Even before a TS SDK, plugins can stop duplicating code:

- `escapeHTML` / `escapeHTMLAttr` — identical in both `qntx-atproto/handlers.go` and `qntx-ix-json/handlers.go`. Extract to a shared `plugin/html` package.
- Common CSS fragments (form inputs, status badges, section headers) could be shared via a Go template library or a shared CSS file served by the core.

### 3e. Plugin glyph lifecycle hooks

Plugin glyphs currently have no cleanup path — when the glyph is removed from the canvas, any intervals, event listeners, or subscriptions set up by inline scripts just leak. The `storeCleanup()` / `runCleanup()` pattern from `glyph-interaction.ts` should be extended to plugin glyphs. The TS SDK approach solves this naturally (the `render` function returns an element; the SDK can track cleanup via the same `storeCleanup` mechanism).

---

## 4. Concrete Next Steps

**Short-term (reduce duplication now):**

1. Extract the shared content-rendering block from `window.ts` and `panel.ts` into a helper (stash restore, renderContent with error boundary, addWindowControls).
2. Extract `escapeHTML`/`escapeHTMLAttr` from both plugins into `plugin/html` or similar shared Go package.
3. Document the CSS custom property contract for plugin glyphs.

**Medium-term (plugin SDK):**

4. Define the `PluginGlyphSDK` interface — the subset of frontend primitives available to plugin TS modules.
5. Add a `glyph_module` field to `GlyphDef` (alongside existing `ContentPath`).
6. Implement dynamic import of plugin glyph modules in the frontend, with SDK injection.
7. Port `ix-json` glyph to TS as the first consumer — it already has issue #626 tracking UI redesign.

**Longer-term (manifestation unification):**

8. Unify the morph skeleton across window/panel/canvas manifestations.
9. Enable plugin glyphs to declare their own manifestation type (currently all plugin glyphs are `canvasPlaced` only — they can't morph to window or panel).
