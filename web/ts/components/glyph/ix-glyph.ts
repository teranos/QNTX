/**
 * IX Glyph - Ingest form directly on canvas
 *
 * Shows textarea with ix argument (URL, file path) and execute button.
 * Editable directly on canvas - no hidden windows, no extra clicks.
 *
 * Future enhancements:
 * - Show preview of attestations that would be created
 * - Display type of ix operation (URL, file, API)
 * - Wire execute button to backend /api/ix/execute
 * - Show status (idle, running, complete, error)
 * - Display count of created attestations
 */

import type { Glyph } from './glyph';
import { IX } from '@generated/sym.js';
import { log, SEG } from '../../logger';
import { uiState } from '../../state/ui';
import { GRID_SIZE } from './grid-constants';

/**
 * Create an IX glyph with input form on canvas
 */
export async function createIxGlyph(glyph: Glyph): Promise<HTMLElement> {
    const element = document.createElement('div');
    element.className = 'canvas-ix-glyph';
    element.dataset.glyphId = glyph.id;

    const gridX = glyph.gridX ?? 5;
    const gridY = glyph.gridY ?? 5;

    // Default size for IX glyph
    const width = glyph.width ?? 360;
    const height = glyph.height ?? 180;

    // Style element
    element.style.position = 'absolute';
    element.style.left = `${gridX * GRID_SIZE}px`;
    element.style.top = `${gridY * GRID_SIZE}px`;
    element.style.width = `${width}px`;
    element.style.height = `${height}px`;
    element.style.backgroundColor = 'var(--bg-secondary)';
    element.style.border = '1px solid var(--border-color)';
    element.style.borderRadius = '4px';
    element.style.display = 'flex';
    element.style.flexDirection = 'column';
    element.style.overflow = 'hidden';
    element.style.resize = 'both';

    // Title bar
    const titleBar = document.createElement('div');
    titleBar.className = 'canvas-glyph-title-bar';
    titleBar.style.height = '32px';
    titleBar.style.backgroundColor = 'var(--bg-tertiary)';
    titleBar.style.borderBottom = '1px solid var(--border-color)';
    titleBar.style.display = 'flex';
    titleBar.style.alignItems = 'center';
    titleBar.style.padding = '0 8px';
    titleBar.style.gap = '8px';
    titleBar.style.cursor = 'move';
    titleBar.style.flexShrink = '0';

    const symbol = document.createElement('span');
    symbol.textContent = IX;
    symbol.style.fontSize = '16px';

    const title = document.createElement('span');
    title.textContent = 'Ingest';
    title.style.fontSize = '13px';
    title.style.flex = '1';

    titleBar.appendChild(symbol);
    titleBar.appendChild(title);

    // Content area
    const content = document.createElement('div');
    content.style.flex = '1';
    content.style.padding = '12px';
    content.style.display = 'flex';
    content.style.flexDirection = 'column';
    content.style.gap = '8px';
    content.style.overflow = 'auto';

    // Source label
    const label = document.createElement('label');
    label.textContent = 'Source:';
    label.style.fontSize = '13px';
    label.style.fontWeight = '500';
    label.style.color = 'var(--text-primary)';

    // Textarea
    const textarea = document.createElement('textarea');
    textarea.placeholder = 'Enter URL, file path, or data source...';
    textarea.style.flex = '1';
    textarea.style.padding = '8px';
    textarea.style.fontSize = '13px';
    textarea.style.fontFamily = 'monospace';
    textarea.style.backgroundColor = 'var(--bg-primary)';
    textarea.style.color = 'var(--text-primary)';
    textarea.style.border = '1px solid var(--border-color)';
    textarea.style.borderRadius = '4px';
    textarea.style.resize = 'none';
    textarea.style.minHeight = '60px';

    // Prevent drag from starting on textarea
    textarea.addEventListener('mousedown', (e) => {
        e.stopPropagation();
    });

    // Execute button
    const executeBtn = document.createElement('button');
    executeBtn.textContent = 'Execute';
    executeBtn.style.padding = '6px 12px';
    executeBtn.style.fontSize = '13px';
    executeBtn.style.fontWeight = '500';
    executeBtn.style.backgroundColor = 'var(--accent-primary)';
    executeBtn.style.color = 'var(--text-primary)';
    executeBtn.style.border = 'none';
    executeBtn.style.borderRadius = '4px';
    executeBtn.style.cursor = 'pointer';
    executeBtn.style.alignSelf = 'flex-end';

    executeBtn.addEventListener('click', () => {
        const input = textarea.value.trim();
        if (!input) {
            log.debug(SEG.UI, '[IX] No input provided');
            return;
        }

        log.debug(SEG.UI, `[IX] Executing: ${input}`);
        // TODO: Wire up to ix backend execution
        alert(`IX execution not yet wired up.\n\nInput: ${input}\n\nThis will be sent to the ix backend.`);
    });

    // Assemble
    content.appendChild(label);
    content.appendChild(textarea);
    content.appendChild(executeBtn);

    element.appendChild(titleBar);
    element.appendChild(content);

    // Make draggable via title bar (similar to py-glyph)
    makeDraggable(element, titleBar, glyph);

    return element;
}

/**
 * Make element draggable via handle
 */
function makeDraggable(element: HTMLElement, handle: HTMLElement, glyph: Glyph): void {
    let isDragging = false;
    let startX = 0;
    let startY = 0;
    let initialLeft = 0;
    let initialTop = 0;

    handle.addEventListener('mousedown', (e) => {
        e.preventDefault();
        isDragging = true;

        startX = e.clientX;
        startY = e.clientY;
        initialLeft = element.offsetLeft;
        initialTop = element.offsetTop;

        element.style.opacity = '0.7';
    });

    document.addEventListener('mousemove', (e) => {
        if (!isDragging) return;

        const deltaX = e.clientX - startX;
        const deltaY = e.clientY - startY;

        element.style.left = `${initialLeft + deltaX}px`;
        element.style.top = `${initialTop + deltaY}px`;
    });

    document.addEventListener('mouseup', () => {
        if (!isDragging) return;
        isDragging = false;

        element.style.opacity = '1';

        // Calculate grid position and persist
        const canvas = element.parentElement;
        const canvasRect = canvas?.getBoundingClientRect() ?? { left: 0, top: 0 };
        const elementRect = element.getBoundingClientRect();
        const gridX = Math.round((elementRect.left - canvasRect.left) / GRID_SIZE);
        const gridY = Math.round((elementRect.top - canvasRect.top) / GRID_SIZE);

        glyph.gridX = gridX;
        glyph.gridY = gridY;

        // Persist to uiState
        if (glyph.symbol) {
            uiState.addCanvasGlyph({
                id: glyph.id,
                symbol: glyph.symbol,
                gridX,
                gridY,
                width: element.offsetWidth,
                height: element.offsetHeight
            });
        }

        log.debug(SEG.UI, `[IX Glyph] Moved to grid (${gridX}, ${gridY})`);
    });
}

