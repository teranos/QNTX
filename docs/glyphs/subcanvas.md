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
