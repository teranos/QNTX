/**
 * Grid Glyph - Visual representation of a glyph on canvas grid
 *
 * Renders a symbol at a grid position, draggable with grid snapping.
 * Clicking (without dragging) opens the glyph's manifestation.
 */

import type { Glyph } from './glyph';
import { log, SEG } from '../../logger';
import { uiState } from '../../state/ui';
import { GRID_SIZE } from './grid-constants';
import { glyphRun } from './run';

// Threshold in pixels - if mouse moves less than this, it's a click not a drag
const CLICK_THRESHOLD = 5;

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

    // Make draggable with grid snapping
    let isDragging = false;
    let dragStartX = 0;
    let dragStartY = 0;
    let elementStartX = 0;
    let elementStartY = 0;
    let totalMovement = 0; // Track total movement to distinguish click from drag

    // Event handlers that need cleanup
    const handleMouseMove = (e: MouseEvent) => {
        if (!isDragging) return;

        // Calculate new pixel position
        const deltaX = e.clientX - dragStartX;
        const deltaY = e.clientY - dragStartY;

        // Track total movement for click detection
        totalMovement = Math.abs(deltaX) + Math.abs(deltaY);

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

        // Clean up temporary drag listeners first
        document.removeEventListener('mousemove', handleMouseMove);
        document.removeEventListener('mouseup', handleMouseUp);

        // Check if this was a click (minimal movement) vs a drag
        if (totalMovement < CLICK_THRESHOLD) {
            log.debug(SEG.UI, `[GridGlyph] Click detected on ${glyph.id}, opening manifestation`);
            openManifestation(glyph);
            return;
        }

        // It was a drag - save the new position
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
    };

    element.addEventListener('mousedown', (e) => {
        e.preventDefault();
        e.stopPropagation(); // Prevent canvas context menu
        isDragging = true;
        totalMovement = 0; // Reset movement tracking

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

        log.debug(SEG.UI, `[GridGlyph] Started interaction with ${glyph.id} at grid (${currentGridX}, ${currentGridY})`);
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

/**
 * Open a glyph's manifestation
 * Creates a new glyph in the tray system and morphs it to its manifestation type
 *
 * Canvas glyphs are launch points - clicking them opens a manifestation window.
 * The canvas glyph persists while the manifestation can be opened/closed/minimized.
 */
function openManifestation(glyph: Glyph): void {
    // Only IX glyphs have manifestations currently
    if (glyph.manifestationType !== 'ix') {
        log.debug(SEG.UI, `[GridGlyph] No manifestation for ${glyph.id} (type: ${glyph.manifestationType})`);
        return;
    }

    // Create a manifestation glyph ID based on the canvas glyph
    // This links the manifestation back to its canvas source
    const manifestationId = `${glyph.id}-manifestation`;

    // Check if this manifestation is already open
    if (glyphRun.has(manifestationId)) {
        log.debug(SEG.UI, `[GridGlyph] Manifestation ${manifestationId} already exists`);
        // TODO: Focus the existing manifestation window
        return;
    }

    // Create a new glyph for the manifestation
    const manifestationGlyph: Glyph = {
        id: manifestationId,
        title: glyph.title,
        symbol: glyph.symbol,
        manifestationType: 'ix',
        renderContent: glyph.renderContent
    };

    // Add to tray (this creates the element via the factory)
    glyphRun.add(manifestationGlyph);

    // The glyph is now in the tray as a dot - we need to trigger a click to morph it
    // Use a small delay to ensure the element is in the DOM
    setTimeout(() => {
        const element = document.querySelector(`[data-glyph-id="${manifestationId}"]`) as HTMLElement;
        if (element) {
            element.click();
        }
    }, 0);

    log.debug(SEG.UI, `[GridGlyph] Opened manifestation for ${glyph.id}`);
}
