/**
 * Subcanvas Glyph — compact canvas-placed glyph that morphs to fullscreen workspace
 *
 * When compact: shows a small purple glyph with grid preview on the parent canvas.
 * On dblclick: morphs to fullscreen workspace with full spawn/drag/meld/pan support.
 * Minimize: morphs back to compact position on parent canvas.
 *
 * The glyph ID doubles as the canvas_id for inner glyphs (no mapping table).
 *
 * TODO(#483): Meld support — subcanvas glyphs should be meldable with other glyphs
 * on the parent canvas. A melded subcanvas acts as a spatial grouping container:
 * its inner workspace becomes the shared context for the melded neighbours.
 */

import type { Glyph } from './glyph';
import { log, SEG } from '../../logger';
import { canvasPlaced } from './manifestations/canvas-placed';
import { morphCanvasPlacedToFullscreen } from './manifestations/canvas-expanded';

/**
 * Create a compact subcanvas glyph for the canvas workspace
 */
export function createSubcanvasGlyph(glyph: Glyph): HTMLElement {
    const { element, titleBar } = canvasPlaced({
        glyph,
        className: 'canvas-subcanvas-glyph',
        defaults: { x: 100, y: 100, width: 180, height: 120 },
        titleBar: { label: '⌗ subcanvas' },
        resizable: { minWidth: 120, minHeight: 80 },
        logLabel: 'Subcanvas',
    });

    // Grid preview content area
    const preview = document.createElement('div');
    preview.className = 'subcanvas-preview';
    element.appendChild(preview);

    // dblclick → morph to fullscreen workspace
    element.addEventListener('dblclick', (e) => {
        e.stopPropagation();
        log.debug(SEG.GLYPH, `[Subcanvas] dblclick on ${glyph.id}, morphing to fullscreen`);

        // Find parent canvas ID from the content layer this glyph lives in
        const contentLayer = element.closest('.canvas-content-layer');
        const parentCanvas = contentLayer?.closest('.canvas-workspace');
        const canvasId = parentCanvas?.dataset?.canvasId ?? 'canvas-workspace';

        morphCanvasPlacedToFullscreen(
            element,
            glyph,
            canvasId,
            (el, g) => restoreToCanvas(el, g, contentLayer as HTMLElement)
        );
    });

    return element;
}

/**
 * Restore subcanvas glyph back to canvas-placed position after fullscreen minimize
 *
 * Uses canvasPlaced() with the `element` option to re-attach drag/resize handlers
 * on the same DOM element (Element Axiom: no recreation).
 */
function restoreToCanvas(
    element: HTMLElement,
    glyph: Glyph,
    contentLayer: HTMLElement | null
): void {
    if (!contentLayer) {
        log.error(SEG.GLYPH, `[Subcanvas] Cannot restore ${glyph.id} — no content layer`);
        return;
    }

    // Clear fullscreen content, rebuild compact appearance via canvasPlaced()
    element.innerHTML = '';

    canvasPlaced({
        glyph,
        className: 'canvas-subcanvas-glyph',
        defaults: { x: 100, y: 100, width: 180, height: 120 },
        titleBar: { label: '⌗ subcanvas' },
        resizable: { minWidth: 120, minHeight: 80 },
        logLabel: 'Subcanvas',
        element, // Reuse existing element — restores class, layout, drag, resize
    });

    // Grid preview content area
    const preview = document.createElement('div');
    preview.className = 'subcanvas-preview';
    element.appendChild(preview);

    // Reparent to canvas content layer
    contentLayer.appendChild(element);

    // Re-attach dblclick for next expand
    element.addEventListener('dblclick', (e) => {
        e.stopPropagation();
        const cl = element.closest('.canvas-content-layer');
        const parentCanvas = cl?.closest('.canvas-workspace');
        const canvasId = parentCanvas?.dataset?.canvasId ?? 'canvas-workspace';

        morphCanvasPlacedToFullscreen(
            element,
            glyph,
            canvasId,
            (el, g) => restoreToCanvas(el, g, cl as HTMLElement)
        );
    });

    log.debug(SEG.GLYPH, `[Subcanvas] Restored ${glyph.id} to canvas at (${glyph.x}, ${glyph.y})`);
}
