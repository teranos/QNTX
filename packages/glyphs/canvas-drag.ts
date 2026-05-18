/**
 * Canvas drag and resize interaction for glyphs.
 *
 * All glyphs on the canvas need pointer-driven move/resize.
 * Uses DI (CanvasHost) for position persistence, transform,
 * selection, and composition state.
 */

import type { Glyph } from './glyph';
import { CANVAS_GLYPH_TITLE_BAR_HEIGHT, MAX_VIEWPORT_HEIGHT_RATIO } from './glyph';
import type { MakeDraggableOptions } from './glyph-ui';
import { isInWindowState } from './dataset';
import { getLogger, getLogSegment, getCanvasHost } from './config';
import {
    canInitiateMeld,
    canReceiveMeld,
    findMeldTarget,
    checkDirectionalProximity,
    PROXIMITY_THRESHOLD,
    MELD_THRESHOLD,
} from './meld/meld-detect';
import { applyMeldFeedback, clearMeldFeedback } from './meld/meld-feedback';
import {
    performMeld,
    extendComposition,
    isMeldedComposition,
} from './meld/meld-composition';
import {
    getMeldOptions,
    selectPreferredMeldOption,
    getGlyphClass,
    getCompatibleDirections,
    isPortFree,
    type EdgeDirection,
} from './meld/meldability';

// Monotonic z-index counter — each drag/click brings glyph to front
let topZIndex = 1;

// ── Composition anchor selection ────────────────────────────────────

/**
 * Find the spatially-nearest glyph in a composition that has a free port
 * compatible with the standalone element, in ANY valid direction.
 *
 * This replaces trusting findMeldTarget's single result as the anchor —
 * findMeldTarget picks the closest glyph on the entire canvas, which may
 * not be the closest glyph *within* the composition the user is targeting.
 * Its detected direction may also be wrong (e.g., 'bottom' when user meant 'right').
 *
 * Returns both the anchor ID and the best direction for that anchor.
 */
function findBestAnchorInComposition(
    standaloneElement: HTMLElement,
    compositionElement: HTMLElement,
    edges: Array<{ from: string; to: string; direction: string }>,
): { anchorId: string; direction: EdgeDirection; role: 'from' | 'to' } | null {
    const standaloneRect = standaloneElement.getBoundingClientRect();
    const standaloneClass = getGlyphClass(standaloneElement);
    if (!standaloneClass) return null;

    let bestId: string | null = null;
    let bestDirection: EdgeDirection = 'right';
    let bestRole: 'from' | 'to' = 'to';
    let bestDistance = Infinity;

    const glyphElements = compositionElement.querySelectorAll('[data-glyph-id]');
    for (const el of glyphElements) {
        const glyphEl = el as HTMLElement;
        const glyphId = glyphEl.dataset.glyphId;
        if (!glyphId) continue;

        const glyphClass = getGlyphClass(glyphEl);
        if (!glyphClass) continue;

        const glyphRect = glyphEl.getBoundingClientRect();

        // Append: composition glyph → standalone (outgoing port)
        for (const dir of getCompatibleDirections(glyphClass, standaloneClass)) {
            if (!isPortFree(glyphId, dir, 'outgoing', edges)) continue;
            const dist = checkDirectionalProximity(glyphRect, standaloneRect, dir);
            if (dist < bestDistance) {
                bestDistance = dist;
                bestId = glyphId;
                bestDirection = dir;
                bestRole = 'to';
            }
        }

        // Prepend: standalone → composition glyph (incoming port)
        for (const dir of getCompatibleDirections(standaloneClass, glyphClass)) {
            if (!isPortFree(glyphId, dir, 'incoming', edges)) continue;
            const dist = checkDirectionalProximity(standaloneRect, glyphRect, dir);
            if (dist < bestDistance) {
                bestDistance = dist;
                bestId = glyphId;
                bestDirection = dir;
                bestRole = 'from';
            }
        }
    }

    if (!bestId) return null;
    return { anchorId: bestId, direction: bestDirection, role: bestRole };
}

// ── Options ─────────────────────────────────────────────────────────

export type { MakeDraggableOptions };

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
    /** Use minHeight instead of height (glyph grows with content) */
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
    const log = getLogger();
    const seg = getLogSegment();
    const existing = (element as any).__resizeObserver;
    if (existing && typeof existing.disconnect === 'function') {
        existing.disconnect();
        delete (element as any).__resizeObserver;
        if (glyphId) {
            log.debug(seg, `[${glyphId}] Disconnected ResizeObserver`);
        }
    }
}

/**
 * Set up a ResizeObserver that auto-sizes a glyph element to its content.
 *
 * Cleans up any existing observer first, caps height at MAX_VIEWPORT_HEIGHT_RATIO,
 * and stores the observer on the element for later cleanup.
 *
 * @param glyphElement - The glyph DOM element whose minHeight is adjusted
 * @param contentElement - The inner element to observe for size changes
 * @param label - Log label (e.g. "AX abc123")
 * @param heightOffset - Pixels to add to content height (default: CANVAS_GLYPH_TITLE_BAR_HEIGHT)
 */
export function setupGlyphResizeObserver(
    glyphElement: HTMLElement,
    contentElement: HTMLElement,
    label: string,
    heightOffset?: number,
): void {
    const log = getLogger();
    const seg = getLogSegment();

    cleanupResizeObserver(glyphElement, label);

    const offset = heightOffset ?? CANVAS_GLYPH_TITLE_BAR_HEIGHT;
    const maxHeight = window.innerHeight * MAX_VIEWPORT_HEIGHT_RATIO;

    const resizeObserver = new ResizeObserver(entries => {
        for (const entry of entries) {
            const contentHeight = entry.contentRect.height;
            const totalHeight = Math.min(contentHeight + offset, maxHeight);
            glyphElement.style.minHeight = `${totalHeight}px`;
            log.debug(seg, `[${label}] Auto-resized to ${totalHeight}px (content: ${contentHeight}px)`);
        }
    });

    resizeObserver.observe(contentElement);
    (glyphElement as any).__resizeObserver = resizeObserver;
}

// ── preventDrag ─────────────────────────────────────────────────────

/**
 * Prevent drag from starting on an interactive child element.
 *
 * Canvas glyphs are draggable, but their interactive children (textareas,
 * buttons, inputs) need to receive mousedown without triggering a drag.
 * This stops the event from bubbling to the drag handler.
 *
 * Also marks elements with data-prevent-drag so the canvas click handler
 * knows to skip focus theft and glyph selection for these elements.
 */
export function preventDrag(...elements: HTMLElement[]): void {
    for (const el of elements) {
        el.dataset.preventDrag = '';
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
 */
export function makeDraggable(
    element: HTMLElement,
    handle: HTMLElement,
    glyph: Glyph,
    opts: MakeDraggableOptions = {},
): () => void {
    const { ignoreButtons = false, logLabel = 'Glyph' } = opts;
    const log = getLogger();
    const seg = getLogSegment();
    const canvasHost = getCanvasHost();

    // AbortController for all event listeners (including mousedown)
    const setupController = new AbortController();

    let isDragging = false;
    let dragStartX = 0;
    let dragStartY = 0;
    let elementStartX = 0;
    let elementStartY = 0;
    let dragController: AbortController | null = null;
    let currentMeldTarget: HTMLElement | null = null;
    let rafId: number | null = null;
    let dragCanvasId = '';

    // Multi-selection drag support
    let isMultiDrag = false;
    let multiDragElements: Array<{ element: HTMLElement; startX: number; startY: number; glyph: Glyph }> = [];

    const handleMouseMove = (e: MouseEvent) => {
        if (!isDragging) return;

        const scale = canvasHost.getTransform(dragCanvasId).scale || 1;
        const deltaX = (e.clientX - dragStartX) / scale;
        const deltaY = (e.clientY - dragStartY) / scale;

        if (isMultiDrag) {
            for (const { element: el, startX, startY } of multiDragElements) {
                const newX = startX + deltaX;
                const newY = startY + deltaY;
                el.style.left = `${newX}px`;
                el.style.top = `${newY}px`;
            }
        } else {
            const newX = elementStartX + deltaX;
            const newY = elementStartY + deltaY;
            element.style.left = `${newX}px`;
            element.style.top = `${newY}px`;
        }

        // Cancel any pending meld feedback update
        if (rafId !== null) {
            cancelAnimationFrame(rafId);
        }

        // Schedule meld feedback for next frame
        if (canInitiateMeld(element) || canReceiveMeld(element)) {
            rafId = requestAnimationFrame(() => {
                rafId = null;
                const meldInfo = findMeldTarget(element);
                if (meldInfo.target && meldInfo.distance < PROXIMITY_THRESHOLD) {
                    const [initiator, target] = meldInfo.reversed
                        ? [meldInfo.target, element]
                        : [element, meldInfo.target];
                    applyMeldFeedback(initiator, target, meldInfo.distance, meldInfo.direction);
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

        if (rafId !== null) {
            cancelAnimationFrame(rafId);
            rafId = null;
        }

        element.classList.remove('is-dragging');
        if (isMultiDrag) {
            for (const { element: el } of multiDragElements) {
                el.classList.remove('is-dragging');
            }
        }

        // Check if we should meld
        if (canInitiateMeld(element) || canReceiveMeld(element)) {
            const meldInfo = findMeldTarget(element);
            if (meldInfo.target && meldInfo.distance < MELD_THRESHOLD) {
                const nearbyElement = meldInfo.target;
                const nearbyGlyphId = nearbyElement.dataset.glyphId || 'glyph-unknown';

                const nearbyGlyph: Glyph = {
                    id: nearbyGlyphId,
                    title: 'Glyph',
                    renderContent: () => nearbyElement
                };

                if (rafId !== null) {
                    cancelAnimationFrame(rafId);
                    rafId = null;
                }
                clearMeldFeedback(element);
                clearMeldFeedback(nearbyElement);
                setupController.abort();
                dragController?.abort();

                const [meldInitiator, meldTarget, meldInitiatorGlyph, meldTargetGlyph] = meldInfo.reversed
                    ? [nearbyElement, element, nearbyGlyph, glyph]
                    : [element, nearbyElement, glyph, nearbyGlyph];

                const targetComp = meldTarget.closest('.melded-composition') as HTMLElement | null;
                const initiatorComp = meldInitiator.closest('.melded-composition') as HTMLElement | null;

                if (targetComp || initiatorComp) {
                    const compositionElement = (targetComp || initiatorComp)!;
                    const standaloneElement = targetComp ? meldInitiator : meldTarget;
                    const standaloneId = standaloneElement.dataset.glyphId || '';
                    const standaloneClass = getGlyphClass(standaloneElement);
                    const fallbackAnchorId = (targetComp ? meldTarget : meldInitiator).dataset.glyphId || '';
                    const existingComp = canvasHost.findCompositionByGlyph(fallbackAnchorId);

                    if (existingComp && standaloneClass) {
                        const selectedIds = canvasHost.getSelectedGlyphIds(dragCanvasId);
                        const selectedAnchor = selectedIds.find(id =>
                            compositionElement.querySelector(`[data-glyph-id="${id}"]`) !== null
                        );

                        let bestAnchorId: string;
                        let bestDirection: EdgeDirection;
                        if (selectedAnchor) {
                            bestAnchorId = selectedAnchor;
                            bestDirection = meldInfo.direction;
                        } else {
                            const bestAnchor = findBestAnchorInComposition(
                                standaloneElement, compositionElement, existingComp.edges
                            );
                            bestAnchorId = bestAnchor?.anchorId || fallbackAnchorId;
                            bestDirection = bestAnchor?.direction || meldInfo.direction;
                        }

                        const options = getMeldOptions(standaloneClass, compositionElement, existingComp.edges);
                        const option = selectPreferredMeldOption(options, bestAnchorId, bestDirection);

                        if (option) {
                            extendComposition(compositionElement, standaloneElement, standaloneId, option.glyphId, option.direction, option.incomingRole);

                            const updatedId = compositionElement.getAttribute('data-glyph-id') || '';
                            const compositionGlyph: Glyph = {
                                id: updatedId,
                                title: 'Melded Composition',
                                renderContent: () => compositionElement
                            };
                            makeDraggable(compositionElement, compositionElement, compositionGlyph, {
                                logLabel: 'MeldedComposition'
                            });

                            log.info(seg, `[${logLabel}] Extended composition with ${standaloneId} (${option.direction}, ${option.incomingRole})`);
                            return;
                        }
                    }
                    log.debug(seg, `[${logLabel}] No free ports for ${standaloneId}, skipping meld`);
                    return;
                }

                // Neither is in a composition — create new 2-glyph composition
                const composition = performMeld(meldInitiator, meldTarget, meldInitiatorGlyph, meldTargetGlyph, meldInfo.direction);

                const compositionGlyph: Glyph = {
                    id: composition.getAttribute('data-glyph-id') || `melded-${meldInitiatorGlyph.id}-${meldTargetGlyph.id}`,
                    title: 'Melded Composition',
                    renderContent: () => composition
                };

                makeDraggable(composition, composition, compositionGlyph, {
                    logLabel: 'MeldedComposition'
                });

                log.info(seg, `[${logLabel}] Melded ${meldInitiatorGlyph.id} → ${meldTargetGlyph.id} (${meldInfo.direction}${meldInfo.reversed ? ', reversed' : ''})`);
                return;
            }
        }

        // Clear any meld feedback
        clearMeldFeedback(element);
        currentMeldTarget = null;

        // Save positions
        if (isMeldedComposition(element)) {
            const x = Math.round(parseFloat(element.style.left) || 0);
            const y = Math.round(parseFloat(element.style.top) || 0);
            const compositionId = element.getAttribute('data-glyph-id') || '';
            const firstChild = element.querySelector('[data-glyph-id]');
            const childId = firstChild?.getAttribute('data-glyph-id') || '';
            const existingComp = canvasHost.findCompositionByGlyph(childId);
            if (existingComp) {
                canvasHost.saveComposition({ ...existingComp, x, y });
                log.debug(seg, `[${logLabel}] Updated composition position`, { compositionId, x, y });
            } else {
                log.warn(seg, `[${logLabel}] Composition ${compositionId} not found in storage`);
            }
        } else if (isMultiDrag) {
            for (const { element: el, glyph: g } of multiDragElements) {
                const x = Math.round(parseFloat(el.style.left) || 0);
                const y = Math.round(parseFloat(el.style.top) || 0);
                g.x = x;
                g.y = y;
                if (g.symbol) {
                    const existing = canvasHost.getCanvasGlyphs().find(cg => cg.id === g.id);
                    canvasHost.saveCanvasGlyph({
                        ...existing,
                        id: g.id,
                        symbol: g.symbol,
                        x,
                        y,
                        width: g.width,
                        height: g.height,
                    });
                }
            }
            log.debug(seg, `[${logLabel}] Finished multi-dragging ${multiDragElements.length} glyphs`);
            multiDragElements = [];
            isMultiDrag = false;
        } else {
            const x = Math.round(parseFloat(element.style.left) || 0);
            const y = Math.round(parseFloat(element.style.top) || 0);
            glyph.x = x;
            glyph.y = y;
            if (glyph.symbol) {
                const existing = canvasHost.getCanvasGlyphs().find(g => g.id === glyph.id);
                canvasHost.saveCanvasGlyph({
                    ...existing,
                    id: glyph.id,
                    symbol: glyph.symbol,
                    x,
                    y,
                    width: glyph.width,
                    height: glyph.height,
                });
            }
            log.debug(seg, `[${logLabel}] Finished dragging ${glyph.id}`);
        }

        dragController?.abort();
        dragController = null;
    };

    handle.addEventListener('mousedown', (e) => {
        if (ignoreButtons && (e.target as HTMLElement).tagName === 'BUTTON') {
            return;
        }

        if (isInWindowState(element)) {
            return;
        }

        if (element.closest('.melded-composition') && !element.classList.contains('melded-composition')) {
            return;
        }

        e.preventDefault();
        e.stopPropagation();
        isDragging = true;

        dragStartX = e.clientX;
        dragStartY = e.clientY;
        elementStartX = element.offsetLeft;
        elementStartY = element.offsetTop;

        element.classList.add('is-dragging');
        element.style.zIndex = String(++topZIndex);

        const canvasId = (element.closest('[data-canvas-id]') as HTMLElement | null)?.dataset?.canvasId ?? 'canvas-workspace';
        dragCanvasId = canvasId;
        const selectedIds = canvasHost.getSelectedGlyphIds(canvasId);
        if (selectedIds.length > 1 && canvasHost.isGlyphSelected(canvasId, glyph.id)) {
            isMultiDrag = true;
            const canvas = element.parentElement;
            if (canvas) {
                for (const id of selectedIds) {
                    const el = canvas.querySelector(`[data-glyph-id="${id}"]`) as HTMLElement | null;
                    if (el) {
                        const elRect = el.getBoundingClientRect();
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
                            startX: el.offsetLeft,
                            startY: el.offsetTop,
                            glyph: glyphData
                        });
                        el.classList.add('is-dragging');
                        el.style.zIndex = element.style.zIndex;
                    }
                }
            }
        }

        dragController = new AbortController();
        document.addEventListener('mousemove', handleMouseMove, { signal: dragController.signal });
        document.addEventListener('mouseup', handleMouseUp, { signal: dragController.signal });

        log.debug(seg, `[${logLabel}] Started dragging ${isMultiDrag ? `${selectedIds.length} glyphs` : glyph.id}`);
    }, { signal: setupController.signal });

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
 * Final dimensions are persisted via CanvasHost.
 *
 * @param element - The element to make resizable
 * @param handle - The resize handle element
 * @param glyph - The glyph model to update with dimensions
 * @param opts - Optional configuration
 */
export function makeResizable(
    element: HTMLElement,
    handle: HTMLElement,
    glyph: Glyph,
    opts: MakeResizableOptions = {},
): () => void {
    const { logLabel = 'Glyph', minWidth = 200, minHeight = 120 } = opts;
    const log = getLogger();
    const seg = getLogSegment();
    const canvasHost = getCanvasHost();

    const setupController = new AbortController();
    let isResizing = false;
    let startX = 0;
    let startY = 0;
    let startWidth = 0;
    let startHeight = 0;
    let abortController: AbortController | null = null;
    let resizeCanvasId = '';

    const handleMouseMove = (e: MouseEvent) => {
        if (!isResizing) return;

        const scale = canvasHost.getTransform(resizeCanvasId).scale || 1;
        const deltaX = (e.clientX - startX) / scale;
        const deltaY = (e.clientY - startY) / scale;

        const newWidth = Math.max(minWidth, startWidth + deltaX);
        const newHeight = Math.max(minHeight, startHeight + deltaY);

        element.style.width = `${newWidth}px`;
        element.style.height = `${newHeight}px`;
    };

    const handleMouseUp = () => {
        if (!isResizing) return;
        isResizing = false;

        element.classList.remove('is-resizing');

        const finalWidth = element.offsetWidth;
        const finalHeight = element.offsetHeight;

        glyph.width = finalWidth;
        glyph.height = finalHeight;

        if (glyph.symbol && glyph.x !== undefined && glyph.y !== undefined) {
            const existing = canvasHost.getCanvasGlyphs().find(g => g.id === glyph.id);
            canvasHost.saveCanvasGlyph({
                ...existing,
                id: glyph.id,
                symbol: glyph.symbol,
                x: glyph.x,
                y: glyph.y,
                width: finalWidth,
                height: finalHeight,
            });
        }

        log.debug(seg, `[${logLabel}] Finished resizing to ${finalWidth}x${finalHeight}`);

        abortController?.abort();
        abortController = null;
    };

    handle.addEventListener('mousedown', (e) => {
        e.preventDefault();
        e.stopPropagation();
        isResizing = true;

        startX = e.clientX;
        startY = e.clientY;
        resizeCanvasId = (element.closest('[data-canvas-id]') as HTMLElement | null)?.dataset?.canvasId ?? '';
        startWidth = element.offsetWidth;
        startHeight = element.offsetHeight;

        element.classList.add('is-resizing');

        abortController = new AbortController();
        document.addEventListener('mousemove', handleMouseMove, { signal: abortController.signal });
        document.addEventListener('mouseup', handleMouseUp, { signal: abortController.signal });

        log.debug(seg, `[${logLabel}] Started resizing`);
    }, { signal: setupController.signal });

    return () => {
        setupController.abort();
        abortController?.abort();
    };
}
