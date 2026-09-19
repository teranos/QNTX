/**
 * Subcanvas Element — compact canvas-placed element that morphs to fullscreen workspace
 *
 * When compact: shows a small purple element with grid preview on the parent canvas.
 * On dblclick: morphs to fullscreen workspace with full spawn/drag/meld/pan support.
 * Minimize: morphs back to compact position on parent canvas.
 *
 * The element ID doubles as the canvas_id for inner elements (no mapping table).
 *
 * TODO(#483): Meld support — subcanvas elements should be meldable with other elements
 * on the parent canvas. A melded subcanvas acts as a spatial grouping container:
 * its inner workspace becomes the shared context for the melded neighbours.
 */

import type { Element } from '@teranos/elements';
import { log, SEG } from '../../logger';
import { canvasPlaced } from '@teranos/elements';
import { morphCanvasPlacedToCanvasExpanded } from './manifestations/canvas-expanded';
import { uiState } from '../../state/ui';

/**
 * Create a compact subcanvas element for the canvas workspace
 */
export function createSubcanvasElement(item: Element): HTMLElement {
    // element.symbol renders as its own span in the title bar — the label is name only
    const label = item.content || 'subcanvas';

    const { element, titleBar } = canvasPlaced({
        item: item,
        className: 'canvas-subcanvas-element',
        defaults: { x: 100, y: 100, width: 180, height: 120 },
        titleBar: { label },
        resizable: { minWidth: 120, minHeight: 80 },
        logLabel: 'Subcanvas',
    });

    // Wire up inline editing on the title bar label span
    if (titleBar) {
        wireEditableLabel(titleBar, item);
    }

    // Grid preview content area
    const preview = document.createElement('div');
    preview.className = 'subcanvas-preview';
    element.appendChild(preview);

    // dblclick → morph to fullscreen workspace
    element.addEventListener('dblclick', (e) => {
        e.stopPropagation();
        log.debug(SEG.ELEMENT, `[Subcanvas] dblclick on ${item.id}, morphing to fullscreen`);

        // Find parent canvas ID from the content layer this element lives in
        const contentLayer = element.closest('.canvas-content-layer');
        const parentCanvas = contentLayer?.closest('.canvas-workspace') as HTMLElement | null;
        const canvasId = parentCanvas?.dataset?.canvasId ?? 'canvas-workspace';

        // If melded, insert ghost placeholder to hold the grid cell
        const composition = element.closest('.melded-composition') as HTMLElement | null;
        let ghost: HTMLElement | null = null;
        if (composition) {
            ghost = document.createElement('div');
            ghost.className = 'subcanvas-ghost';
            ghost.style.gridRow = element.style.gridRow;
            ghost.style.gridColumn = element.style.gridColumn;
            ghost.style.width = element.offsetWidth + 'px';
            ghost.style.height = element.offsetHeight + 'px';
            element.parentNode!.insertBefore(ghost, element);
            log.debug(SEG.ELEMENT, `[Subcanvas] Ghost inserted for ${item.id} at grid(${element.style.gridRow}, ${element.style.gridColumn})`);
        }

        morphCanvasPlacedToCanvasExpanded(
            element,
            item,
            canvasId,
            (el, g) => restoreToCanvas(el, g, contentLayer as HTMLElement, ghost, composition)
        );
    });

    return element;
}

/**
 * Wire inline editing on the title bar label <span>.
 * dblclick → contentEditable, blur → persist, Enter → commit.
 */
function wireEditableLabel(titleBar: HTMLElement, item: Element): void {
    const labelSpan = titleBar.querySelector<HTMLElement>('span:not(.symbol)');
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

    const previousValue = () => item.content || 'subcanvas';

    labelSpan.addEventListener('blur', () => {
        if (labelSpan.contentEditable !== 'true') return;
        const newName = labelSpan.innerText.trim();
        labelSpan.contentEditable = 'false';

        if (newName && newName !== previousValue()) {
            item.content = newName;
            // Persist to uiState + API
            uiState.addCanvasElement({
                id: item.id,
                symbol: item.symbol ?? '⌗',
                x: item.x ?? 100,
                y: item.y ?? 100,
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
        }
    });
}

/**
 * Restore subcanvas element back to canvas-placed position after fullscreen minimize
 *
 * Uses canvasPlaced() with the `element` option to re-attach drag/resize handlers
 * on the same DOM element (Element Axiom: no recreation).
 */
function restoreToCanvas(
    element: HTMLElement,
    item: Element,
    contentLayer: HTMLElement | null,
    ghost: HTMLElement | null,
    composition: HTMLElement | null
): void {
    if (!contentLayer) {
        log.error(SEG.ELEMENT, `[Subcanvas] Cannot restore ${item.id} — no content layer`);
        return;
    }

    // Read latest name from persisted state
    const saved = uiState.getCanvasElement(item.id);
    const name = saved?.content || item.content || '⌗ subcanvas';
    item.content = saved?.content || item.content;

    // Clear fullscreen content, rebuild compact appearance via canvasPlaced()
    element.innerHTML = '';

    const { titleBar } = canvasPlaced({
        item: item,
        className: 'canvas-subcanvas-element',
        defaults: { x: 100, y: 100, width: 180, height: 120 },
        titleBar: { label: name },
        resizable: { minWidth: 120, minHeight: 80 },
        logLabel: 'Subcanvas',
        element, // Reuse existing element — restores class, layout, drag, resize
    });

    // Re-wire inline editing on the restored title bar
    if (titleBar) {
        wireEditableLabel(titleBar, item);
    }

    // Grid preview content area
    const preview = document.createElement('div');
    preview.className = 'subcanvas-preview';
    element.appendChild(preview);

    // Replace ghost with real element in the composition, or reparent to content layer
    if (ghost && composition && ghost.parentNode) {
        // Restore grid positioning — canvasPlaced sets absolute left/top which fights CSS grid
        element.style.position = 'relative';
        element.style.left = '';
        element.style.top = '';
        element.style.gridRow = ghost.style.gridRow;
        element.style.gridColumn = ghost.style.gridColumn;
        ghost.parentNode.replaceChild(element, ghost);
        log.debug(SEG.ELEMENT, `[Subcanvas] Replaced ghost for ${item.id}, back in composition at grid(${element.style.gridRow}, ${element.style.gridColumn})`);
    } else {
        contentLayer.appendChild(element);
    }

    log.debug(SEG.ELEMENT, `[Subcanvas] Restored ${item.id} to canvas at (${item.x}, ${item.y})`);
}
