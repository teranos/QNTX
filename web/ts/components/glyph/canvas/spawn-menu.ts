/**
 * Canvas Spawn Menu
 *
 * Right-click menu for spawning new glyphs on the canvas.
 * Glyph types and their spawn configuration come from glyph-registry.ts.
 */

import type { Glyph } from '../glyph';
import { log, SEG } from '../../../logger';
import { getMinimizeDuration } from '../glyph';
import { uiState } from '../../../state/ui';
import {
    type GlyphTypeEntry,
    getAllGlyphTypes,
    getSpawnableGlyphs,
    getCommandEntry,
    getMatchingCommandNames,
    getCommandLabel,
} from '../glyph-registry';

/** Duration multiplier for spawn menu animation */
const SPAWN_MENU_ANIMATION_SPEED = 0.5;

/**
 * Show right-click spawn menu with available symbols
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
    glyphs: Glyph[],
    canvasId: string = 'canvas-workspace'
): void {
    // Remove any existing menu
    const existingMenu = document.querySelector('.canvas-spawn-menu');
    if (existingMenu) {
        existingMenu.remove();
    }

    // Calculate pixel position relative to canvas
    const canvasRect = canvas.getBoundingClientRect();
    const x = Math.round(mouseX - canvasRect.left);
    const y = Math.round(mouseY - canvasRect.top);

    // Create spawn menu
    const menu = document.createElement('div');
    menu.className = 'canvas-spawn-menu';
    menu.setAttribute('role', 'menu');
    menu.setAttribute('aria-label', 'Spawn new glyph');
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

    // Add built-in spawnable glyphs (ordered by spawnMenuOrder)
    for (const entry of getSpawnableGlyphs()) {
        const btn = document.createElement('button');
        btn.className = 'canvas-spawn-button';
        btn.setAttribute('role', 'menuitem');
        btn.textContent = entry.symbol;
        btn.title = `Spawn ${entry.title} glyph`;

        btn.addEventListener('click', () => {
            void spawnGlyph(x, y, canvas, glyphs, canvasId, entry);
            removeMenu();
        });

        menu.appendChild(btn);
    }

    // Add plugin glyphs
    const pluginGlyphs = getAllGlyphTypes().filter(g =>
        g.className.includes('canvas-plugin-glyph')
    );

    for (const glyphType of pluginGlyphs) {
        const btn = document.createElement('button');
        btn.className = 'canvas-spawn-button';
        btn.setAttribute('role', 'menuitem');
        btn.textContent = glyphType.symbol;
        btn.title = `Spawn ${glyphType.title} glyph`;

        btn.addEventListener('click', () => {
            void spawnGlyph(x, y, canvas, glyphs, canvasId, glyphType);
            removeMenu();
        });

        menu.appendChild(btn);
    }

    document.body.appendChild(menu);

    // Adjust position to keep menu within viewport bounds
    const menuRect = menu.getBoundingClientRect();
    const viewportWidth = window.innerWidth;
    const viewportHeight = window.innerHeight;

    let adjustedX = mouseX;
    let adjustedY = mouseY;

    // Check right edge
    if (mouseX + menuRect.width > viewportWidth) {
        adjustedX = viewportWidth - menuRect.width - 8; // 8px padding from edge
    }

    // Check bottom edge
    if (mouseY + menuRect.height > viewportHeight) {
        adjustedY = viewportHeight - menuRect.height - 8;
    }

    // Apply adjusted position if needed
    if (adjustedX !== mouseX || adjustedY !== mouseY) {
        menu.style.left = `${adjustedX}px`;
        menu.style.top = `${adjustedY}px`;
    }

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

    log.debug(SEG.GLYPH, `[Canvas] Spawn menu opened at (${x}, ${y})`);
}

/** Normalize canvasId for storage: 'canvas-workspace' → '' (root) */
function storageCanvasId(canvasId: string): string {
    return canvasId === 'canvas-workspace' ? '' : canvasId;
}

/** Spawn a glyph of the given type at pixel position */
async function spawnGlyph(
    x: number,
    y: number,
    canvas: HTMLElement,
    glyphs: Glyph[],
    canvasId: string,
    entry: GlyphTypeEntry
): Promise<void> {
    const glyph: Glyph = {
        id: `${entry.label.toLowerCase()}-${crypto.randomUUID()}`,
        title: entry.title,
        symbol: entry.symbol,
        x,
        y,
        renderContent: () => {
            const content = document.createElement('div');
            content.textContent = `${entry.title} glyph`;
            return content;
        }
    };

    glyphs.push(glyph);

    const glyphElement = await entry.render(glyph);
    canvas.appendChild(glyphElement);

    const rect = glyphElement.getBoundingClientRect();
    const width = Math.round(rect.width);
    const height = Math.round(rect.height);

    uiState.addCanvasGlyph({
        id: glyph.id,
        symbol: entry.symbol,
        x,
        y,
        width,
        height,
        canvas_id: storageCanvasId(canvasId),
        ...(entry.defaultContent !== undefined && { content: entry.defaultContent }),
        ...(entry.pluginName !== undefined && { plugin_name: entry.pluginName }),
    });

    log.debug(SEG.GLYPH, `[Canvas] Spawned ${entry.label} glyph at (${x}, ${y}) with size ${width}x${height}`);
}

/** Re-export registry command helpers for existing callers */
export { getMatchingCommandNames as getMatchingCommands, getCommandLabel };

/** Human-readable labels for spawn commands */
export const COMMAND_LABELS: Record<string, string> = (() => {
    const labels: Record<string, string> = {};
    for (const entry of getSpawnableGlyphs()) {
        labels[entry.label.toLowerCase()] = getCommandLabel(entry.label.toLowerCase());
        for (const alias of entry.commandAliases ?? []) {
            labels[alias] = getCommandLabel(alias);
        }
    }
    return labels;
})();

/**
 * Spawn a glyph on the active canvas by command name.
 * Returns true if a glyph was spawned.
 */
export function spawnGlyphByCommand(command: string): boolean {
    const entry = getCommandEntry(command);
    if (!entry) return false;

    const workspace = document.querySelector('.canvas-workspace') as HTMLElement | null;
    if (!workspace) return false;

    const contentLayer = workspace.querySelector('.canvas-content-layer') as HTMLElement | null;
    if (!contentLayer) return false;

    const glyphs: Glyph[] = (workspace as any).__glyphs || [];
    const canvasId = workspace.dataset.canvasId || 'canvas-workspace';

    // Spawn at center of visible canvas
    const rect = workspace.getBoundingClientRect();
    const x = Math.round(rect.width / 2);
    const y = Math.round(rect.height / 2);

    // TODO(#547): Glyph spawning from search bar needs refinement — ghost preview under cursor, click-to-place, visual distinction in search results
    spawnGlyph(x, y, contentLayer, glyphs, canvasId, entry)
        .catch(err => log.error(SEG.GLYPH, `Failed to spawn glyph "${command}": ${err}`));
    return true;
}
