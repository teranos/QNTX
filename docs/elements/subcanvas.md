# Subcanvas Element (⌗)

Nested canvas workspace on the parent canvas. Compact form shows a purple element with grid preview; double-click morphs to a fullscreen workspace with its own spawn/drag/meld/pan context.

## Two manifestations

- **Compact** (`canvas-subcanvas-element`): 180×120 canvas-placed element with title bar and grid preview. Default label: `⌗ subcanvas`.
- **Expanded** (`canvas-subcanvas-element-expanded`): fullscreen workspace reparented to `document.body`. Element is reparented, not recreated (Element Axiom). Breadcrumb bar shows nesting trail; minimize button or Escape returns to compact.

## Naming

Double-click the title bar label to edit inline. Blur or Enter commits the new name to `uiState` and persists via API. The name carries through to the breadcrumb bar when expanded.

## Canvas ID mapping

The element ID doubles as the `canvas_id` for inner elements — no mapping table. Inner elements are loaded from `uiState.getCanvasElements(subcanvasId)` on expand.

## Nesting

Subcanvases nest arbitrarily. Each expanded level pushes a breadcrumb entry. Clicking an ancestor breadcrumb cascade-minimizes all levels above. Escape only collapses the innermost level (event listener is on the element, not document).

## Melding

Subcanvas is a universal connector in the meld grid:

| Direction | Targets |
|-----------|---------|
| right | All element classes |
| bottom | All element classes |
| top | All element classes |

All other elements also list subcanvas as a valid target in their port rules.

When a melded subcanvas expands to fullscreen, a ghost placeholder (`.subcanvas-ghost`) holds its grid cell in the composition. On minimize, the ghost is replaced with the real element and grid positioning is restored.

## Files

| File | Role |
|------|------|
| `web/ts/components/element/subcanvas-element.ts` | Element factory, ghost placeholder, restore logic |
| `web/ts/components/element/manifestations/canvas-expanded.ts` | Compact ↔ fullscreen morph path |
| `web/ts/components/element/canvas/breadcrumb.ts` | Breadcrumb stack for nested subcanvases |
| `web/css/canvas.css` | `.canvas-subcanvas-element`, `.subcanvas-preview`, `.subcanvas-ghost` styles |

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
| #491 | Drag element into subcanvas | Can't populate a subcanvas from the parent canvas |
| #492 | Drag composition into subcanvas | Can't move an entire composition into a subcanvas |

### The melded subcanvas — DAG in a DAG

Today melding is purely spatial. Compositions are edge DAGs used for CSS Grid layout and attachment aggregation (docs/notes collected as prompt context). No dataflow through edges.

A melded subcanvas (`ax → ⌗ → prompt`) is a **sub-DAG node**: the inner workspace IS a DAG. The subcanvas's external meld ports map to its inner root and leaf elements. Upstream attestations enter through inner roots; inner leaves produce output that flows downstream. The composition DAG becomes recursive — a DAG whose nodes can themselves be DAGs.

`ax → ⌗ → prompt` is really `ax → [inner-root → ... → inner-leaf] → prompt`. The subcanvas boundary is a scope gate with typed ports, not an opaque box.

### Follow-ups (recommended order)

1. **#491 — Drag element into subcanvas.** Single biggest usability blocker.
2. **#492 — Drag composition into subcanvas.** Natural extension — move a melded chain into a subcanvas as a unit.
3. **Sub-DAG wiring.** Map inner root/leaf elements to the subcanvas's external meld ports so attestations flow through. Starts as a vision doc update to `fractal-workspace.md`.
4. **Phase 5 hardening.** Composition reconstruction on page load, stale edge cleanup, error propagation through DAG — all compositions, not just subcanvas, but more urgent as compositions nest.
