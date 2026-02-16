# Subcanvas Glyph (⌗)

Nested canvas workspace on the parent canvas. Compact form shows a purple glyph with grid preview; double-click morphs to a fullscreen workspace with its own spawn/drag/meld/pan context.

## Two manifestations

- **Compact** (`canvas-subcanvas-glyph`): 180×120 canvas-placed glyph with title bar and grid preview. Default label: `⌗ subcanvas`.
- **Expanded** (`canvas-subcanvas-glyph-expanded`): fullscreen workspace reparented to `document.body`. Element is reparented, not recreated (Element Axiom). Breadcrumb bar shows nesting trail; minimize button or Escape returns to compact.

## Naming

Double-click the title bar label to edit inline. Blur or Enter commits the new name to `uiState` and persists via API. The name carries through to the breadcrumb bar when expanded.

## Canvas ID mapping

The glyph ID doubles as the `canvas_id` for inner glyphs — no mapping table. Inner glyphs are loaded from `uiState.getCanvasGlyphs(subcanvasId)` on expand.

## Nesting

Subcanvases nest arbitrarily. Each expanded level pushes a breadcrumb entry. Clicking an ancestor breadcrumb cascade-minimizes all levels above. Escape only collapses the innermost level (event listener is on the element, not document).

## Melding

Subcanvas is a universal connector in the meld grid:

| Direction | Targets |
|-----------|---------|
| right | All glyph classes |
| bottom | All glyph classes |
| top | All glyph classes |

All other glyphs also list subcanvas as a valid target in their port rules.

When a melded subcanvas expands to fullscreen, a ghost placeholder (`.subcanvas-ghost`) holds its grid cell in the composition. On minimize, the ghost is replaced with the real element and grid positioning is restored.

## Files

| File | Role |
|------|------|
| `web/ts/components/glyph/subcanvas-glyph.ts` | Glyph factory, ghost placeholder, restore logic |
| `web/ts/components/glyph/manifestations/canvas-expanded.ts` | Compact ↔ fullscreen morph path |
| `web/ts/components/glyph/canvas/breadcrumb.ts` | Breadcrumb stack for nested subcanvases |
| `web/ts/components/glyph/meld/meldability.ts` | `canvas-subcanvas-glyph` port rules |
| `web/css/canvas.css` | `.canvas-subcanvas-glyph`, `.subcanvas-preview`, `.subcanvas-ghost` styles |

## Status & roadmap

### Built

| PR | What |
|----|------|
| #484 | Compact ↔ fullscreen morph (Element Axiom) |
| #495 | Per-workspace selection scoping |
| #507 | Naming, breadcrumb navigation |
| #483 | Meld grid participation, ghost placeholder, port rules |

Physical melding works: subcanvas sits in a composition grid, ghost holds its cell when expanded, grid restores on minimize.

### Not built — the usability gap

| Issue | What | Why it matters |
|-------|------|----------------|
| #491 | Drag glyph into subcanvas | Can't populate a subcanvas from the parent canvas |
| #492 | Drag composition into subcanvas | Can't move an entire composition into a subcanvas |

### The melded subcanvas — DAG in a DAG

Today melding is purely spatial. Compositions are edge DAGs used for CSS Grid layout and attachment aggregation (docs/notes collected as prompt context). No dataflow through edges.

A melded subcanvas (`ax → ⌗ → prompt`) is a **sub-DAG node**: the inner workspace IS a DAG. The subcanvas's external meld ports map to its inner root and leaf glyphs. Upstream attestations enter through inner roots; inner leaves produce output that flows downstream. The composition DAG becomes recursive — a DAG whose nodes can themselves be DAGs.

`ax → ⌗ → prompt` is really `ax → [inner-root → ... → inner-leaf] → prompt`. The subcanvas boundary is a scope gate with typed ports, not an opaque box.

### Follow-ups (recommended order)

1. **#491 — Drag glyph into subcanvas.** Single biggest usability blocker.
2. **#492 — Drag composition into subcanvas.** Natural extension — move a melded chain into a subcanvas as a unit.
3. **Sub-DAG wiring.** Map inner root/leaf glyphs to the subcanvas's external meld ports so attestations flow through. Starts as a vision doc update to `fractal-workspace.md`.
4. **Phase 5 hardening.** Composition reconstruction on page load, stale edge cleanup, error propagation through DAG — all compositions, not just subcanvas, but more urgent as compositions nest.
