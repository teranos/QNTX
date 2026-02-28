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

### ~~2b. Plugin glyphs are HTML islands~~ — RESOLVED (ix-json)

ix-json's legacy HTML pipeline (~350 lines of Go string builder HTML/CSS/JS, ~90 lines of inline `<script>`, global `window.*` functions) has been deleted. The plugin now ships only a JS module (`ix-glyph-module.js`) that uses the Plugin Glyph SDK — `sdk.container()`, `sdk.input()`, `sdk.button()`, `sdk.pluginFetch()`, `sdk.loadConfig()`, `sdk.statusLine()`.

`qntx-atproto` still uses the legacy HTML pipeline and remains an HTML island.

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

### ~~3b. @qntx/glyphs: TypeScript-first glyph authoring~~ — SHIPPED

Plugins author glyphs in TypeScript using the `GlyphUI` interface (from `@qntx/glyphs`). The Go side handles domain logic; the TS side handles rendering. The host injects `GlyphUI` at render time — plugins import types only.

```typescript
// qntx-ix-json/web/ix-glyph-module.ts
import type { Glyph, GlyphUI, RenderFn } from '@qntx/glyphs';

export const render: RenderFn = async (glyph, ui) => {
    const { element } = ui.container({
        defaults: { x: 200, y: 200, width: 600, height: 700 },
        titleBar: { label: 'JSON API Ingestor' },
        resizable: true,
    });

    const apiUrl = ui.input({ label: 'API URL', placeholder: 'https://...' });
    element.appendChild(apiUrl);
    return element;
};
```

**What `GlyphUI` exposes:**

| Primitive | Source | Purpose |
|-----------|--------|---------|
| `container()` | `manifestations/canvas-placed.ts` | Positioned, draggable, resizable container |
| `button()` | `glyph-ui.ts` | Button with click handler |
| `input()` | `glyph-ui.ts` | Text input with drag protection |
| `preventDrag()` | `glyph-interaction.ts` | Protect interactive children from drag |
| `pluginFetch()` | wrapper around `apiFetch` | `POST /api/{plugin}/{path}` with auth |
| `loadConfig()` / `saveConfig()` | server-owned `/api/glyph-config` | Attestation-based config persistence |
| `statusLine()` | `glyph-ui.ts` | Feedback status display |
| `log` | `logger.ts` | Structured logging with SEG prefix |
| `onCleanup()` | `glyph-interaction.ts` | Cleanup registration for glyph removal |

**Build pipeline:** Plugin TS → `bun build` → JS → `go:embed`. Type-only imports erased during compilation. ix-json is the first consumer (legacy HTML pipeline deleted).

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

**Medium-term (@qntx/glyphs):** — DONE

4. ~~Define the `GlyphUI` interface — the subset of frontend primitives available to plugin TS modules.~~
5. ~~Add a `module_url` field to `GlyphDef` (alongside existing `ContentPath`).~~
6. ~~Implement dynamic import of plugin glyph modules in the frontend, with `GlyphUI` injection.~~
7. ~~Port `ix-json` glyph to TS as the first consumer (legacy HTML pipeline deleted).~~

**Longer-term (manifestation unification):**

8. Unify the morph skeleton across window/panel/canvas manifestations.
9. Enable plugin glyphs to declare their own manifestation type (currently all plugin glyphs are `canvasPlaced` only — they can't morph to window or panel).
