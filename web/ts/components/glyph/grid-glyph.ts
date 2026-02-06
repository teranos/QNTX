/**
 * Grid Glyph - Visual representation of a glyph on canvas
 *
 * Renders a symbol at a pixel position, freely draggable.
 */

import type { Glyph } from './glyph';
import { log, SEG } from '../../logger';
import { uiState } from '../../state/ui';
import { GRID_SIZE } from './grid-constants';

/**
 * Create a canvas glyph element
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
    let currentX = glyph.x ?? 200;
    let currentY = glyph.y ?? 200;

    // Style element - all canvas glyphs are symbol-only (40px squares)
    element.style.position = 'absolute';
    element.style.left = `${currentX}px`;
    element.style.top = `${currentY}px`;
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

    // Make draggable with free-form positioning
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

        // Update position directly
        element.style.left = `${newX}px`;
        element.style.top = `${newY}px`;
    };

    let abortController: AbortController | null = null;

    const handleMouseUp = () => {
        if (!isDragging) return;
        isDragging = false;

        element.classList.remove('is-dragging');

        // Get final pixel position (relative to canvas parent)
        const canvas = element.parentElement;
        const canvasRect = canvas?.getBoundingClientRect() ?? { left: 0, top: 0 };
        const elementRect = element.getBoundingClientRect();
        currentX = elementRect.left - canvasRect.left;
        currentY = elementRect.top - canvasRect.top;

        // Save position to glyph metadata
        glyph.x = currentX;
        glyph.y = currentY;

        // Persist to uiState
        if (glyph.symbol) {
            uiState.addCanvasGlyph({
                id: glyph.id,
                symbol: glyph.symbol,
                x: currentX,
                y: currentY
            });
        }

        log.debug(SEG.UI, `[GridGlyph] Finished dragging ${glyph.id} to (${currentX}, ${currentY})`);

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

        log.debug(SEG.UI, `[GridGlyph] Started dragging ${glyph.id} from (${currentX}, ${currentY})`);
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
