/**
 * Rectangle Selection for Canvas
 *
 * Allows dragging on canvas background to create a selection rectangle
 * that selects individual elements (including elements within compositions).
 *
 * Behavior:
 * - Plain drag: Replace current selection
 * - Shift+drag: Add to current selection
 * - Only activates on canvas background (not on elements)
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
 * @param selectElement - Function to select an element by ID
 * @param deselectAll - Function to deselect all elements
 * @returns Cleanup function to remove event listeners
 */
export function setupRectangleSelection(
    container: HTMLElement,
    selectElement: (itemId: string, container: HTMLElement, addToSelection: boolean) => void,
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

        // Only start rectangle selection on canvas background (not on elements)
        // Scope check to this workspace: element ancestors outside the container
        // (e.g. parent subcanvas element) are ignored
        const elementEl = target.closest('[data-element-id]') as HTMLElement | null;
        if (elementEl && elementEl !== container && container.contains(elementEl)) {
            return;
        }

        // Allow rectangle selection to start on the container or its content layer
        const isCanvasChild = target === container ||
                              target.classList.contains('canvas-content-layer');

        if (!isCanvasChild) {
            return;
        }

        log.debug(SEG.ELEMENT, '[RectangleSelection] Starting selection', {
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

        log.debug(SEG.ELEMENT, '[RectangleSelection] Started', {
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

            // Find all elements that intersect with selection rectangle
            const elementsInSelection: string[] = [];
            const allElementElements = container.querySelectorAll('[data-element-id]');

            allElementElements.forEach((el) => {
                const elementEl = el as HTMLElement;
                const itemId = elementEl.dataset.elementId;

                // Skip canvas itself
                if (itemId === 'canvas-workspace') return;
                if (elementEl.classList.contains('canvas-workspace')) return;

                // Skip composition containers - we only want individual elements
                if (elementEl.classList.contains('melded-composition')) return;

                // Check if element intersects with selection rectangle
                // This includes elements within compositions
                const elementBounds = elementEl.getBoundingClientRect();
                const intersects =
                    rectBounds.left < elementBounds.right &&
                    rectBounds.right > elementBounds.left &&
                    rectBounds.top < elementBounds.bottom &&
                    rectBounds.bottom > elementBounds.top;

                if (intersects && itemId) {
                    log.debug(SEG.ELEMENT, '[RectangleSelection] Found intersecting element', {
                        itemId,
                        className: elementEl.className,
                        rectBounds: {
                            left: rectBounds.left,
                            right: rectBounds.right,
                            top: rectBounds.top,
                            bottom: rectBounds.bottom
                        },
                        elementBounds: {
                            left: elementBounds.left,
                            right: elementBounds.right,
                            top: elementBounds.top,
                            bottom: elementBounds.bottom
                        }
                    });
                    elementsInSelection.push(itemId);
                }
            });

            log.debug(SEG.ELEMENT, '[RectangleSelection] Found elements', {
                count: elementsInSelection.length,
                elementIds: elementsInSelection
            });

            // Apply selection
            if (elementsInSelection.length > 0) {
                log.debug(SEG.ELEMENT, '[RectangleSelection] Applying selection', {
                    count: elementsInSelection.length,
                    elementIds: elementsInSelection,
                    mode: rectangleSelection.shiftKey ? 'add' : 'replace'
                });

                if (rectangleSelection.shiftKey) {
                    // Add to existing selection
                    elementsInSelection.forEach(id => {
                        log.debug(SEG.ELEMENT, '[RectangleSelection] Calling selectElement (add)', { id });
                        selectElement(id, container, true);
                    });
                } else {
                    // Replace selection
                    deselectAll(container);
                    elementsInSelection.forEach(id => {
                        log.debug(SEG.ELEMENT, '[RectangleSelection] Calling selectElement (replace)', { id });
                        selectElement(id, container, true);
                    });
                }

                log.debug(SEG.ELEMENT, '[RectangleSelection] Selection complete');
            } else if (!rectangleSelection.shiftKey) {
                // Empty selection and no shift - deselect all
                deselectAll(container);
                log.debug(SEG.ELEMENT, '[RectangleSelection] Deselected all (empty rectangle)');
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
