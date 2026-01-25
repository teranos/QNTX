/**
 * Canvas Glyph - Fractal container with spatial grid layout
 *
 * The canvas is a glyph that morphs to full-screen and contains other glyphs
 * arranged on a spatial grid. Right-click spawns new glyphs.
 *
 * This demonstrates the fractal principle: all glyphs are containers.
 */

import type { Glyph } from './glyph';
import { Pulse } from '@generated/sym.js';
import { log, SEG } from '../../logger';
import { createGridGlyph } from './grid-glyph';
import { uiState } from '../../state/ui';

// Grid configuration
const GRID_SIZE = 40; // pixels per grid cell

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
        onSpawnMenu: () => [Pulse], // Symbols that can be spawned

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

    // Snap menu position to grid
    const gridX = Math.round(mouseX / GRID_SIZE);
    const gridY = Math.round(mouseY / GRID_SIZE);

    // Create spawn menu
    const menu = document.createElement('div');
    menu.className = 'canvas-spawn-menu';
    menu.style.position = 'fixed';
    menu.style.left = `${mouseX}px`;
    menu.style.top = `${mouseY}px`;
    menu.style.zIndex = '10000';

    // Add Pulse symbol
    const pulseBtn = document.createElement('button');
    pulseBtn.className = 'canvas-spawn-button';
    pulseBtn.textContent = Pulse;
    pulseBtn.title = 'Spawn Pulse glyph';

    pulseBtn.addEventListener('click', () => {
        spawnPulseGlyph(gridX, gridY, canvas, glyphs);
        menu.remove();
    });

    menu.appendChild(pulseBtn);
    document.body.appendChild(menu);

    // Close menu on click outside
    const closeMenu = (e: MouseEvent) => {
        if (!menu.contains(e.target as Node)) {
            menu.remove();
            document.removeEventListener('click', closeMenu);
        }
    };
    setTimeout(() => {
        document.addEventListener('click', closeMenu);
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
    const pulseGlyph: Glyph = {
        id: `pulse-${Date.now()}`,
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
 * Render a glyph on the canvas
 */
function renderGlyph(glyph: Glyph): HTMLElement {
    // Render at saved position (or default if not set)
    return createGridGlyph(glyph);
}
