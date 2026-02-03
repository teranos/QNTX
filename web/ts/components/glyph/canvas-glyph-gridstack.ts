/**
 * GridStack Canvas Glyph - Canvas with GridStack-powered melding
 *
 * This is the GridStack-integrated version of the canvas that enables
 * proximity-based glyph melding through grid-based positioning.
 */

import type { Glyph } from './glyph';
import { Pulse, IX, AX, SO } from '@generated/sym.js';
import { log, SEG } from '../../logger';
import { createIxGlyph } from './ix-glyph';
import { createAxGlyph } from './ax-glyph';
import { createPromptGlyph } from './prompt-glyph';
import { createPyGlyph } from './py-glyph';
import { uiState } from '../../state/ui';
import { initializeGridStack, addGlyphToGrid } from './gridstack-melding';
import { prepareGlyphForGrid, injectGridStackStyles } from './glyph-widget-adapter';

/**
 * Factory function to create a GridStack-enabled Canvas glyph
 */
export function createGridStackCanvasGlyph(): Glyph {
    // Load persisted glyphs from uiState
    const savedGlyphs = uiState.getCanvasGlyphs();
    log.debug(SEG.UI, `[GridCanvas] Restoring ${savedGlyphs.length} glyphs from state`);

    const glyphs: Glyph[] = savedGlyphs.map(saved => {
        // Restore glyphs based on their type
        if (saved.symbol === AX) {
            const axGlyph = createAxGlyph(saved.id, '', saved.gridX, saved.gridY);
            axGlyph.width = saved.width;
            axGlyph.height = saved.height;
            return axGlyph;
        }

        if (saved.symbol === SO) {
            return {
                id: saved.id,
                title: 'Prompt',
                symbol: SO,
                gridX: saved.gridX,
                gridY: saved.gridY,
                width: saved.width,
                height: saved.height,
                renderContent: () => {
                    const content = document.createElement('div');
                    content.className = 'prompt-content';
                    content.textContent = 'Prompt template editor';
                    return content;
                }
            };
        }

        return {
            id: saved.id,
            title: saved.symbol === 'result' ? 'Python Result' : 'Glyph',
            symbol: saved.symbol,
            gridX: saved.gridX,
            gridY: saved.gridY,
            width: saved.width,
            height: saved.height,
            result: saved.result,
            renderContent: () => {
                const content = document.createElement('div');
                content.textContent = `${saved.symbol} glyph content`;
                return content;
            }
        };
    });

    return {
        id: 'gridstack-canvas',
        title: 'GridStack Canvas',
        manifestationType: 'fullscreen',
        layoutStrategy: 'grid',
        children: glyphs,
        onSpawnMenu: () => [AX, SO, IX, 'py'],

        renderContent: () => {
            const container = document.createElement('div');
            container.className = 'canvas-workspace gridstack-canvas';
            container.id = 'gridstack-canvas-container';

            // Full-screen styling
            container.style.width = '100%';
            container.style.height = '100%';
            container.style.position = 'relative';
            container.style.overflow = 'hidden';
            container.style.backgroundColor = '#2a2b2a';

            // Inject GridStack styles
            injectGridStackStyles();

            // Initialize GridStack after DOM is ready
            setTimeout(() => {
                // Initialize GridStack on the container
                const grid = initializeGridStack(container);

                // Add existing glyphs to the grid
                glyphs.forEach(glyph => {
                    const glyphContent = glyph.renderContent();
                    const gridWidget = prepareGlyphForGrid(glyph, glyphContent);
                    addGlyphToGrid(glyph, gridWidget);
                });

                log.debug(SEG.UI, '[GridCanvas] GridStack initialized with glyphs');
            }, 0);

            // Right-click handler for spawn menu
            container.addEventListener('contextmenu', (e) => {
                e.preventDefault();
                showGridSpawnMenu(e.clientX, e.clientY, container, glyphs);
            });

            return container;
        }
    };
}

/**
 * Show spawn menu for creating new glyphs
 */
function showGridSpawnMenu(
    mouseX: number,
    mouseY: number,
    canvas: HTMLElement,
    glyphs: Glyph[]
): void {
    // Remove existing menu
    const existingMenu = document.querySelector('.gridstack-spawn-menu');
    if (existingMenu) {
        existingMenu.remove();
    }

    // Create spawn menu
    const menu = document.createElement('div');
    menu.className = 'gridstack-spawn-menu';
    menu.style.cssText = `
        position: fixed;
        left: ${mouseX}px;
        top: ${mouseY}px;
        background: rgba(40, 40, 40, 0.95);
        border: 1px solid rgba(255, 255, 255, 0.2);
        border-radius: 8px;
        padding: 8px;
        display: flex;
        gap: 8px;
        z-index: 10000;
        box-shadow: 0 4px 16px rgba(0, 0, 0, 0.4);
    `;

    const symbols = [
        { symbol: AX, name: 'Ax Query', factory: createAxGlyph },
        { symbol: SO, name: 'Prompt', factory: createPromptGlyph },
        { symbol: IX, name: 'IX Transform', factory: createIxGlyph },
        { symbol: 'py', name: 'Python', factory: createPyGlyph }
    ];

    symbols.forEach(({ symbol, name, factory }) => {
        const btn = document.createElement('button');
        btn.className = 'gridstack-spawn-button';
        btn.style.cssText = `
            background: rgba(60, 60, 60, 0.8);
            border: 1px solid rgba(255, 255, 255, 0.1);
            color: rgba(255, 255, 255, 0.9);
            padding: 8px 12px;
            border-radius: 4px;
            cursor: pointer;
            font-size: 14px;
            transition: all 0.2s;
        `;
        btn.textContent = typeof symbol === 'string' ? symbol : name;
        btn.title = `Spawn ${name}`;

        btn.addEventListener('mouseenter', () => {
            btn.style.background = 'rgba(80, 80, 80, 0.9)';
            btn.style.transform = 'scale(1.05)';
        });

        btn.addEventListener('mouseleave', () => {
            btn.style.background = 'rgba(60, 60, 60, 0.8)';
            btn.style.transform = 'scale(1)';
        });

        btn.addEventListener('click', async () => {
            await spawnGridGlyph(symbol, name, factory, mouseX, mouseY, canvas, glyphs);
            menu.remove();
        });

        menu.appendChild(btn);
    });

    document.body.appendChild(menu);

    // Close menu on click outside
    setTimeout(() => {
        const closeMenu = (e: MouseEvent) => {
            if (!menu.contains(e.target as Node)) {
                menu.remove();
                document.removeEventListener('click', closeMenu);
            }
        };
        document.addEventListener('click', closeMenu);
    }, 0);

    log.debug(SEG.UI, '[GridCanvas] Spawn menu opened', { x: mouseX, y: mouseY });
}

/**
 * Spawn a new glyph on the GridStack canvas
 */
async function spawnGridGlyph(
    symbol: string,
    name: string,
    factory: Function,
    mouseX: number,
    mouseY: number,
    canvas: HTMLElement,
    glyphs: Glyph[]
): Promise<void> {
    // Calculate grid position from mouse coordinates
    const gridX = Math.floor(mouseX / 20); // Approximate grid cell
    const gridY = Math.floor(mouseY / 20);

    let glyph: Glyph;

    if (symbol === AX) {
        glyph = factory(undefined, '', gridX, gridY) as Glyph;
    } else {
        glyph = {
            id: `${symbol}-${crypto.randomUUID()}`,
            title: name,
            symbol: symbol,
            gridX,
            gridY,
            renderContent: () => {
                const content = document.createElement('div');
                content.textContent = `${name} content`;
                return content;
            }
        };
    }

    glyphs.push(glyph);

    // Prepare glyph for grid
    let glyphElement: HTMLElement;

    if (symbol === AX) {
        glyphElement = glyph.renderContent();
    } else if (symbol === SO) {
        glyphElement = await createPromptGlyph(glyph);
    } else if (symbol === IX) {
        glyphElement = await createIxGlyph(glyph);
    } else if (symbol === 'py') {
        glyphElement = await createPyGlyph(glyph);
    } else {
        glyphElement = glyph.renderContent();
    }

    const gridWidget = prepareGlyphForGrid(glyph, glyphElement);
    addGlyphToGrid(glyph, gridWidget);

    // Save to state
    const rect = gridWidget.getBoundingClientRect();
    uiState.addCanvasGlyph({
        id: glyph.id,
        symbol: symbol,
        gridX,
        gridY,
        width: Math.round(rect.width),
        height: Math.round(rect.height)
    });

    log.debug(SEG.UI, '[GridCanvas] Spawned glyph', {
        id: glyph.id,
        symbol,
        position: { gridX, gridY }
    });
}

/**
 * Export a function to switch the canvas to GridStack mode
 */
export function enableGridStackCanvas(): void {
    log.info(SEG.UI, '[GridCanvas] Enabling GridStack canvas mode');

    // Find existing canvas
    const existingCanvas = document.querySelector('.canvas-workspace');
    if (existingCanvas) {
        const parent = existingCanvas.parentElement;
        if (parent) {
            // Create new GridStack canvas
            const gridCanvas = createGridStackCanvasGlyph();
            const newCanvas = gridCanvas.renderContent();

            // Replace old canvas with new one
            parent.replaceChild(newCanvas, existingCanvas);

            log.info(SEG.UI, '[GridCanvas] Canvas replaced with GridStack version');
        }
    }
}