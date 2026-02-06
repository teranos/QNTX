/**
 * Canvas Spawn Menu
 *
 * Right-click menu for spawning new glyphs on the canvas.
 * Creates IX, AX, Python, and Prompt glyphs at the clicked position.
 */

import type { Glyph } from '../glyph';
import { IX, AX, SO } from '@generated/sym.js';
import { log, SEG } from '../../../logger';
import { getMinimizeDuration } from '../glyph';
import { createIxGlyph } from '../ix-glyph';
import { createAxGlyph } from '../ax-glyph';
import { createPyGlyph } from '../py-glyph';
import { createPromptGlyph } from '../prompt-glyph';
import { uiState } from '../../../state/ui';

/** Duration multiplier for spawn menu animation */
const SPAWN_MENU_ANIMATION_SPEED = 0.5;

/**
 * Show right-click spawn menu with available symbols
 *
 * Available glyphs: IX (ingest), AX (attestation query), py (python editor), SO (prompt)
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
export function showSpawnMenu(
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

    // Calculate pixel position relative to canvas
    const canvasRect = canvas.getBoundingClientRect();
    const x = mouseX - canvasRect.left;
    const y = mouseY - canvasRect.top;

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
        const duration = getMinimizeDuration() * 0.4;
        if (duration === 0) {
            menu.remove();
            menuRemoved = true;
            return;
        }

        // Fade out before removing
        const animation = menu.animate([
            { opacity: 1 },
            { opacity: 0 }
        ], {
            duration,
            easing: 'ease',
            fill: 'forwards'
        });

        animation.onfinish = () => {
            menu.remove();
            menuRemoved = true;
        };
    };

    // Add IX symbol
    const ixBtn = document.createElement('button');
    ixBtn.className = 'canvas-spawn-button';
    ixBtn.textContent = IX;
    ixBtn.title = 'Spawn IX glyph';

    ixBtn.addEventListener('click', () => {
        spawnIxGlyph(x, y, canvas, glyphs);
        removeMenu();
    });

    menu.appendChild(ixBtn);

    // Add AX symbol
    const axBtn = document.createElement('button');
    axBtn.className = 'canvas-spawn-button';
    axBtn.textContent = AX;
    axBtn.title = 'Spawn AX query glyph';

    axBtn.addEventListener('click', () => {
        spawnAxGlyph(x, y, canvas, glyphs);
        removeMenu();
    });

    menu.appendChild(axBtn);

    // TODO: Refactor spawn menu to be data-driven
    // Loop over available symbols (Pulse, py, go, rs, ts) instead of hardcoding buttons
    // This will make it easier to add new programmature types (go, rs, ts)

    // Add py button
    const pyBtn = document.createElement('button');
    pyBtn.className = 'canvas-spawn-button';
    pyBtn.textContent = 'py';
    pyBtn.title = 'Spawn Python glyph';

    pyBtn.addEventListener('click', () => {
        spawnPyGlyph(x, y, canvas, glyphs);
        removeMenu();
    });

    menu.appendChild(pyBtn);

    // Add prompt button
    const promptBtn = document.createElement('button');
    promptBtn.className = 'canvas-spawn-button';
    promptBtn.textContent = SO;
    promptBtn.title = 'Spawn Prompt glyph';

    promptBtn.addEventListener('click', () => {
        spawnPromptGlyph(x, y, canvas, glyphs);
        removeMenu();
    });

    menu.appendChild(promptBtn);

    document.body.appendChild(menu);

    // Expand from mouse position (small to large)
    const duration = getMinimizeDuration() * SPAWN_MENU_ANIMATION_SPEED;
    if (duration > 0) {
        menu.animate([
            { transform: 'scale(0.3)', opacity: 0 },
            { transform: 'scale(1)', opacity: 1 }
        ], {
            duration,
            easing: 'ease-out',
            fill: 'both'
        });
    }

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

    log.debug(SEG.UI, `[Canvas] Spawn menu opened at (${x}, ${y})`);
}

/**
 * Spawn a new IX glyph at pixel position
 */
async function spawnIxGlyph(
    x: number,
    y: number,
    canvas: HTMLElement,
    glyphs: Glyph[]
): Promise<void> {
    const ixGlyph: Glyph = {
        id: `ix-${crypto.randomUUID()}`,
        title: 'Ingest',
        symbol: IX,
        x,
        y,
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
        x,
        y,
        width,
        height
    });

    log.debug(SEG.UI, `[Canvas] Spawned IX glyph at (${x}, ${y}) with size ${width}x${height}`);
}

/**
 * Spawn a new AX query glyph at pixel position
 */
function spawnAxGlyph(
    x: number,
    y: number,
    canvas: HTMLElement,
    glyphs: Glyph[]
): void {
    const axGlyph = createAxGlyph(undefined, '', x, y);

    // Add to glyphs array
    glyphs.push(axGlyph);

    // Render glyph on canvas (ax glyphs now render themselves)
    const glyphElement = axGlyph.renderContent();
    canvas.appendChild(glyphElement);

    // Get actual rendered size and persist (ensures default size is saved)
    const rect = glyphElement.getBoundingClientRect();
    const width = Math.round(rect.width);
    const height = Math.round(rect.height);

    // Update glyph with actual dimensions
    axGlyph.width = width;
    axGlyph.height = height;

    uiState.addCanvasGlyph({
        id: axGlyph.id,
        symbol: AX,
        x,
        y,
        width,
        height
    });

    log.debug(SEG.UI, `[Canvas] Spawned AX glyph at (${x}, ${y}) with size ${width}x${height}`);
}

/**
 * Spawn a new Python glyph at pixel position
 */
async function spawnPyGlyph(
    x: number,
    y: number,
    canvas: HTMLElement,
    glyphs: Glyph[]
): Promise<void> {
    const pyGlyph: Glyph = {
        id: `py-${crypto.randomUUID()}`,
        title: 'Python',
        symbol: 'py',
        x,
        y,
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
        x,
        y,
        width,
        height
    });

    log.debug(SEG.UI, `[Canvas] Spawned Python glyph at (${x}, ${y}) with size ${width}x${height}`);
}

/**
 * Spawn a new Prompt glyph at pixel position
 */
async function spawnPromptGlyph(
    x: number,
    y: number,
    canvas: HTMLElement,
    glyphs: Glyph[]
): Promise<void> {
    const promptGlyph: Glyph = {
        id: `prompt-${crypto.randomUUID()}`,
        title: 'Prompt',
        symbol: SO,
        x,
        y,
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
        x,
        y,
        width,
        height
    });

    log.debug(SEG.UI, `[Canvas] Spawned Prompt glyph at (${x}, ${y}) with size ${width}x${height}`);
}
