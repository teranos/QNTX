/**
 * Canvas Glyph - Fractal container with spatial grid layout
 *
 * The canvas is a glyph that morphs to full-screen and contains other glyphs
 * arranged on a spatial grid. Right-click spawns new glyphs.
 *
 * This demonstrates the fractal principle: all glyphs are containers.
 */

import type { Glyph } from './glyph';
import { IX, SO, Prose } from '@generated/sym.js';
import { log, SEG } from '../../logger';
import { createGridGlyph } from './grid-glyph';
import { createIxGlyph } from './ix-glyph';
import { createPromptGlyph } from './prompt-glyph';
import { createPyGlyph } from './py-glyph';
import { createResultGlyph, type ExecutionResult } from './result-glyph';
import { uiState } from '../../state/ui';
import { GRID_SIZE } from './grid-constants';

/**
 * Factory function to create a Canvas glyph
 */
export function createCanvasGlyph(): Glyph {
    // Load persisted glyphs from uiState
    const savedGlyphs = uiState.getCanvasGlyphs();
    log.debug(SEG.UI, `[Canvas] Restoring ${savedGlyphs.length} glyphs from state`);

    const glyphs: Glyph[] = savedGlyphs.map(saved => {
        if (saved.symbol === 'result') {
            log.debug(SEG.UI, `[Canvas] Restoring result glyph ${saved.id}`, {
                hasResult: !!saved.result,
                gridX: saved.gridX,
                gridY: saved.gridY
            });
        }

        return {
            id: saved.id,
            title: saved.symbol === 'result' ? 'Python Result' : 'Pulse Schedule',
            symbol: saved.symbol,
            gridX: saved.gridX,
            gridY: saved.gridY,
            width: saved.width,   // Restore custom size if saved
            height: saved.height,
            result: saved.result, // For result glyphs
            // TODO: Clarify if grid glyphs should display content
            renderContent: () => {
                const content = document.createElement('div');
                content.textContent = 'Pulse glyph content (TBD)';
                return content;
            }
        };
    });

    return {
        id: 'canvas-workspace',
        title: 'Canvas',
        manifestationType: 'fullscreen', // Full-viewport, no chrome
        layoutStrategy: 'grid',
        children: glyphs,
        onSpawnMenu: () => [IX], // IX and py available, can add go/rs/ts later

        renderContent: () => {
            const container = document.createElement('div');
            container.className = 'canvas-workspace';

            // Full-screen, no padding
            container.style.width = '100%';
            container.style.height = '100%';
            container.style.position = 'relative';
            container.style.overflow = 'hidden';
            container.style.backgroundColor = '#2a2b2a'; // Mid-dark gray for night work

            // Add subtle grid overlay
            const gridOverlay = document.createElement('div');
            gridOverlay.className = 'canvas-grid-overlay';
            container.appendChild(gridOverlay);

            // Right-click handler for spawn menu
            container.addEventListener('contextmenu', (e) => {
                e.preventDefault();
                showSpawnMenu(e.clientX, e.clientY, container, glyphs);
            });

            // Render existing glyphs asynchronously (to support py and ix glyphs)
            (async () => {
                for (const glyph of glyphs) {
                    const glyphElement = await renderGlyph(glyph);
                    container.appendChild(glyphElement);
                }
            })();

            return container;
        }
    };
}

/**
 * Show right-click spawn menu with available symbols
 *
 * Available glyphs: IX (ingest), py (python editor)
 * Future: go, rs, ts programmature glyphs
 *
 * Architecture Note:
 * - Pulse glyph removed - IX glyphs now use forceTriggerJob() for execution
 * - Pulse (scheduling system) remains the execution layer for both IX and ATS
 * - Execution paths:
 *   - One-time execution: IX glyphs on canvas → forceTriggerJob() → Pulse
 *   - Scheduled execution: ATS blocks in Prose → createScheduledJob() → Pulse
 *
 * TODO: Spawn menu as glyph with morphing mini-glyphs
 *
 * Vision: Menu container is a glyph, menu items are tiny glyphs (8px) that use
 * proximity morphing like GlyphRun. As mouse approaches, glyphs morph larger and
 * reveal labels. Clicking a morphed glyph spawns that type on canvas.
 *
 * Implementation:
 * - Menu container: Glyph entity with renderContent
 * - Menu items: Array of tiny glyphs with symbols (IX, "py", "go", "rs", "ts")
 * - Reuse GlyphRun proximity morphing logic (window-tray.ts:164-285)
 * - Priority: Medium (after core window↔glyph morphing works)
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

    // TODO: Refactor spawn menu to be data-driven
    // Loop over available symbols (Pulse, py, go, rs, ts) instead of hardcoding buttons
    // This will make it easier to add new programmature types (go, rs, ts)

    // Add py button
    const pyBtn = document.createElement('button');
    pyBtn.className = 'canvas-spawn-button';
    pyBtn.textContent = 'py';
    pyBtn.title = 'Spawn Python glyph';

    pyBtn.addEventListener('click', () => {
        spawnPyGlyph(gridX, gridY, canvas, glyphs);
        removeMenu();
    });

    menu.appendChild(pyBtn);

    // Add prompt button
    const promptBtn = document.createElement('button');
    promptBtn.className = 'canvas-spawn-button';
    promptBtn.textContent = SO;
    promptBtn.title = 'Spawn Prompt glyph';

    promptBtn.addEventListener('click', () => {
        spawnPromptGlyph(gridX, gridY, canvas, glyphs);
        removeMenu();
    });

    menu.appendChild(promptBtn);

    // Add prose button
    const proseBtn = document.createElement('button');
    proseBtn.className = 'canvas-spawn-button';
    proseBtn.textContent = Prose;
    proseBtn.title = 'Spawn Prose glyph';

    proseBtn.addEventListener('click', () => {
        spawnProseGlyph(gridX, gridY, canvas, glyphs);
        removeMenu();
    });

    menu.appendChild(proseBtn);
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
 * Spawn a new IX glyph at grid position
 */
async function spawnIxGlyph(
    gridX: number,
    gridY: number,
    canvas: HTMLElement,
    glyphs: Glyph[]
): Promise<void> {
    const ixGlyph: Glyph = {
        id: `ix-${crypto.randomUUID()}`,
        title: 'Ingest',
        symbol: IX,
        gridX,
        gridY,
        renderContent: () => {
            const content = document.createElement('div');
            content.textContent = 'IX glyph';
            return content;
        }
    };

    // Add to glyphs array
    glyphs.push(ixGlyph);

    // Render IX glyph with form
    const glyphElement = await createIxGlyph(ixGlyph);
    canvas.appendChild(glyphElement);

    // Get actual rendered size and persist
    const rect = glyphElement.getBoundingClientRect();
    const width = Math.round(rect.width);
    const height = Math.round(rect.height);

    uiState.addCanvasGlyph({
        id: ixGlyph.id,
        symbol: IX,
        gridX,
        gridY,
        width,
        height
    });

    log.debug(SEG.UI, `[Canvas] Spawned IX glyph at grid (${gridX}, ${gridY}) with size ${width}x${height}`);
}

/**
 * Spawn a new Python glyph at grid position
 */
async function spawnPyGlyph(
    gridX: number,
    gridY: number,
    canvas: HTMLElement,
    glyphs: Glyph[]
): Promise<void> {
    const pyGlyph: Glyph = {
        id: `py-${crypto.randomUUID()}`,
        title: 'Python',
        symbol: 'py',
        gridX,
        gridY,
        renderContent: () => {
            const content = document.createElement('div');
            content.textContent = 'Python glyph (TBD)';
            return content;
        }
    };

    // Add to glyphs array
    glyphs.push(pyGlyph);

    // Render Python editor glyph
    const glyphElement = await createPyGlyph(pyGlyph);
    canvas.appendChild(glyphElement);

    // Get actual rendered size and persist (ensures default size is saved)
    const rect = glyphElement.getBoundingClientRect();
    const width = Math.round(rect.width);
    const height = Math.round(rect.height);

    uiState.addCanvasGlyph({
        id: pyGlyph.id,
        symbol: 'py',
        gridX,
        gridY,
        width,
        height
    });

    log.debug(SEG.UI, `[Canvas] Spawned Python glyph at grid (${gridX}, ${gridY}) with size ${width}x${height}`);
}

/**
 * Spawn a new Prompt glyph at grid position
 */
async function spawnPromptGlyph(
    gridX: number,
    gridY: number,
    canvas: HTMLElement,
    glyphs: Glyph[]
): Promise<void> {
    const promptGlyph: Glyph = {
        id: `prompt-${crypto.randomUUID()}`,
        title: 'Prompt',
        symbol: SO,
        gridX,
        gridY,
        renderContent: () => {
            const content = document.createElement('div');
            content.textContent = 'Prompt glyph';
            return content;
        }
    };

    glyphs.push(promptGlyph);

    const glyphElement = await createPromptGlyph(promptGlyph);
    canvas.appendChild(glyphElement);

    const rect = glyphElement.getBoundingClientRect();
    const width = Math.round(rect.width);
    const height = Math.round(rect.height);

    uiState.addCanvasGlyph({
        id: promptGlyph.id,
        symbol: SO,
        gridX,
        gridY,
        width,
        height
    });

    log.debug(SEG.UI, `[Canvas] Spawned Prompt glyph at grid (${gridX}, ${gridY}) with size ${width}x${height}`);
}

/**
 * Spawn a new Prose glyph at grid position
 */
async function spawnProseGlyph(
    gridX: number,
    gridY: number,
    canvas: HTMLElement,
    glyphs: Glyph[]
): Promise<void> {
    const proseGlyph: Glyph = {
        id: `prose-${crypto.randomUUID()}`,
        title: 'Prose Document',
        symbol: Prose,
        gridX,
        gridY,
        renderContent: () => {
            const content = document.createElement('div');
            content.textContent = 'Prose glyph';
            return content;
        }
    };

    glyphs.push(proseGlyph);

    const glyphElement = await createProseGlyph(proseGlyph);
    canvas.appendChild(glyphElement);

    const rect = glyphElement.getBoundingClientRect();
    const width = Math.round(rect.width);
    const height = Math.round(rect.height);

    uiState.addCanvasGlyph({
        id: proseGlyph.id,
        symbol: Prose,
        gridX,
        gridY,
        width,
        height
    });

    log.debug(SEG.UI, `[Canvas] Spawned Prose glyph at grid (${gridX}, ${gridY}) with size ${width}x${height}`);
}

/**
 * Create a prose glyph element with markdown editor
 */
async function createProseGlyph(glyph: Glyph): Promise<HTMLElement> {
    const element = document.createElement('div');
    element.className = 'glyph prose-glyph';
    element.id = glyph.id;
    element.style.position = 'absolute';
    element.style.left = `${glyph.gridX * GRID_SIZE}px`;
    element.style.top = `${glyph.gridY * GRID_SIZE}px`;
    element.style.width = `${glyph.width || 400}px`;
    element.style.height = `${glyph.height || 300}px`;

    // Title bar
    const titleBar = document.createElement('div');
    titleBar.className = 'glyph-title-bar';
    titleBar.innerHTML = `
        <span class="glyph-symbol">${Prose}</span>
        <span class="glyph-title">${glyph.title || 'Prose Document'}</span>
        <button class="glyph-close" aria-label="Close">✕</button>
    `;

    // Close button
    const closeBtn = titleBar.querySelector('.glyph-close') as HTMLButtonElement;
    closeBtn.addEventListener('click', () => {
        element.remove();
        uiState.removeCanvasGlyph(glyph.id);
    });

    // Content area with textarea for markdown editing
    const contentArea = document.createElement('div');
    contentArea.className = 'glyph-content prose-editor-content';

    const textarea = document.createElement('textarea');
    textarea.className = 'prose-markdown-input';
    textarea.placeholder = 'Write your prose here...';
    textarea.style.width = '100%';
    textarea.style.height = '100%';
    textarea.style.border = 'none';
    textarea.style.outline = 'none';
    textarea.style.resize = 'none';
    textarea.style.fontFamily = 'monospace';
    textarea.style.fontSize = '14px';
    textarea.style.padding = '8px';

    contentArea.appendChild(textarea);

    // Resize handle
    const resizeHandle = document.createElement('div');
    resizeHandle.className = 'glyph-resize-handle';
    resizeHandle.style.position = 'absolute';
    resizeHandle.style.right = '0';
    resizeHandle.style.bottom = '0';
    resizeHandle.style.width = '12px';
    resizeHandle.style.height = '12px';
    resizeHandle.style.cursor = 'nwse-resize';
    resizeHandle.textContent = '⋰';

    element.appendChild(titleBar);
    element.appendChild(contentArea);
    element.appendChild(resizeHandle);

    // Make draggable and resizable using shared infrastructure
    const { makeDraggable, makeResizable } = await import('./glyph-interaction');
    makeDraggable(element, titleBar, glyph, { logLabel: 'ProseGlyph' });
    makeResizable(element, resizeHandle, glyph, { logLabel: 'ProseGlyph' });

    return element;
}

/**
 * Render a glyph on the canvas
 * Checks symbol type and creates appropriate glyph element
 */
async function renderGlyph(glyph: Glyph): Promise<HTMLElement> {
    log.debug(SEG.UI, `[Canvas] Rendering glyph ${glyph.id}`, {
        symbol: glyph.symbol,
        hasResult: !!glyph.result
    });

    // For py glyphs, create full editor
    if (glyph.symbol === 'py') {
        return await createPyGlyph(glyph);
    }

    // For IX glyphs, create full form
    if (glyph.symbol === IX) {
        return await createIxGlyph(glyph);
    }

    // For prompt glyphs, create template editor
    if (glyph.symbol === SO) {
        return await createPromptGlyph(glyph);
    }

    // For prose glyphs, create prose editor
    if (glyph.symbol === Prose) {
        return await createProseGlyph(glyph);
    }

    // For result glyphs, create result display
    if (glyph.symbol === 'result' && glyph.result) {
        log.debug(SEG.UI, `[Canvas] Creating result glyph for ${glyph.id}`);
        return createResultGlyph(glyph, glyph.result as ExecutionResult);
    }

    // Otherwise create simple grid glyph
    return createGridGlyph(glyph);
}
