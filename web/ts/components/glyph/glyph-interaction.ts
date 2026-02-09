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
import {
    canInitiateMeld,
    findMeldTarget,
    applyMeldFeedback,
    clearMeldFeedback,
    performMeld,
    isMeldedComposition,
    PROXIMITY_THRESHOLD,
    MELD_THRESHOLD
} from './meld-system';
import { isGlyphSelected, getSelectedGlyphIds } from './canvas-glyph';
import { addComposition, findCompositionByGlyph } from '../../state/compositions';

// ── Options ─────────────────────────────────────────────────────────

export interface MakeDraggableOptions {
    /** When true the drag is cancelled if the mousedown target is a <button>. */
    ignoreButtons?: boolean;
    /** Label used in log messages, e.g. "PyGlyph". */
    logLabel?: string;
    /** The prompt glyph object (if this is a prompt being made draggable) */
    promptGlyph?: Glyph;
}

export interface MakeResizableOptions {
    /** Label used in log messages, e.g. "PyGlyph". */
    logLabel?: string;
    /** Minimum width in pixels (default: 200). */
    minWidth?: number;
    /** Minimum height in pixels (default: 120). */
    minHeight?: number;
}

// ── applyCanvasGlyphLayout ──────────────────────────────────────────

export interface CanvasGlyphLayoutOptions {
    x: number;
    y: number;
    width: number;
    height: number;
    /** Use minHeight instead of height (e.g. ix-glyph grows with content) */
    useMinHeight?: boolean;
}

/**
 * Apply shared positioning and flex layout to a canvas-placed glyph.
 *
 * Pairs with the `.canvas-glyph` CSS class which provides the visual
 * defaults (background, border, border-radius, overflow). This function
 * handles the instance-specific values that can't live in CSS (x/y/size).
 */
export function applyCanvasGlyphLayout(el: HTMLElement, opts: CanvasGlyphLayoutOptions): void {
    el.style.left = `${opts.x}px`;
    el.style.top = `${opts.y}px`;
    el.style.width = `${opts.width}px`;
    if (opts.useMinHeight) {
        el.style.minHeight = `${opts.height}px`;
    } else {
        el.style.height = `${opts.height}px`;
    }
}

// ── Cleanup registry ────────────────────────────────────────────────

const CLEANUP_KEY = '__glyphCleanup';

/**
 * Store a cleanup function on a glyph element.
 * Called by glyph setup code so conversions can tear down handlers
 * before repopulating the same element as a different glyph type.
 */
export function storeCleanup(element: HTMLElement, fn: () => void): void {
    const list: Array<() => void> = (element as any)[CLEANUP_KEY] ??= [];
    list.push(fn);
}

/**
 * Run all stored cleanup functions and clear the list.
 * Tears down drag, resize, editor, and observer handlers
 * so the element can be repopulated as a different glyph type.
 */
export function runCleanup(element: HTMLElement): void {
    const list: Array<() => void> | undefined = (element as any)[CLEANUP_KEY];
    if (list) {
        for (const fn of list) fn();
        (element as any)[CLEANUP_KEY] = [];
    }
}

/**
 * Clean up ResizeObserver attached to an element.
 * Prevents memory leaks when glyphs are removed or re-rendered.
 */
export function cleanupResizeObserver(element: HTMLElement, glyphId?: string): void {
    const existing = (element as any).__resizeObserver;
    if (existing && typeof existing.disconnect === 'function') {
        existing.disconnect();
        delete (element as any).__resizeObserver;
        if (glyphId) {
            log.debug(SEG.GLYPH, `[${glyphId}] Disconnected ResizeObserver`);
        }
    }
}

// ── preventDrag ─────────────────────────────────────────────────────

/**
 * Prevent drag from starting on an interactive child element.
 *
 * Canvas glyphs are draggable, but their interactive children (textareas,
 * buttons, inputs) need to receive mousedown without triggering a drag.
 * This stops the event from bubbling to the drag handler.
 */
export function preventDrag(...elements: HTMLElement[]): void {
    for (const el of elements) {
        el.addEventListener('mousedown', (e) => {
            e.stopPropagation();
        });
    }
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
 * @returns Cleanup function to remove all event listeners
 *
 * @example
 * // Basic usage
 * const cleanup = makeDraggable(element, titleBar, glyph, { logLabel: 'PyGlyph' });
 * // Later: cleanup();
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
): () => void {
    const { ignoreButtons = false, logLabel = 'Glyph' } = opts;

    // AbortController for all event listeners (including mousedown)
    const setupController = new AbortController();

    let isDragging = false;
    let dragStartX = 0;
    let dragStartY = 0;
    let elementStartX = 0;
    let elementStartY = 0;
    let dragController: AbortController | null = null;
    let currentMeldTarget: HTMLElement | null = null;
    let rafId: number | null = null; // Track requestAnimationFrame for meld feedback

    // Multi-selection drag support
    let isMultiDrag = false;
    let multiDragElements: Array<{ element: HTMLElement; startX: number; startY: number; glyph: Glyph }> = [];

    const handleMouseMove = (e: MouseEvent) => {
        if (!isDragging) return;

        const deltaX = e.clientX - dragStartX;
        const deltaY = e.clientY - dragStartY;

        if (isMultiDrag) {
            // Move all selected glyphs together
            for (const { element: el, startX, startY } of multiDragElements) {
                const newX = startX + deltaX;
                const newY = startY + deltaY;
                el.style.left = `${newX}px`;
                el.style.top = `${newY}px`;
            }
        } else {
            // Single glyph drag
            const newX = elementStartX + deltaX;
            const newY = elementStartY + deltaY;
            element.style.left = `${newX}px`;
            element.style.top = `${newY}px`;
        }

        // Cancel any pending meld feedback update to prevent race conditions
        if (rafId !== null) {
            cancelAnimationFrame(rafId);
        }

        // Schedule meld feedback for next frame (prevents interleaving during fast drags)
        if (canInitiateMeld(element)) {
            rafId = requestAnimationFrame(() => {
                rafId = null;
                const meldInfo = findMeldTarget(element);
                if (meldInfo.target && meldInfo.distance < PROXIMITY_THRESHOLD) {
                    applyMeldFeedback(element, meldInfo.target, meldInfo.distance);
                    currentMeldTarget = meldInfo.target;
                } else if (currentMeldTarget) {
                    clearMeldFeedback(element);
                    currentMeldTarget = null;
                }
            });
        }
    };

    const handleMouseUp = () => {
        if (!isDragging) return;
        isDragging = false;

        // Cancel any pending meld feedback animation
        if (rafId !== null) {
            cancelAnimationFrame(rafId);
            rafId = null;
        }

        // Remove dragging class from all elements
        element.classList.remove('is-dragging');
        if (isMultiDrag) {
            for (const { element: el } of multiDragElements) {
                el.classList.remove('is-dragging');
            }
        }

        // Check if we should meld (for ax-glyphs only)
        if (canInitiateMeld(element)) {
            const meldInfo = findMeldTarget(element);
            if (meldInfo.target && meldInfo.distance < MELD_THRESHOLD) {
                const targetElement = meldInfo.target; // Store for type safety

                // Get the prompt glyph ID from the target element
                const promptGlyphId = targetElement.dataset.glyphId || 'prompt-unknown';

                // Create minimal glyph object for the target
                const targetGlyph: Glyph = {
                    id: promptGlyphId,
                    title: 'Prompt',
                    renderContent: () => targetElement
                };

                // Clean up event listeners and animations before melding
                if (rafId !== null) {
                    cancelAnimationFrame(rafId);
                    rafId = null;
                }
                setupController.abort();
                dragController?.abort();

                // Perform the meld - this reparents the actual DOM elements
                const composition = performMeld(element, targetElement, glyph, targetGlyph, meldInfo.direction);

                // Make the composition draggable as a unit
                const compositionGlyph: Glyph = {
                    id: `melded-${glyph.id}-${promptGlyphId}`,
                    title: 'Melded Composition',
                    renderContent: () => composition
                };

                makeDraggable(composition, composition, compositionGlyph, {
                    logLabel: 'MeldedComposition'
                });

                log.info(SEG.GLYPH, `[${logLabel}] Melded with prompt glyph`);
                return;
            }
        }

        // Clear any meld feedback
        clearMeldFeedback(element);
        currentMeldTarget = null;

        // Save positions for all dragged glyphs
        const canvas = element.parentElement;
        const canvasRect = canvas?.getBoundingClientRect() ?? { left: 0, top: 0 };

        // Check if we're dragging a melded composition
        if (isMeldedComposition(element)) {
            const elementRect = element.getBoundingClientRect();
            const x = Math.round(elementRect.left - canvasRect.left);
            const y = Math.round(elementRect.top - canvasRect.top);

            // Get composition data from DOM
            const compositionId = element.getAttribute('data-glyph-id') || '';
            const initiatorId = element.getAttribute('data-initiator-id') || '';

            // Find existing composition in storage to get type
            const existingComp = findCompositionByGlyph(initiatorId);
            if (existingComp) {
                // Update composition position
                addComposition({
                    ...existingComp,
                    x,
                    y
                });
                log.debug(SEG.GLYPH, `[${logLabel}] Updated composition position`, {
                    compositionId,
                    x,
                    y
                });
            } else {
                log.warn(SEG.GLYPH, `[${logLabel}] Composition ${compositionId} not found in storage`);
            }
        } else if (isMultiDrag) {
            // Save positions for all selected glyphs
            for (const { element: el, glyph: g } of multiDragElements) {
                const rect = el.getBoundingClientRect();
                const x = Math.round(rect.left - canvasRect.left);
                const y = Math.round(rect.top - canvasRect.top);
                g.x = x;
                g.y = y;

                if (g.symbol) {
                    uiState.addCanvasGlyph({
                        id: g.id,
                        symbol: g.symbol,
                        x,
                        y,
                        width: g.width,
                        height: g.height,
                        result: g.result, // Preserve result data for result glyphs
                    });
                }
            }
            log.debug(SEG.GLYPH, `[${logLabel}] Finished multi-dragging ${multiDragElements.length} glyphs`);
            multiDragElements = [];
            isMultiDrag = false;
        } else {
            // Single glyph position save
            const elementRect = element.getBoundingClientRect();
            const x = Math.round(elementRect.left - canvasRect.left);
            const y = Math.round(elementRect.top - canvasRect.top);
            glyph.x = x;
            glyph.y = y;

            if (glyph.symbol) {
                uiState.addCanvasGlyph({
                    id: glyph.id,
                    symbol: glyph.symbol,
                    x,
                    y,
                    width: glyph.width,
                    height: glyph.height,
                    result: glyph.result, // Preserve result data for result glyphs
                });
            }
            log.debug(SEG.GLYPH, `[${logLabel}] Finished dragging ${glyph.id}`);
        }

        dragController?.abort();
        dragController = null;
    };

    handle.addEventListener('mousedown', (e) => {
        if (ignoreButtons && (e.target as HTMLElement).tagName === 'BUTTON') {
            return;
        }

        // Don't allow dragging child glyphs inside compositions - only drag the composition itself
        if (element.closest('.melded-composition') && !element.classList.contains('melded-composition')) {
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

        // Check if this glyph is part of a multi-selection
        const selectedIds = getSelectedGlyphIds();
        if (selectedIds.length > 1 && isGlyphSelected(glyph.id)) {
            isMultiDrag = true;
            const canvas = element.parentElement;
            if (canvas) {
                // Gather all selected glyphs with their initial positions
                for (const id of selectedIds) {
                    const el = canvas.querySelector(`[data-glyph-id="${id}"]`) as HTMLElement | null;
                    if (el) {
                        const elRect = el.getBoundingClientRect();
                        // Create a glyph object for tracking with current dimensions
                        const glyphData: Glyph = {
                            id,
                            title: el.dataset.glyphTitle || 'Glyph',
                            symbol: el.dataset.glyphSymbol,
                            width: Math.round(elRect.width),
                            height: Math.round(elRect.height),
                            renderContent: () => el
                        };
                        multiDragElements.push({
                            element: el,
                            startX: elRect.left,
                            startY: elRect.top,
                            glyph: glyphData
                        });
                        el.classList.add('is-dragging');
                    }
                }
            }
        }

        dragController = new AbortController();
        document.addEventListener('mousemove', handleMouseMove, { signal: dragController.signal });
        document.addEventListener('mouseup', handleMouseUp, { signal: dragController.signal });

        log.debug(SEG.GLYPH, `[${logLabel}] Started dragging ${isMultiDrag ? `${selectedIds.length} glyphs` : glyph.id}`);
    }, { signal: setupController.signal });

    // Return cleanup function
    return () => {
        if (rafId !== null) {
            cancelAnimationFrame(rafId);
        }
        setupController.abort();
        dragController?.abort();
    };
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
        if (glyph.symbol && glyph.x !== undefined && glyph.y !== undefined) {
            uiState.addCanvasGlyph({
                id: glyph.id,
                symbol: glyph.symbol,
                x: glyph.x,
                y: glyph.y,
                width: finalWidth,
                height: finalHeight,
            });
        }

        log.debug(SEG.GLYPH, `[${logLabel}] Finished resizing to ${finalWidth}x${finalHeight}`);

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

        log.debug(SEG.GLYPH, `[${logLabel}] Started resizing`);
    });
}
