/**
 * Canvas Glyph - Fractal container with spatial grid layout
 *
 * The canvas is a glyph that morphs to full-screen and contains other glyphs
 * arranged on a spatial grid. Right-click spawns new glyphs.
 *
 * This demonstrates the fractal principle: all glyphs are containers.
 */

import type { Glyph } from './glyph';
import { Pulse, IX } from '@generated/sym.js';
import { log, SEG } from '../../logger';
import { createGridGlyph } from './grid-glyph';
import { createIxGlyph } from './ix-glyph';
import { morphToIx } from './manifestations/ix';
import { uiState } from '../../state/ui';
import { GRID_SIZE } from './grid-constants';

/**
 * Factory function to create a Canvas glyph
 */
export function createCanvasGlyph(): Glyph {
    // Load persisted glyphs from uiState
    const savedGlyphs = uiState.getCanvasGlyphs();
    const glyphs: Glyph[] = savedGlyphs.map(saved => ({
        id: saved.id,
        title: 'Pulse Schedule',
        symbol: saved.symbol,
        gridX: saved.gridX,
        gridY: saved.gridY,
        // TODO: Clarify if grid glyphs should display content
        renderContent: () => {
            const content = document.createElement('div');
            content.textContent = 'Pulse glyph content (TBD)';
            return content;
        }
    }));

    return {
        id: 'canvas-workspace',
        title: 'Canvas',
        manifestationType: 'fullscreen', // Full-viewport, no chrome
        layoutStrategy: 'grid',
        children: glyphs,
        onSpawnMenu: () => [Pulse, IX], // Symbols that can be spawned

        renderContent: () => {
            const container = document.createElement('div');
            container.className = 'canvas-workspace';

            // Full-screen, no padding
            container.style.width = '100%';
            container.style.height = '100%';
            container.style.position = 'relative';
            container.style.overflow = 'hidden';
            container.style.backgroundColor = 'var(--bg-primary)';

            // Add subtle grid overlay
            const gridOverlay = document.createElement('div');
            gridOverlay.className = 'canvas-grid-overlay';
            container.appendChild(gridOverlay);

            // Right-click handler for spawn menu
            container.addEventListener('contextmenu', (e) => {
                e.preventDefault();
                showSpawnMenu(e.clientX, e.clientY, container, glyphs);
            });

            // Click handler for grid glyphs - trigger manifestation
            container.addEventListener('glyph-click', ((e: CustomEvent) => {
                const glyph = e.detail.glyph as Glyph;
                const glyphElement = e.target as HTMLElement;

                log.debug(SEG.UI, `[Canvas] Glyph clicked: ${glyph.id}, manifestationType: ${glyph.manifestationType}`);

                // Route to correct manifestation
                if (glyph.manifestationType === 'ix') {
                    morphToIx(
                        glyphElement,
                        glyph,
                        (id, element) => {
                            // Verify element (simplified - canvas glyphs aren't tracked)
                            if (element.dataset.glyphId !== id) {
                                throw new Error(`Element mismatch: expected ${id}, got ${element.dataset.glyphId}`);
                            }
                        },
                        (id) => {
                            // Remove glyph (simplified - just remove from array)
                            const index = glyphs.findIndex(g => g.id === id);
                            if (index !== -1) {
                                glyphs.splice(index, 1);
                            }
                            uiState.removeCanvasGlyph(id);
                        },
                        (_element, g) => {
                            // Reattach to canvas (simplified - not implemented for canvas glyphs yet)
                            // TODO: When minimizing from IX manifestation, should it go to tray or back to canvas?
                            log.debug(SEG.UI, `[Canvas] IX glyph ${g.id} minimized - not re-adding to canvas`);
                        }
                    );
                }
                // TODO: Handle other manifestation types (Pulse, etc.) when implemented
            }) as EventListener);

            // Render existing glyphs
            glyphs.forEach(glyph => {
                const glyphElement = renderGlyph(glyph);
                container.appendChild(glyphElement);
            });

            return container;
        }
    };
}

/**
 * Show right-click spawn menu with available symbols
 * TODO: Spawn menu as container glyph, menu items as glyphs
 */
function showSpawnMenu(
    mouseX: number,
    mouseY: number,
    canvas: HTMLElement,
    glyphs: Glyph[]
): void {
    // Remove any existing menu
    const existingMenu = document.querySelector('.canvas-spawn-menu');
    if (existingMenu) {
        existingMenu.remove();
    }

    // Snap menu position to grid with bounds checking
    const maxGridX = Math.floor(window.innerWidth / GRID_SIZE) - 1;
    const maxGridY = Math.floor(window.innerHeight / GRID_SIZE) - 1;
    const gridX = Math.max(0, Math.min(maxGridX, Math.round(mouseX / GRID_SIZE)));
    const gridY = Math.max(0, Math.min(maxGridY, Math.round(mouseY / GRID_SIZE)));

    // Create spawn menu
    const menu = document.createElement('div');
    menu.className = 'canvas-spawn-menu';
    menu.style.position = 'fixed';
    menu.style.left = `${mouseX}px`;
    menu.style.top = `${mouseY}px`;
    menu.style.zIndex = '10000';

    // Close menu on click outside (with cleanup flag to prevent memory leak)
    let menuRemoved = false;
    const removeMenu = () => {
        menu.remove();
        menuRemoved = true;
    };

    // Add Pulse symbol
    const pulseBtn = document.createElement('button');
    pulseBtn.className = 'canvas-spawn-button';
    pulseBtn.textContent = Pulse;
    pulseBtn.title = 'Spawn Pulse glyph';

    pulseBtn.addEventListener('click', () => {
        spawnPulseGlyph(gridX, gridY, canvas, glyphs);
        removeMenu();
    });

    menu.appendChild(pulseBtn);

    // Add IX symbol
    const ixBtn = document.createElement('button');
    ixBtn.className = 'canvas-spawn-button';
    ixBtn.textContent = IX;
    ixBtn.title = 'Spawn IX glyph';

    ixBtn.addEventListener('click', () => {
        spawnIxGlyph(gridX, gridY, canvas, glyphs);
        removeMenu();
    });

    menu.appendChild(ixBtn);

    document.body.appendChild(menu);

    // Close menu on click outside
    const closeMenu = (e: MouseEvent) => {
        if (!menu.contains(e.target as Node)) {
            removeMenu();
            document.removeEventListener('click', closeMenu);
        }
    };
    setTimeout(() => {
        // Only attach listener if menu hasn't been removed synchronously
        if (!menuRemoved) {
            document.addEventListener('click', closeMenu);
        }
    }, 0);

    log.debug(SEG.UI, `[Canvas] Spawn menu opened at grid (${gridX}, ${gridY})`);
}

/**
 * Spawn a new Pulse glyph at grid position
 */
function spawnPulseGlyph(
    gridX: number,
    gridY: number,
    canvas: HTMLElement,
    glyphs: Glyph[]
): void {
    // NOTE: Using crypto.randomUUID() for now to ensure uniqueness.
    // Future: integrate vanity-id generator as Glyph vision expands.
    const pulseGlyph: Glyph = {
        id: `pulse-${crypto.randomUUID()}`,
        title: 'Pulse Schedule',
        symbol: Pulse,
        gridX,
        gridY,
        renderContent: () => {
            const content = document.createElement('div');
            content.textContent = 'Pulse glyph content (TBD)';
            return content;
        }
    };

    // Add to glyphs array
    glyphs.push(pulseGlyph);

    // Persist to uiState
    uiState.addCanvasGlyph({
        id: pulseGlyph.id,
        symbol: Pulse,
        gridX,
        gridY
    });

    // Render glyph on canvas
    const glyphElement = createGridGlyph(pulseGlyph);
    canvas.appendChild(glyphElement);

    log.debug(SEG.UI, `[Canvas] Spawned Pulse glyph at grid (${gridX}, ${gridY})`);
}

/**
 * Spawn a new IX glyph at grid position
 */
function spawnIxGlyph(
    gridX: number,
    gridY: number,
    canvas: HTMLElement,
    glyphs: Glyph[]
): void {
    const ixGlyph = createIxGlyph(gridX, gridY);

    // Add to glyphs array
    glyphs.push(ixGlyph);

    // Persist to uiState
    uiState.addCanvasGlyph({
        id: ixGlyph.id,
        symbol: IX,
        gridX,
        gridY
    });

    // Render glyph on canvas
    const glyphElement = createGridGlyph(ixGlyph);
    canvas.appendChild(glyphElement);

    log.debug(SEG.UI, `[Canvas] Spawned IX glyph at grid (${gridX}, ${gridY})`);
}

/**
 * Render a glyph on the canvas
 */
function renderGlyph(glyph: Glyph): HTMLElement {
    // Render at saved position (or default if not set)
    return createGridGlyph(glyph);
}
