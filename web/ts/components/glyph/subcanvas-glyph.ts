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
import { uiState } from '../../state/ui';

/**
 * Create a compact subcanvas glyph for the canvas workspace
 */
export function createSubcanvasGlyph(glyph: Glyph): HTMLElement {
    const label = glyph.content || '⌗ subcanvas';

    const { element, titleBar } = canvasPlaced({
        glyph,
        className: 'canvas-subcanvas-glyph',
        defaults: { x: 100, y: 100, width: 180, height: 120 },
        titleBar: { label },
        resizable: { minWidth: 120, minHeight: 80 },
        logLabel: 'Subcanvas',
    });

    // Wire up inline editing on the title bar label span
    if (titleBar) {
        wireEditableLabel(titleBar, glyph);
    }

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
        const parentCanvas = contentLayer?.closest('.canvas-workspace') as HTMLElement | null;
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
 * Wire inline editing on the title bar label <span>.
 * dblclick → contentEditable, blur → persist, Enter → commit, Escape → revert.
 */
function wireEditableLabel(titleBar: HTMLElement, glyph: Glyph): void {
    const labelSpan = titleBar.querySelector('span');
    if (!labelSpan) return;

    labelSpan.addEventListener('dblclick', (e) => {
        e.stopPropagation(); // prevent expand-to-fullscreen
        labelSpan.contentEditable = 'true';
        labelSpan.focus();

        // Select all text
        const range = document.createRange();
        range.selectNodeContents(labelSpan);
        const sel = window.getSelection();
        sel?.removeAllRanges();
        sel?.addRange(range);
    });

    // Prevent drag initiation while editing
    labelSpan.addEventListener('mousedown', (e) => {
        if (labelSpan.contentEditable === 'true') {
            e.stopPropagation();
        }
    });

    const previousValue = () => glyph.content || '⌗ subcanvas';

    labelSpan.addEventListener('blur', () => {
        if (labelSpan.contentEditable !== 'true') return;
        const newName = labelSpan.innerText.trim();
        labelSpan.contentEditable = 'false';

        if (newName && newName !== previousValue()) {
            glyph.content = newName;
            // Persist to uiState + API
            uiState.addCanvasGlyph({
                id: glyph.id,
                symbol: glyph.symbol ?? '⌗',
                x: glyph.x ?? 100,
                y: glyph.y ?? 100,
                content: newName,
            });
        } else {
            // Revert to previous value
            labelSpan.textContent = previousValue();
        }
    });

    labelSpan.addEventListener('keydown', (e) => {
        if (labelSpan.contentEditable !== 'true') return;

        if (e.key === 'Enter') {
            e.preventDefault();
            labelSpan.blur();
        } else if (e.key === 'Escape') {
            e.preventDefault();
            labelSpan.textContent = previousValue();
            labelSpan.contentEditable = 'false';
        }
    });
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

    // Read latest name from persisted state
    const saved = uiState.getCanvasGlyphs().find(g => g.id === glyph.id);
    const name = saved?.content || glyph.content || '⌗ subcanvas';
    glyph.content = saved?.content || glyph.content;

    // Clear fullscreen content, rebuild compact appearance via canvasPlaced()
    element.innerHTML = '';

    const { titleBar } = canvasPlaced({
        glyph,
        className: 'canvas-subcanvas-glyph',
        defaults: { x: 100, y: 100, width: 180, height: 120 },
        titleBar: { label: name },
        resizable: { minWidth: 120, minHeight: 80 },
        logLabel: 'Subcanvas',
        element, // Reuse existing element — restores class, layout, drag, resize
    });

    // Re-wire inline editing on the restored title bar
    if (titleBar) {
        wireEditableLabel(titleBar, glyph);
    }

    // Grid preview content area
    const preview = document.createElement('div');
    preview.className = 'subcanvas-preview';
    element.appendChild(preview);

    // Reparent to canvas content layer — dblclick handler from createSubcanvasGlyph
    // persists on the element (it uses closest() at dispatch time, no re-registration needed)
    contentLayer.appendChild(element);

    log.debug(SEG.GLYPH, `[Subcanvas] Restored ${glyph.id} to canvas at (${glyph.x}, ${glyph.y})`);
}
