/**
 * Canvas Glyph - Fractal container with spatial grid layout
 *
 * The canvas is a glyph that morphs to full-screen and contains other glyphs
 * arranged on a spatial grid. Right-click spawns new glyphs.
 *
 * Selection:
 * - Click a glyph to select it (highlighted border, action bar appears)
 * - Click canvas background to deselect
 * - Single selection only
 * - Action bar provides delete (and future actions)
 *
 * This demonstrates the fractal principle: all glyphs are containers.
 */

import type { Glyph } from './glyph';
import { IX } from '@generated/sym.js';
import { log, SEG } from '../../logger';
import { createGridGlyph } from './grid-glyph';
import { createIxGlyph } from './ix-glyph';
import { createPyGlyph } from './py-glyph';
import { createResultGlyph, type ExecutionResult } from './result-glyph';
import { uiState } from '../../state/ui';
import { GRID_SIZE } from './grid-constants';
import { getMinimizeDuration } from './glyph';

// ============================================================================
// Selection State
// ============================================================================

/** Currently selected glyph ID (null = nothing selected) */
let selectedGlyphId: string | null = null;

/** Reference to the action bar element */
let actionBar: HTMLElement | null = null;

/**
 * Select a glyph on the canvas. Deselects any previous selection.
 */
function selectGlyph(glyphId: string, container: HTMLElement): void {
    deselectAll(container);

    selectedGlyphId = glyphId;

    const el = container.querySelector(`[data-glyph-id="${glyphId}"]`) as HTMLElement | null;
    if (el) {
        el.classList.add('canvas-glyph-selected');
        showActionBar(el, container);
    }

    log.debug(SEG.UI, `[Canvas] Selected glyph ${glyphId}`);
}

/**
 * Deselect all glyphs and hide action bar
 */
function deselectAll(container: HTMLElement): void {
    if (!selectedGlyphId) return;

    const prev = container.querySelector('.canvas-glyph-selected');
    if (prev) {
        prev.classList.remove('canvas-glyph-selected');
    }

    hideActionBar();
    selectedGlyphId = null;
}

/**
 * Show the action bar positioned above the selected glyph
 */
function showActionBar(glyphEl: HTMLElement, container: HTMLElement): void {
    hideActionBar();

    const bar = document.createElement('div');
    bar.className = 'canvas-action-bar';

    const deleteBtn = document.createElement('button');
    deleteBtn.className = 'canvas-action-button canvas-action-delete';
    deleteBtn.title = 'Delete glyph';
    deleteBtn.textContent = '\u{1F5D1}';
    deleteBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        deleteSelectedGlyph(container);
    });

    bar.appendChild(deleteBtn);
    container.appendChild(bar);

    positionActionBar(bar, glyphEl, container);
    actionBar = bar;
}

/**
 * Position action bar centered above the glyph element
 */
function positionActionBar(bar: HTMLElement, glyphEl: HTMLElement, container: HTMLElement): void {
    const canvasRect = container.getBoundingClientRect();
    const glyphRect = glyphEl.getBoundingClientRect();

    const glyphLeft = glyphRect.left - canvasRect.left;
    const glyphTop = glyphRect.top - canvasRect.top;
    const glyphCenterX = glyphLeft + glyphRect.width / 2;

    bar.style.position = 'absolute';
    bar.style.left = `${glyphCenterX}px`;
    bar.style.top = `${glyphTop - 8}px`;
    bar.style.transform = 'translate(-50%, -100%)';
    bar.style.zIndex = '9999';
}

/**
 * Hide the action bar
 */
function hideActionBar(): void {
    if (actionBar) {
        actionBar.remove();
        actionBar = null;
    }
}

/**
 * Delete the currently selected glyph from the canvas
 * Animates scale-down + fade-out before removal (respects reduced motion)
 */
function deleteSelectedGlyph(container: HTMLElement): void {
    if (!selectedGlyphId) return;

    const glyphId = selectedGlyphId;
    const el = container.querySelector(`[data-glyph-id="${glyphId}"]`) as HTMLElement | null;

    // Clear selection immediately (prevent double-delete)
    hideActionBar();
    selectedGlyphId = null;

    // Remove from persisted state and local array immediately
    uiState.removeCanvasGlyph(glyphId);
    container.dispatchEvent(new CustomEvent('glyph-deleted', {
        detail: { glyphId }
    }));

    if (!el) return;

    const duration = getMinimizeDuration();

    if (duration === 0) {
        el.remove();
        log.debug(SEG.UI, `[Canvas] Deleted glyph ${glyphId}`);
        return;
    }

    // Animate out, then remove
    const animation = el.animate([
        { opacity: 1, transform: 'scale(1)' },
        { opacity: 0, transform: 'scale(0.85)' }
    ], {
        duration,
        easing: 'ease-in',
        fill: 'forwards'
    });

    animation.onfinish = () => {
        el.remove();
        log.debug(SEG.UI, `[Canvas] Deleted glyph ${glyphId}`);
    };
}

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

            // Selection: click on a glyph to select, click background to deselect
            container.addEventListener('click', (e) => {
                const target = e.target as HTMLElement;

                // Walk up from click target to find a glyph element
                const glyphEl = target.closest('[data-glyph-id]') as HTMLElement | null;

                if (glyphEl) {
                    const glyphId = glyphEl.dataset.glyphId;
                    if (glyphId) {
                        selectGlyph(glyphId, container);
                    }
                } else {
                    // Clicked on background (not a glyph) — deselect
                    deselectAll(container);
                }
            });

            // Clean up local glyphs array when a glyph is deleted
            container.addEventListener('glyph-deleted', ((e: CustomEvent<{ glyphId: string }>) => {
                const idx = glyphs.findIndex(g => g.id === e.detail.glyphId);
                if (idx !== -1) {
                    glyphs.splice(idx, 1);
                }
            }) as EventListener);

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

    // For result glyphs, create result display
    if (glyph.symbol === 'result' && glyph.result) {
        log.debug(SEG.UI, `[Canvas] Creating result glyph for ${glyph.id}`);
        return createResultGlyph(glyph, glyph.result as ExecutionResult);
    }

    // Otherwise create simple grid glyph
    return createGridGlyph(glyph);
}
