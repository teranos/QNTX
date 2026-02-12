/**
 * Subcircuit Glyph — compact square glyph with centered ⌗ symbol.
 *
 * Compact: small square on canvas, large centered symbol, no title bar.
 * Expanded: dblclick → viewport-inset bordered workspace. dblclick again → collapse.
 *
 * Element stays in canvas DOM — expand/collapse toggles position:absolute ↔ fixed.
 * Fixed positioning escapes parent overflow:hidden without reparenting.
 *
 * Drag is disabled when expanded by setting pointerEvents:none on the drag
 * handle (content div). The dblclick listener lives on the outer element
 * so it still fires for collapse.
 */

import type { Glyph } from './glyph';
import { getMinimizeDuration } from './glyph';
import { log, SEG } from '../../logger';
import { canvasPlaced } from './manifestations/canvas-placed';

const COMPACT_SIZE = 120;
const EXPAND_INSET = 40;

export async function createSubcircuitGlyph(glyph: Glyph): Promise<HTMLElement> {
    // Content area — serves as drag handle in compact mode
    const content = document.createElement('div');
    content.className = 'subcircuit-content';
    content.style.flex = '1';
    content.style.position = 'relative';
    content.style.overflow = 'hidden';
    content.style.backgroundColor = 'var(--bg-dark-hover)';
    content.style.cursor = 'grab';
    content.style.display = 'grid';
    content.style.placeItems = 'center';

    const { element } = canvasPlaced({
        glyph,
        className: 'canvas-subcircuit-glyph',
        defaults: {
            x: glyph.x ?? 200,
            y: glyph.y ?? 200,
            width: glyph.width ?? COMPACT_SIZE,
            height: glyph.height ?? COMPACT_SIZE,
        },
        dragHandle: content,
        resizable: true,
        resizeHandleClass: 'glyph-resize-handle--hidden',
        logLabel: 'Subcircuit',
    });

    // Large centered symbol
    const symbol = document.createElement('span');
    symbol.className = 'subcircuit-symbol';
    symbol.textContent = '⌗';
    symbol.style.fontSize = '48px';
    symbol.style.color = '#c0a0e8';
    symbol.style.pointerEvents = 'none';
    symbol.style.userSelect = 'none';
    symbol.style.lineHeight = '1';
    content.appendChild(symbol);

    // Subtle inner grid overlay
    const gridOverlay = document.createElement('div');
    gridOverlay.className = 'canvas-grid-overlay';
    gridOverlay.style.position = 'absolute';
    gridOverlay.style.inset = '0';
    gridOverlay.style.pointerEvents = 'none';
    gridOverlay.style.opacity = '0.3';
    content.appendChild(gridOverlay);

    element.appendChild(content);

    // Double-click on the outer element toggles expand/collapse.
    // This works even when content has pointerEvents:none because
    // the click lands on the element itself.
    element.addEventListener('dblclick', (e) => {
        e.stopPropagation();
        if (element.dataset.subcircuitExpanded === 'true') {
            collapseSubcircuit(element, content);
        } else {
            expandSubcircuit(element, content);
        }
    });

    log.debug(SEG.GLYPH, `[Subcircuit] Created compact subcircuit ${glyph.id}`);

    return element;
}

function expandSubcircuit(element: HTMLElement, content: HTMLElement): void {
    // Store compact geometry for collapse
    const rect = element.getBoundingClientRect();
    element.dataset.compactLeft = element.style.left;
    element.dataset.compactTop = element.style.top;
    element.dataset.compactWidth = element.style.width;
    element.dataset.compactHeight = element.style.height || element.style.minHeight;
    element.dataset.compactZIndex = element.style.zIndex;

    // Disable drag — no drag handle when expanded
    content.style.pointerEvents = 'none';
    content.style.cursor = 'default';

    // Target rect: viewport with inset
    const targetLeft = EXPAND_INSET;
    const targetTop = EXPAND_INSET;
    const targetWidth = window.innerWidth - EXPAND_INSET * 2;
    const targetHeight = window.innerHeight - EXPAND_INSET * 2;

    // Apply expanded styles first so animation plays on top of final state
    element.style.position = 'fixed';
    element.style.left = `${targetLeft}px`;
    element.style.top = `${targetTop}px`;
    element.style.width = `${targetWidth}px`;
    element.style.height = `${targetHeight}px`;
    element.style.minHeight = '';
    element.style.zIndex = '1000';
    element.style.border = '2px solid var(--border-bright, #c0a0e8)';

    // Animate from compact to expanded (no fill — inline styles are source of truth)
    const duration = getMinimizeDuration() * 1.5;
    if (duration > 0) {
        element.animate([
            {
                left: `${rect.left}px`,
                top: `${rect.top}px`,
                width: `${rect.width}px`,
                height: `${rect.height}px`,
            },
            {
                left: `${targetLeft}px`,
                top: `${targetTop}px`,
                width: `${targetWidth}px`,
                height: `${targetHeight}px`,
            },
        ], {
            duration,
            easing: 'ease-out',
        });
    }

    element.dataset.subcircuitExpanded = 'true';

    log.debug(SEG.GLYPH, `[Subcircuit] Expanded ${element.dataset.glyphId}`);
}

function collapseSubcircuit(element: HTMLElement, content: HTMLElement): void {
    const expandedRect = element.getBoundingClientRect();

    // Retrieve stored compact geometry
    const compactLeft = element.dataset.compactLeft || '200px';
    const compactTop = element.dataset.compactTop || '200px';
    const compactWidth = element.dataset.compactWidth || `${COMPACT_SIZE}px`;
    const compactHeight = element.dataset.compactHeight || `${COMPACT_SIZE}px`;
    const compactZIndex = element.dataset.compactZIndex || '';

    const duration = getMinimizeDuration() * 1.5;

    // Calculate where the compact position will be in viewport coords for animation
    const canvas = element.closest('.canvas-workspace') as HTMLElement;
    const canvasRect = canvas?.getBoundingClientRect();
    const viewportLeft = (canvasRect?.left ?? 0) + parseFloat(compactLeft);
    const viewportTop = (canvasRect?.top ?? 0) + parseFloat(compactTop);

    // Restore compact styles immediately — animation plays on top
    element.style.position = 'absolute';
    element.style.left = compactLeft;
    element.style.top = compactTop;
    element.style.width = compactWidth;
    element.style.height = compactHeight;
    element.style.zIndex = compactZIndex;
    element.style.border = '';

    // Re-enable drag
    content.style.pointerEvents = '';
    content.style.cursor = 'grab';

    // Animate from expanded to compact (no fill — inline styles are source of truth)
    if (duration > 0) {
        element.animate([
            {
                position: 'fixed',
                left: `${expandedRect.left}px`,
                top: `${expandedRect.top}px`,
                width: `${expandedRect.width}px`,
                height: `${expandedRect.height}px`,
            },
            {
                position: 'absolute',
                left: compactLeft,
                top: compactTop,
                width: compactWidth,
                height: compactHeight,
            },
        ], {
            duration,
            easing: 'ease-in',
        });
    }

    // Clean up
    delete element.dataset.subcircuitExpanded;
    delete element.dataset.compactLeft;
    delete element.dataset.compactTop;
    delete element.dataset.compactWidth;
    delete element.dataset.compactHeight;
    delete element.dataset.compactZIndex;

    log.debug(SEG.GLYPH, `[Subcircuit] Collapsed ${element.dataset.glyphId}`);
}
