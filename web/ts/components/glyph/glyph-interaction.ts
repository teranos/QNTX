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
    /** Minimum width in pixels (default: 200). */
    minWidth?: number;
    /** Minimum height in pixels (default: 120). */
    minHeight?: number;
}

// ── makeDraggable ───────────────────────────────────────────────────

/**
 * Make an element draggable by a handle.
 *
 * Design decision: Uses free-form dragging without live grid snapping.
 * Grid position is calculated only on mouseup for persistence. This provides
 * smoother UX for content glyphs compared to grid-snapped dragging.
 *
 * @param element - The element to make draggable
 * @param handle - The handle that triggers dragging (typically a title bar)
 * @param glyph - The glyph model to update with position
 * @param opts - Optional configuration
 *
 * @example
 * // Basic usage
 * makeDraggable(element, titleBar, glyph, { logLabel: 'PyGlyph' });
 *
 * @example
 * // Ignore button clicks in the handle
 * makeDraggable(element, header, glyph, {
 *   logLabel: 'ResultGlyph',
 *   ignoreButtons: true
 * });
 */
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
                width: glyph.width,
                height: glyph.height,
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

/**
 * Make an element resizable by a handle.
 *
 * Enables resize via a handle (typically in the bottom-right corner).
 * Final dimensions are persisted to the glyph model and uiState.
 *
 * @param element - The element to make resizable
 * @param handle - The resize handle element
 * @param glyph - The glyph model to update with dimensions
 * @param opts - Optional configuration
 *
 * @example
 * // Basic usage with default min size (200x120)
 * makeResizable(element, resizeHandle, glyph, { logLabel: 'IX Glyph' });
 *
 * @example
 * // Custom minimum dimensions
 * makeResizable(element, resizeHandle, glyph, {
 *   logLabel: 'PyGlyph',
 *   minWidth: 300,
 *   minHeight: 200
 * });
 */
export function makeResizable(
    element: HTMLElement,
    handle: HTMLElement,
    glyph: Glyph,
    opts: MakeResizableOptions = {},
): void {
    const { logLabel = 'Glyph', minWidth = 200, minHeight = 120 } = opts;

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

        const newWidth = Math.max(minWidth, startWidth + deltaX);
        const newHeight = Math.max(minHeight, startHeight + deltaY);

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
