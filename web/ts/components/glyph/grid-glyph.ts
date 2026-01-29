/**
 * Grid Glyph - Visual representation of a glyph on canvas grid
 *
 * Renders a symbol at a grid position, draggable with grid snapping.
 */

import type { Glyph } from './glyph';
import { log, SEG } from '../../logger';
import { uiState } from '../../state/ui';
import { GRID_SIZE } from './grid-constants';

/**
 * Create a grid-positioned glyph element
 * Canvas glyphs are lightweight references (symbols only)
 * Clicking them morphs to their full manifestation
 *
 * TODO: Future enhancements for canvas glyphs:
 * - Status indicators (active/inactive, running, error states)
 * - Badge overlays (e.g., count of incoming attestations for IX)
 * - Visual feedback on state changes
 * - Context menu on right-click (edit, delete, duplicate)
 */
export function createGridGlyph(glyph: Glyph): HTMLElement {
    const element = document.createElement('div');
    element.className = 'canvas-grid-glyph';
    element.dataset.glyphId = glyph.id;

    // Get position and symbol from glyph metadata
    const symbol = glyph.symbol || '?';
    let currentGridX = glyph.gridX ?? 5;
    let currentGridY = glyph.gridY ?? 5;

    // Style element - all canvas glyphs are symbol-only (40px squares)
    element.style.position = 'absolute';
    element.style.width = `${GRID_SIZE}px`;
    element.style.height = `${GRID_SIZE}px`;
    element.style.display = 'flex';
    element.style.alignItems = 'center';
    element.style.justifyContent = 'center';
    element.style.fontSize = '24px';
    element.style.cursor = 'move';
    element.style.userSelect = 'none';
    element.style.backgroundColor = 'var(--bg-secondary)';
    element.style.borderRadius = '4px';
    element.style.border = '1px solid var(--border-color)';

    // Set symbol content
    element.textContent = symbol;

    // Set initial position
    updatePosition(element, currentGridX, currentGridY);

    // Make draggable with free-form positioning (no live grid snapping)
    // Design decision: Free-form dragging provides better UX than grid-snapped dragging
    let isDragging = false;
    let dragStartX = 0;
    let dragStartY = 0;
    let elementStartX = 0;
    let elementStartY = 0;

    // Event handlers that need cleanup
    const handleMouseMove = (e: MouseEvent) => {
        if (!isDragging) return;

        // Calculate new pixel position
        const deltaX = e.clientX - dragStartX;
        const deltaY = e.clientY - dragStartY;
        const newX = elementStartX + deltaX;
        const newY = elementStartY + deltaY;

        // Track drag distance to distinguish clicks from drags
        dragDistance = Math.abs(deltaX) + Math.abs(deltaY);

        // Update position directly (free-form, no grid snapping during drag)
        element.style.left = `${newX}px`;
        element.style.top = `${newY}px`;
    };

    let abortController: AbortController | null = null;

    const handleMouseUp = () => {
        if (!isDragging) return;
        isDragging = false;

        element.classList.remove('is-dragging');

        // Get final grid position (calculate relative to canvas parent)
        const canvas = element.parentElement;
        const canvasRect = canvas?.getBoundingClientRect() ?? { left: 0, top: 0 };
        const elementRect = element.getBoundingClientRect();
        currentGridX = Math.round((elementRect.left - canvasRect.left) / GRID_SIZE);
        currentGridY = Math.round((elementRect.top - canvasRect.top) / GRID_SIZE);

        // Save position to glyph metadata
        glyph.gridX = currentGridX;
        glyph.gridY = currentGridY;

        // Persist to uiState
        // TODO: Add error handling for state persistence failures
        if (glyph.symbol) {
            uiState.addCanvasGlyph({
                id: glyph.id,
                symbol: glyph.symbol,
                gridX: currentGridX,
                gridY: currentGridY
            });
        }

        log.debug(SEG.UI, `[GridGlyph] Finished dragging ${glyph.id} to grid (${currentGridX}, ${currentGridY})`);

        abortController?.abort();
        abortController = null;
    };

    let dragDistance = 0;

    element.addEventListener('mousedown', (e) => {
        e.preventDefault();
        e.stopPropagation(); // Prevent canvas context menu
        isDragging = true;
        dragDistance = 0;

        // Record start positions
        dragStartX = e.clientX;
        dragStartY = e.clientY;
        const rect = element.getBoundingClientRect();
        elementStartX = rect.left;
        elementStartY = rect.top;

        element.classList.add('is-dragging');

        abortController = new AbortController();
        document.addEventListener('mousemove', handleMouseMove, { signal: abortController.signal });
        document.addEventListener('mouseup', handleMouseUp, { signal: abortController.signal });

        log.debug(SEG.UI, `[GridGlyph] Started dragging ${glyph.id} from grid (${currentGridX}, ${currentGridY})`);
    });

    // Detect clicks vs drags - if mouse hasn't moved much, it's a click
    element.addEventListener('click', () => {
        // Don't stopPropagation — let click bubble for canvas selection handling
        // Only trigger click if this wasn't a drag
        if (dragDistance < 5) {
            log.debug(SEG.UI, `[GridGlyph] Clicked ${glyph.id}, triggering manifestation`);

            // Dispatch custom event that canvas can listen for
            const clickEvent = new CustomEvent('glyph-click', {
                detail: { glyph },
                bubbles: true
            });
            element.dispatchEvent(clickEvent);
        }
    });

    return element;
}

/**
 * Update element position from grid coordinates
 */
function updatePosition(element: HTMLElement, gridX: number, gridY: number): void {
    element.style.left = `${gridX * GRID_SIZE}px`;
    element.style.top = `${gridY * GRID_SIZE}px`;
}
