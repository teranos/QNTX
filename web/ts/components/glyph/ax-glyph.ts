/**
 * Ax Glyph - Ax query editor on canvas grid
 *
 * Text editor for writing ax queries like "is git", "has certification", etc.
 * Similar to ATS editor or prompt editor - just the query text itself.
 *
 * ARCHITECTURE:
 * Canvas ax glyphs are lightweight references/thumbnails that show the query text.
 * They serve as visual markers on the spatial canvas to show active ax queries.
 *
 * TODO: Future enhancements
 * - Add result count badge (e.g., "42 attestations")
 * - Show mini type distribution (tiny bar chart or colored dots for node types)
 * - Click handler to spawn full ax manifestation (attestation explorer window)
 * - Integration with graph explorer (may reuse existing graph component)
 * - Persist query results to avoid re-execution on reload
 * - Support query templates/snippets
 */

import type { Glyph } from './glyph';
import { AX } from '@generated/sym.js';
import { log, SEG } from '../../logger';
import { GRID_SIZE } from './grid-constants';
import { uiState } from '../../state/ui';

/**
 * Factory function to create an Ax query editor glyph
 *
 * @param id Optional glyph ID
 * @param initialQuery Optional initial query text
 * @param gridX Optional grid X position
 * @param gridY Optional grid Y position
 */
export function createAxGlyph(id?: string, initialQuery: string = '', gridX?: number, gridY?: number): Glyph {
    const glyphId = id || `ax-${crypto.randomUUID()}`;
    let currentQuery = initialQuery;

    const glyph: Glyph = {
        id: glyphId,
        title: 'Ax Query',
        symbol: AX,
        manifestationType: 'ax',
        gridX,
        gridY,
        renderContent: () => {
            // Calculate default size
            const defaultWidth = 400;
            const defaultHeight = 200;
            const width = glyph.width ?? defaultWidth;
            const height = glyph.height ?? defaultHeight;

            // Main container
            const container = document.createElement('div');
            container.className = 'canvas-ax-glyph';
            container.dataset.glyphId = glyphId;

            // Style element - resizable
            container.style.position = 'absolute';
            container.style.left = `${(gridX ?? 5) * GRID_SIZE}px`;
            container.style.top = `${(gridY ?? 5) * GRID_SIZE}px`;
            container.style.width = `${width}px`;
            container.style.height = `${height}px`;
            container.style.minWidth = '200px';
            container.style.minHeight = '120px';
            container.style.backgroundColor = 'var(--bg-secondary)';
            container.style.borderRadius = '4px';
            container.style.border = '1px solid var(--border-color)';
            container.style.display = 'flex';
            container.style.flexDirection = 'column';
            container.style.overflow = 'hidden';
            container.style.zIndex = '1';

            // Title bar for dragging
            const titleBar = document.createElement('div');
            titleBar.className = 'ax-glyph-title-bar';
            titleBar.textContent = AX;
            titleBar.style.padding = '8px';
            titleBar.style.backgroundColor = 'var(--bg-tertiary)';
            titleBar.style.cursor = 'move';
            titleBar.style.userSelect = 'none';
            titleBar.style.fontWeight = 'bold';
            titleBar.style.fontSize = '14px';

            container.appendChild(titleBar);

            // Editor container
            const editorContainer = document.createElement('div');
            editorContainer.className = 'ax-glyph-editor';
            editorContainer.style.flex = '1';
            editorContainer.style.overflow = 'hidden';
            editorContainer.style.display = 'flex';
            editorContainer.style.flexDirection = 'column';

            // Text editor for the ax query
            const editor = document.createElement('textarea');
            editor.className = 'ax-query-textarea';
            editor.value = currentQuery;
            editor.placeholder = 'Enter ax query (e.g., is git, has certification)';
            editor.style.flex = '1';
            editor.style.width = '100%';
            editor.style.padding = '8px';
            editor.style.fontSize = '13px';
            editor.style.fontFamily = 'monospace';
            editor.style.border = 'none';
            editor.style.outline = 'none';
            editor.style.resize = 'none';
            editor.style.backgroundColor = 'var(--bg-primary)';
            editor.style.color = 'var(--text-primary)';
            editor.style.overflow = 'auto';

            // Update current query on change
            editor.addEventListener('input', () => {
                currentQuery = editor.value;
                log.debug(SEG.UI, `[Ax Glyph ${glyphId}] Query updated: ${currentQuery}`);
            });

            editorContainer.appendChild(editor);
            container.appendChild(editorContainer);

            // Resize handle
            const resizeHandle = document.createElement('div');
            resizeHandle.className = 'ax-glyph-resize-handle';
            resizeHandle.style.position = 'absolute';
            resizeHandle.style.bottom = '0';
            resizeHandle.style.right = '0';
            resizeHandle.style.width = '16px';
            resizeHandle.style.height = '16px';
            resizeHandle.style.cursor = 'nwse-resize';
            resizeHandle.style.backgroundColor = 'var(--bg-tertiary)';
            resizeHandle.style.borderTopLeftRadius = '4px';
            container.appendChild(resizeHandle);

            // Make draggable and resizable
            makeDraggable(container, titleBar, glyph);
            makeResizable(container, resizeHandle, glyph);

            return container;
        }
    };

    return glyph;
}

/**
 * Make an element draggable by a handle
 */
function makeDraggable(element: HTMLElement, handle: HTMLElement, glyph: Glyph): void {
    let isDragging = false;
    let dragStartX = 0;
    let dragStartY = 0;
    let elementStartX = 0;
    let elementStartY = 0;
    let abortController: AbortController | null = null;

    const handleMouseMove = (e: MouseEvent) => {
        if (!isDragging) return;

        const deltaX = e.clientX - dragStartX;
        const deltaY = e.clientY - dragStartY;
        const newX = elementStartX + deltaX;
        const newY = elementStartY + deltaY;

        element.style.left = `${newX}px`;
        element.style.top = `${newY}px`;
    };

    const handleMouseUp = () => {
        if (!isDragging) return;
        isDragging = false;

        element.classList.remove('is-dragging');

        // Save position (calculate relative to canvas parent)
        const canvas = element.parentElement;
        const canvasRect = canvas?.getBoundingClientRect() ?? { left: 0, top: 0 };
        const elementRect = element.getBoundingClientRect();
        const gridX = Math.round((elementRect.left - canvasRect.left) / GRID_SIZE);
        const gridY = Math.round((elementRect.top - canvasRect.top) / GRID_SIZE);
        glyph.gridX = gridX;
        glyph.gridY = gridY;

        if (glyph.symbol) {
            uiState.addCanvasGlyph({
                id: glyph.id,
                symbol: glyph.symbol,
                gridX,
                gridY,
                width: glyph.width,
                height: glyph.height
            });
        }

        log.debug(SEG.UI, `[AxGlyph] Finished dragging ${glyph.id}`);

        abortController?.abort();
        abortController = null;
    };

    handle.addEventListener('mousedown', (e) => {
        e.preventDefault();
        e.stopPropagation();
        isDragging = true;

        dragStartX = e.clientX;
        dragStartY = e.clientY;
        const rect = element.getBoundingClientRect();
        elementStartX = rect.left;
        elementStartY = rect.top;

        element.classList.add('is-dragging');

        abortController = new AbortController();
        document.addEventListener('mousemove', handleMouseMove, { signal: abortController.signal });
        document.addEventListener('mouseup', handleMouseUp, { signal: abortController.signal });

        log.debug(SEG.UI, `[AxGlyph] Started dragging ${glyph.id}`);
    });
}

/**
 * Make an element resizable by a handle
 */
function makeResizable(element: HTMLElement, handle: HTMLElement, glyph: Glyph): void {
    let isResizing = false;
    let startX = 0;
    let startY = 0;
    let startWidth = 0;
    let startHeight = 0;
    let abortController: AbortController | null = null;

    const handleMouseMove = (e: MouseEvent) => {
        if (!isResizing) return;

        const deltaX = e.clientX - startX;
        const deltaY = e.clientY - startY;

        const newWidth = Math.max(200, startWidth + deltaX);
        const newHeight = Math.max(120, startHeight + deltaY);

        element.style.width = `${newWidth}px`;
        element.style.height = `${newHeight}px`;
    };

    const handleMouseUp = () => {
        if (!isResizing) return;
        isResizing = false;

        element.classList.remove('is-resizing');

        // Save final size
        const rect = element.getBoundingClientRect();
        const finalWidth = Math.round(rect.width);
        const finalHeight = Math.round(rect.height);

        glyph.width = finalWidth;
        glyph.height = finalHeight;

        // Persist to uiState
        if (glyph.symbol && glyph.gridX !== undefined && glyph.gridY !== undefined) {
            uiState.addCanvasGlyph({
                id: glyph.id,
                symbol: glyph.symbol,
                gridX: glyph.gridX,
                gridY: glyph.gridY,
                width: finalWidth,
                height: finalHeight
            });
        }

        log.debug(SEG.UI, `[AxGlyph] Finished resizing to ${finalWidth}x${finalHeight}`);

        abortController?.abort();
        abortController = null;
    };

    handle.addEventListener('mousedown', (e) => {
        e.preventDefault();
        e.stopPropagation();
        isResizing = true;

        startX = e.clientX;
        startY = e.clientY;
        const rect = element.getBoundingClientRect();
        startWidth = rect.width;
        startHeight = rect.height;

        element.classList.add('is-resizing');

        abortController = new AbortController();
        document.addEventListener('mousemove', handleMouseMove, { signal: abortController.signal });
        document.addEventListener('mouseup', handleMouseUp, { signal: abortController.signal });

        log.debug(SEG.UI, `[AxGlyph] Started resizing`);
    });
}
