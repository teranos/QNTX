/**
 * Shared drag and resize interaction for canvas glyphs.
 *
 * All glyphs that live on the canvas need the same pointer-driven
 * move / resize behaviour.  The logic was previously duplicated
 * across py-glyph, ix-glyph and result-glyph.
 */

import type { Glyph } from './glyph';
import { log, SEG } from '../../logger';
import { uiState } from '../../state/ui';
import { GRID_SIZE } from './grid-constants';

// ── Options ─────────────────────────────────────────────────────────

export interface MakeDraggableOptions {
    /** When true the drag is cancelled if the mousedown target is a <button>. */
    ignoreButtons?: boolean;
    /** Label used in log messages, e.g. "PyGlyph". */
    logLabel?: string;
}

export interface MakeResizableOptions {
    /** Label used in log messages, e.g. "PyGlyph". */
    logLabel?: string;
}

// ── makeDraggable ───────────────────────────────────────────────────

export function makeDraggable(
    element: HTMLElement,
    handle: HTMLElement,
    glyph: Glyph,
    opts: MakeDraggableOptions = {},
): void {
    const { ignoreButtons = false, logLabel = 'Glyph' } = opts;

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

        // Save position relative to canvas parent
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
            });
        }

        log.debug(SEG.UI, `[${logLabel}] Finished dragging ${glyph.id}`);

        abortController?.abort();
        abortController = null;
    };

    handle.addEventListener('mousedown', (e) => {
        if (ignoreButtons && (e.target as HTMLElement).tagName === 'BUTTON') {
            return;
        }

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

        log.debug(SEG.UI, `[${logLabel}] Started dragging ${glyph.id}`);
    });
}

// ── makeResizable ───────────────────────────────────────────────────

export function makeResizable(
    element: HTMLElement,
    handle: HTMLElement,
    glyph: Glyph,
    opts: MakeResizableOptions = {},
): void {
    const { logLabel = 'Glyph' } = opts;

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
                height: finalHeight,
            });
        }

        log.debug(SEG.UI, `[${logLabel}] Finished resizing to ${finalWidth}x${finalHeight}`);

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

        log.debug(SEG.UI, `[${logLabel}] Started resizing`);
    });
}
