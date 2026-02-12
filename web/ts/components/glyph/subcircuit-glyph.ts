/**
 * Subcircuit Glyph — compact canvas-placed glyph that expands to a
 * bordered fullscreen workspace on double-click.
 *
 * Compact state: small square canvasPlaced glyph (120x120) with purple title bar.
 * Expanded state: viewport-inset bordered workspace (fixed, z-index 1000).
 *
 * The element never leaves the canvas DOM — expand/collapse just toggles
 * between position:absolute (canvas-relative) and position:fixed (viewport).
 * Fixed positioning escapes parent overflow:hidden without reparenting.
 */

import type { Glyph } from './glyph';
import { getMinimizeDuration } from './glyph';
import { log, SEG } from '../../logger';
import { canvasPlaced } from './manifestations/canvas-placed';

const COMPACT_SIZE = 120;
const EXPAND_INSET = 40;

export async function createSubcircuitGlyph(glyph: Glyph): Promise<HTMLElement> {
    const { element } = canvasPlaced({
        glyph,
        className: 'canvas-subcircuit-glyph',
        defaults: {
            x: glyph.x ?? 200,
            y: glyph.y ?? 200,
            width: glyph.width ?? COMPACT_SIZE,
            height: glyph.height ?? COMPACT_SIZE,
        },
        titleBar: { label: '⌗ subcircuit' },
        resizable: true,
        logLabel: 'Subcircuit',
    });

    // Purple title bar styling
    const titleBar = element.querySelector('.canvas-glyph-title-bar') as HTMLElement;
    if (titleBar) {
        titleBar.style.backgroundColor = '#3a2d5a';
        const labelSpan = titleBar.querySelector('span:first-child') as HTMLElement;
        if (labelSpan) {
            labelSpan.style.color = '#c0a0e8';
            labelSpan.style.fontWeight = 'bold';
        }
    }

    // Content area
    const content = document.createElement('div');
    content.className = 'subcircuit-content';
    content.style.flex = '1';
    content.style.position = 'relative';
    content.style.overflow = 'hidden';
    content.style.backgroundColor = 'var(--bg-dark-hover)';
    element.appendChild(content);

    // Subtle inner grid overlay
    const gridOverlay = document.createElement('div');
    gridOverlay.className = 'canvas-grid-overlay';
    gridOverlay.style.position = 'absolute';
    gridOverlay.style.inset = '0';
    gridOverlay.style.pointerEvents = 'none';
    gridOverlay.style.opacity = '0.3';
    content.appendChild(gridOverlay);

    // Double-click to expand
    element.addEventListener('dblclick', (e) => {
        e.stopPropagation();
        if (element.dataset.subcircuitExpanded === 'true') return;
        expandSubcircuit(element);
    });

    log.debug(SEG.GLYPH, `[Subcircuit] Created compact subcircuit ${glyph.id}`);

    return element;
}

function expandSubcircuit(element: HTMLElement): void {
    // Store compact geometry for collapse
    const rect = element.getBoundingClientRect();
    element.dataset.compactLeft = element.style.left;
    element.dataset.compactTop = element.style.top;
    element.dataset.compactWidth = element.style.width;
    element.dataset.compactHeight = element.style.height || element.style.minHeight;
    element.dataset.compactZIndex = element.style.zIndex;

    // Target rect: viewport with inset
    const targetLeft = EXPAND_INSET;
    const targetTop = EXPAND_INSET;
    const targetWidth = window.innerWidth - EXPAND_INSET * 2;
    const targetHeight = window.innerHeight - EXPAND_INSET * 2;

    // Animate from compact to expanded
    const duration = getMinimizeDuration() * 1.5;
    if (duration > 0) {
        element.animate([
            {
                position: 'fixed',
                left: `${rect.left}px`,
                top: `${rect.top}px`,
                width: `${rect.width}px`,
                height: `${rect.height}px`,
            },
            {
                position: 'fixed',
                left: `${targetLeft}px`,
                top: `${targetTop}px`,
                width: `${targetWidth}px`,
                height: `${targetHeight}px`,
            },
        ], {
            duration,
            easing: 'ease-out',
            fill: 'forwards',
        });
    }

    // Apply expanded styles — stays in canvas DOM, fixed escapes overflow:hidden
    element.style.position = 'fixed';
    element.style.left = `${targetLeft}px`;
    element.style.top = `${targetTop}px`;
    element.style.width = `${targetWidth}px`;
    element.style.height = `${targetHeight}px`;
    element.style.minHeight = '';
    element.style.zIndex = '1000';
    element.style.border = '2px solid var(--border-bright, #c0a0e8)';

    element.dataset.subcircuitExpanded = 'true';

    // Add minimize button
    const minimizeBtn = document.createElement('button');
    minimizeBtn.className = 'subcircuit-minimize-btn';
    minimizeBtn.textContent = '−';
    minimizeBtn.title = 'Collapse subcircuit';
    minimizeBtn.style.position = 'absolute';
    minimizeBtn.style.top = '4px';
    minimizeBtn.style.right = '8px';
    minimizeBtn.style.zIndex = '1';
    minimizeBtn.style.background = 'none';
    minimizeBtn.style.border = 'none';
    minimizeBtn.style.color = '#c0a0e8';
    minimizeBtn.style.fontSize = '18px';
    minimizeBtn.style.cursor = 'pointer';
    minimizeBtn.style.lineHeight = '1';
    minimizeBtn.style.padding = '2px 6px';

    minimizeBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        collapseSubcircuit(element, minimizeBtn);
    });

    element.appendChild(minimizeBtn);

    log.debug(SEG.GLYPH, `[Subcircuit] Expanded ${element.dataset.glyphId}`);
}

function collapseSubcircuit(element: HTMLElement, minimizeBtn: HTMLElement): void {
    const expandedRect = element.getBoundingClientRect();

    // Retrieve stored compact geometry
    const compactLeft = element.dataset.compactLeft || '200px';
    const compactTop = element.dataset.compactTop || '200px';
    const compactWidth = element.dataset.compactWidth || `${COMPACT_SIZE}px`;
    const compactHeight = element.dataset.compactHeight || `${COMPACT_SIZE}px`;
    const compactZIndex = element.dataset.compactZIndex || '';

    // Animate from expanded to compact (still fixed during animation)
    const duration = getMinimizeDuration() * 1.5;

    // Calculate where the compact position will be in viewport coords for animation
    const canvas = element.closest('.canvas-workspace') as HTMLElement;
    const canvasRect = canvas?.getBoundingClientRect();
    const viewportLeft = (canvasRect?.left ?? 0) + parseFloat(compactLeft);
    const viewportTop = (canvasRect?.top ?? 0) + parseFloat(compactTop);

    if (duration > 0) {
        const animation = element.animate([
            {
                left: `${expandedRect.left}px`,
                top: `${expandedRect.top}px`,
                width: `${expandedRect.width}px`,
                height: `${expandedRect.height}px`,
            },
            {
                left: `${viewportLeft}px`,
                top: `${viewportTop}px`,
                width: `${parseFloat(compactWidth)}px`,
                height: `${parseFloat(compactHeight)}px`,
            },
        ], {
            duration,
            easing: 'ease-in',
            fill: 'forwards',
        });

        animation.onfinish = () => finishCollapse();
    } else {
        finishCollapse();
    }

    function finishCollapse() {
        // Restore compact styles — back to absolute within canvas
        element.style.position = 'absolute';
        element.style.left = compactLeft;
        element.style.top = compactTop;
        element.style.width = compactWidth;
        element.style.height = compactHeight;
        element.style.zIndex = compactZIndex;
        element.style.border = '';

        // Clean up
        minimizeBtn.remove();
        delete element.dataset.subcircuitExpanded;
        delete element.dataset.compactLeft;
        delete element.dataset.compactTop;
        delete element.dataset.compactWidth;
        delete element.dataset.compactHeight;
        delete element.dataset.compactZIndex;

        log.debug(SEG.GLYPH, `[Subcircuit] Collapsed ${element.dataset.glyphId}`);
    }
}
