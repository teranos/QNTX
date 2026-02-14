/**
 * Rectangle Selection for Canvas
 *
 * Allows dragging on canvas background to create a selection rectangle
 * that selects individual glyphs (including glyphs within compositions).
 *
 * Behavior:
 * - Plain drag: Replace current selection
 * - Shift+drag: Add to current selection
 * - Only activates on canvas background (not on glyphs)
 */

import { log, SEG } from '../../../logger';

interface RectangleSelectionState {
    active: boolean;
    startX: number;
    startY: number;
    rect: HTMLElement | null;
    shiftKey: boolean;
}

// Track if we just completed a rectangle selection (to prevent click deselection)
let rectangleSelectionJustCompleted = false;

/**
 * Check if rectangle selection just completed
 * Used by click handler to avoid deselecting immediately after rectangle selection
 */
export function didRectangleSelectionJustComplete(): boolean {
    if (rectangleSelectionJustCompleted) {
        rectangleSelectionJustCompleted = false;  // Reset flag
        return true;
    }
    return false;
}

/**
 * Setup rectangle selection on a canvas container
 *
 * @param container - The canvas element
 * @param selectGlyph - Function to select a glyph by ID
 * @param deselectAll - Function to deselect all glyphs
 * @returns Cleanup function to remove event listeners
 */
export function setupRectangleSelection(
    container: HTMLElement,
    selectGlyph: (glyphId: string, container: HTMLElement, addToSelection: boolean) => void,
    deselectAll: (container: HTMLElement) => void
): () => void {
    const abortController = new AbortController();
    const signal = abortController.signal;

    let rectangleSelection: RectangleSelectionState = {
        active: false,
        startX: 0,
        startY: 0,
        rect: null,
        shiftKey: false
    };

    // Start rectangle selection on mousedown (canvas background only)
    container.addEventListener('mousedown', (e) => {
        const target = e.target as HTMLElement;

        // Only start rectangle selection on canvas background (not on glyphs)
        // Scope check to this workspace: glyph ancestors outside the container
        // (e.g. parent subcanvas element) are ignored
        const glyphEl = target.closest('[data-glyph-id]') as HTMLElement | null;
        if (glyphEl && glyphEl !== container && container.contains(glyphEl)) {
            return;
        }

        // Allow rectangle selection to start on the container or its content layer
        const isCanvasChild = target === container ||
                              target.classList.contains('canvas-content-layer');

        if (!isCanvasChild) {
            return;
        }

        log.debug(SEG.GLYPH, '[RectangleSelection] Starting selection', {
            target: target.className,
            shiftKey: e.shiftKey
        });

        // Don't interfere with right-click
        if (e.button !== 0) {
            return;
        }

        const containerRect = container.getBoundingClientRect();
        rectangleSelection = {
            active: true,
            startX: e.clientX - containerRect.left,
            startY: e.clientY - containerRect.top,
            rect: null,
            shiftKey: e.shiftKey
        };

        // Create selection rectangle
        const rect = document.createElement('div');
        rect.className = 'canvas-selection-rectangle';
        rect.style.position = 'absolute';
        rect.style.left = `${rectangleSelection.startX}px`;
        rect.style.top = `${rectangleSelection.startY}px`;
        rect.style.width = '0px';
        rect.style.height = '0px';
        rect.style.pointerEvents = 'none'; // Don't interfere with other mouse events
        container.appendChild(rect);
        rectangleSelection.rect = rect;

        e.preventDefault();

        log.debug(SEG.GLYPH, '[RectangleSelection] Started', {
            startX: rectangleSelection.startX,
            startY: rectangleSelection.startY,
            shiftKey: rectangleSelection.shiftKey
        });
    }, { signal });

    // Update rectangle dimensions on mousemove
    container.addEventListener('mousemove', (e) => {
        if (!rectangleSelection.active || !rectangleSelection.rect) {
            return;
        }

        const containerRect = container.getBoundingClientRect();
        const currentX = e.clientX - containerRect.left;
        const currentY = e.clientY - containerRect.top;

        const left = Math.min(rectangleSelection.startX, currentX);
        const top = Math.min(rectangleSelection.startY, currentY);
        const width = Math.abs(currentX - rectangleSelection.startX);
        const height = Math.abs(currentY - rectangleSelection.startY);

        rectangleSelection.rect.style.left = `${left}px`;
        rectangleSelection.rect.style.top = `${top}px`;
        rectangleSelection.rect.style.width = `${width}px`;
        rectangleSelection.rect.style.height = `${height}px`;
    }, { signal });

    // Complete selection on mouseup
    container.addEventListener('mouseup', (e) => {
        if (!rectangleSelection.active) {
            return;
        }

        // Prevent this from bubbling and triggering the click handler
        e.stopPropagation();
        e.preventDefault();

        if (rectangleSelection.rect) {
            const rectBounds = rectangleSelection.rect.getBoundingClientRect();

            // Find all glyphs that intersect with selection rectangle
            const glyphsInSelection: string[] = [];
            const allGlyphElements = container.querySelectorAll('[data-glyph-id]');

            allGlyphElements.forEach((el) => {
                const glyphEl = el as HTMLElement;
                const glyphId = glyphEl.dataset.glyphId;

                // Skip canvas itself
                if (glyphId === 'canvas-workspace') return;
                if (glyphEl.classList.contains('canvas-workspace')) return;

                // Skip composition containers - we only want individual glyphs
                if (glyphEl.classList.contains('melded-composition')) return;

                // Check if glyph intersects with selection rectangle
                // This includes glyphs within compositions
                const glyphBounds = glyphEl.getBoundingClientRect();
                const intersects =
                    rectBounds.left < glyphBounds.right &&
                    rectBounds.right > glyphBounds.left &&
                    rectBounds.top < glyphBounds.bottom &&
                    rectBounds.bottom > glyphBounds.top;

                if (intersects && glyphId) {
                    log.debug(SEG.GLYPH, '[RectangleSelection] Found intersecting glyph', {
                        glyphId,
                        className: glyphEl.className,
                        rectBounds: {
                            left: rectBounds.left,
                            right: rectBounds.right,
                            top: rectBounds.top,
                            bottom: rectBounds.bottom
                        },
                        glyphBounds: {
                            left: glyphBounds.left,
                            right: glyphBounds.right,
                            top: glyphBounds.top,
                            bottom: glyphBounds.bottom
                        }
                    });
                    glyphsInSelection.push(glyphId);
                }
            });

            log.debug(SEG.GLYPH, '[RectangleSelection] Found glyphs', {
                count: glyphsInSelection.length,
                glyphIds: glyphsInSelection
            });

            // Apply selection
            if (glyphsInSelection.length > 0) {
                log.debug(SEG.GLYPH, '[RectangleSelection] Applying selection', {
                    count: glyphsInSelection.length,
                    glyphIds: glyphsInSelection,
                    mode: rectangleSelection.shiftKey ? 'add' : 'replace'
                });

                if (rectangleSelection.shiftKey) {
                    // Add to existing selection
                    glyphsInSelection.forEach(id => {
                        log.debug(SEG.GLYPH, '[RectangleSelection] Calling selectGlyph (add)', { id });
                        selectGlyph(id, container, true);
                    });
                } else {
                    // Replace selection
                    deselectAll(container);
                    glyphsInSelection.forEach(id => {
                        log.debug(SEG.GLYPH, '[RectangleSelection] Calling selectGlyph (replace)', { id });
                        selectGlyph(id, container, true);
                    });
                }

                log.debug(SEG.GLYPH, '[RectangleSelection] Selection complete');
            } else if (!rectangleSelection.shiftKey) {
                // Empty selection and no shift - deselect all
                deselectAll(container);
                log.debug(SEG.GLYPH, '[RectangleSelection] Deselected all (empty rectangle)');
            }

            // Remove rectangle
            rectangleSelection.rect.remove();
        }

        rectangleSelection.active = false;
        rectangleSelection.rect = null;

        // Set flag to prevent click handler from immediately deselecting
        rectangleSelectionJustCompleted = true;
        setTimeout(() => {
            rectangleSelectionJustCompleted = false;
        }, 100);  // Reset after 100ms
    }, { signal });

    // Cleanup function
    return () => {
        abortController.abort();
        if (rectangleSelection.rect) {
            rectangleSelection.rect.remove();
        }
    };
}
