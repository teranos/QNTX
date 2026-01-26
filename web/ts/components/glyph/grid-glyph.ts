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
 */
export function createGridGlyph(glyph: Glyph): HTMLElement {
    const element = document.createElement('div');
    element.className = 'canvas-grid-glyph';
    element.dataset.glyphId = glyph.id;

    // Get position and symbol from glyph metadata
    const symbol = glyph.symbol || '?';
    let currentGridX = glyph.gridX ?? 5;
    let currentGridY = glyph.gridY ?? 5;

    // Check if glyph has content to render (e.g., IX input form)
    const hasContent = glyph.renderContent !== undefined;

    // Style element
    element.style.position = 'absolute';

    if (hasContent) {
        // Content glyphs are larger and render their content
        element.style.width = 'auto';
        element.style.height = 'auto';
        element.style.display = 'block';

        // Render the content
        const content = glyph.renderContent!();
        element.appendChild(content);
    } else {
        // Symbol-only glyphs are small (40px)
        element.style.width = `${GRID_SIZE}px`;
        element.style.height = `${GRID_SIZE}px`;
        element.style.display = 'flex';
        element.style.alignItems = 'center';
        element.style.justifyContent = 'center';
        element.style.fontSize = '24px';

        // Set symbol content
        element.textContent = symbol;
    }

    element.style.cursor = 'move';
    element.style.userSelect = 'none';
    element.style.backgroundColor = 'var(--bg-secondary)';
    element.style.borderRadius = '4px';
    element.style.border = '1px solid var(--border-color)';

    // Set initial position
    updatePosition(element, currentGridX, currentGridY);

    // Make draggable with grid snapping
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

        // Snap to grid with bounds checking
        const maxGridX = Math.floor(window.innerWidth / GRID_SIZE) - 1;
        const maxGridY = Math.floor(window.innerHeight / GRID_SIZE) - 1;
        const snappedGridX = Math.max(0, Math.min(maxGridX, Math.round(newX / GRID_SIZE)));
        const snappedGridY = Math.max(0, Math.min(maxGridY, Math.round(newY / GRID_SIZE)));

        // Update position
        updatePosition(element, snappedGridX, snappedGridY);
    };

    const handleMouseUp = () => {
        if (!isDragging) return;
        isDragging = false;

        element.style.opacity = '1';
        element.style.zIndex = '';

        // Get final grid position
        const rect = element.getBoundingClientRect();
        currentGridX = Math.round(rect.left / GRID_SIZE);
        currentGridY = Math.round(rect.top / GRID_SIZE);

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

        // Clean up temporary drag listeners
        document.removeEventListener('mousemove', handleMouseMove);
        document.removeEventListener('mouseup', handleMouseUp);
    };

    element.addEventListener('mousedown', (e) => {
        e.preventDefault();
        e.stopPropagation(); // Prevent canvas context menu
        isDragging = true;

        // Record start positions
        dragStartX = e.clientX;
        dragStartY = e.clientY;
        const rect = element.getBoundingClientRect();
        elementStartX = rect.left;
        elementStartY = rect.top;

        element.style.opacity = '0.7';
        element.style.zIndex = '1000';

        // Add document listeners only during drag
        document.addEventListener('mousemove', handleMouseMove);
        document.addEventListener('mouseup', handleMouseUp);

        log.debug(SEG.UI, `[GridGlyph] Started dragging ${glyph.id} from grid (${currentGridX}, ${currentGridY})`);
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
