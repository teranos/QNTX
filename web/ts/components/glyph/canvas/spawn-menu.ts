/**
 * Canvas Spawn Menu
 *
 * Right-click menu for spawning new glyphs on the canvas.
 * Glyph types and their spawn configuration come from glyph-registry.ts.
 */

import type { Glyph } from '@qntx/glyphs';
import { log, SEG } from '../../../logger';
import { getMinimizeDuration } from '@qntx/glyphs';
import { uiState } from '../../../state/ui';
import { getTransform } from './canvas-pan';
import {
    type GlyphTypeEntry,
    getAllGlyphTypes,
    getSpawnableGlyphs,
    getCommandEntry,
    getMatchingCommandNames,
    getCommandLabel,
} from '../glyph-registry';
import { showMenuScrim, removeScrim, enterPlacementMode } from './placement-mode';
import { commitCursorPlacement } from '@qntx/glyphs';

/** Whether the spawn menu is currently open */
let spawnMenuOpen = false;

/** Check if the spawn menu is currently visible */
export function isSpawnMenuOpen(): boolean {
    return spawnMenuOpen;
}

/** Vertical spacing between glyphs in the list */
const GLYPH_SPACING = 48;

/** Depth range for subtle Z float */
const FLOAT_DEPTH = 15;

/** Cursor-facing tilt intensity */
const FACE_INTENSITY = 0.08;

/** Glyph descriptions for context reveal */
const GLYPH_DESCRIPTIONS: Record<string, { desc: string; hint?: string }> = {
    'AX':        { desc: 'Query the attestation graph', hint: 'subject predicate context actor' },
    'SE':        { desc: 'Semantic similarity search', hint: 'Find attestations by meaning' },
    'Py':        { desc: 'Python code editor', hint: 'Runs in embedded interpreter' },
    'TS':        { desc: 'TypeScript code editor', hint: 'Runs in browser via Bun' },
    'Prompt':    { desc: 'AI prompt with context', hint: 'Attach attestations as grounding' },
    'Note':      { desc: 'Freeform text note', hint: 'Select text → convert to prompt' },
    'Subcanvas': { desc: 'Nested canvas workspace', hint: 'Infinite depth' },
};

function buildContextReveal(entry: GlyphTypeEntry): string {
    const info = GLYPH_DESCRIPTIONS[entry.label];
    if (!info) return `<strong>${entry.title}</strong>`;
    const hint = info.hint ? `<div class="spawn-context-hint">${info.hint}</div>` : '';
    return `<strong>${entry.title}</strong><div class="spawn-context-desc">${info.desc}</div>${hint}`;
}

/**
 * Show right-click spawn nebula — 3D cloud of cursor glyphs
 *
 * Symbols drift in 3D space around the click point, facing the cursor.
 * Hovering a glyph brings it forward, flattens it, grows it, and reveals
 * contextual data. Moving into the reveal allows inline interaction.
 * Clicking carries the glyph to cursor for canvas placement.
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

    // Calculate logical canvas position for spawn
    const container = canvas.parentElement!;
    const containerRect = container.getBoundingClientRect();
    const { panX, panY, scale } = getTransform(canvasId);
    const x = Math.round((mouseX - containerRect.left - panX) / scale);
    const y = Math.round((mouseY - containerRect.top - panY) / scale);

    spawnMenuOpen = true;
    showMenuScrim();

    // Create nebula container
    const menu = document.createElement('div');
    menu.className = 'canvas-spawn-menu';
    menu.setAttribute('role', 'menu');
    menu.setAttribute('aria-label', 'Spawn new glyph');
    menu.style.left = `${mouseX}px`;
    menu.style.top = `${mouseY}px`;

    let menuRemoved = false;
    let driftAnimId = 0;

    const removeMenu = (keepScrim = false) => {
        cancelAnimationFrame(driftAnimId);
        const duration = getMinimizeDuration() * 0.4;
        if (duration === 0) {
            menu.remove();
            if (!keepScrim) removeScrim();
            menuRemoved = true;
            return;
        }
        const animation = menu.animate([{ opacity: 1 }, { opacity: 0 }], {
            duration,
            easing: 'ease',
            fill: 'forwards'
        });
        animation.onfinish = () => {
            menu.remove();
            if (!keepScrim) removeScrim();
            menuRemoved = true;
        };
    };

    const selectEntry = (entry: GlyphTypeEntry, btnElement: HTMLElement) => {
        // Detach button from menu before removing menu — preserves DOM identity
        btnElement.remove();
        removeMenu(true);
        enterPlacementMode(entry, canvas, (clientX, clientY, cursorElement, cursorRect, symbolElement, content) => {
            const cont = canvas.parentElement!;
            const contRect = cont.getBoundingClientRect();
            const t = getTransform(canvasId);
            const px = Math.round((clientX - contRect.left - t.panX) / t.scale);
            const py = Math.round((clientY - contRect.top - t.panY) / t.scale);
            void spawnGlyph(px, py, canvas, glyphs, canvasId, entry, cursorElement, cursorRect, symbolElement, content);
        }, btnElement);
    };

    // Collect all spawnable entries
    const entries = [
        ...getSpawnableGlyphs(),
        ...getAllGlyphTypes().filter(g => g.className.includes('canvas-plugin-glyph'))
    ];

    // Assign each glyph a vertical list position with per-glyph float phase
    interface FloatNode {
        el: HTMLElement;
        entry: GlyphTypeEntry;
        baseY: number;       // vertical position in list
        driftPhase: number;  // per-glyph phase offset for organic float
    }

    const nodes: FloatNode[] = [];
    const totalHeight = (entries.length - 1) * GLYPH_SPACING;

    for (let i = 0; i < entries.length; i++) {
        const entry = entries[i];
        const baseY = i * GLYPH_SPACING - totalHeight / 2;

        const btn = document.createElement('button');
        btn.className = 'canvas-spawn-button';
        btn.setAttribute('role', 'menuitem');
        btn.textContent = entry.symbol;

        // Context reveal panel — rich description
        const reveal = document.createElement('div');
        reveal.className = 'spawn-context-reveal';
        reveal.innerHTML = buildContextReveal(entry);
        btn.appendChild(reveal);

        const onBtnMouseDown = (e: MouseEvent) => {
            e.stopPropagation();
            btn.removeEventListener('mousedown', onBtnMouseDown);
            selectEntry(entry, btn);
        };
        btn.addEventListener('mousedown', onBtnMouseDown);
        menu.appendChild(btn);

        nodes.push({ el: btn, entry, baseY, driftPhase: Math.random() * Math.PI * 2 });
    }

    document.body.appendChild(menu);

    // Track mouse position for cursor-facing
    let cursorX = mouseX;
    let cursorY = mouseY;
    const onMouseMove = (e: MouseEvent) => {
        cursorX = e.clientX;
        cursorY = e.clientY;
    };
    document.addEventListener('mousemove', onMouseMove);

    // Animate: orbital drift + cursor-facing
    let startTime = performance.now();
    const drift = (now: number) => {
        if (menuRemoved) {
            document.removeEventListener('mousemove', onMouseMove);
            return;
        }

        const elapsed = (now - startTime) / 1000; // seconds

        for (const node of nodes) {
            // Skip hovered glyph — lock it in place, no wobble
            if (node.el.matches(':hover')) {
                node.el.style.transform = `translate3d(0px, ${node.baseY}px, 0px)`;
                continue;
            }

            // Subtle float — small XY drift around fixed list position
            const driftX = Math.sin(elapsed * 0.8 + node.driftPhase) * 3;
            const driftY = Math.cos(elapsed * 0.6 + node.driftPhase * 1.3) * 2;
            const driftZ = Math.sin(elapsed * 0.3 + node.driftPhase * 0.7) * FLOAT_DEPTH;

            // Cursor-facing: gentle tilt toward mouse
            const dx = cursorX - mouseX;
            const dy = cursorY - mouseY;
            const rotY = dx * FACE_INTENSITY * 0.1;
            const rotX = -dy * FACE_INTENSITY * 0.1;

            node.el.style.transform =
                `translate3d(${driftX}px, ${node.baseY + driftY}px, ${driftZ}px) rotateX(${rotX}deg) rotateY(${rotY}deg)`;
        }

        driftAnimId = requestAnimationFrame(drift);
    };

    // Fade in from nothing
    menu.style.opacity = '0';
    requestAnimationFrame(() => {
        menu.animate([{ opacity: 0 }, { opacity: 1 }], {
            duration: 300,
            easing: 'ease-out',
            fill: 'forwards'
        });
        driftAnimId = requestAnimationFrame(drift);
    });

    // Dismiss: click anywhere outside the menu, or press Escape
    const dismiss = () => {
        spawnMenuOpen = false;
        removeMenu();
        document.removeEventListener('mousemove', onMouseMove);
        document.removeEventListener('keydown', onKeyDown);
        document.removeEventListener('mousedown', onMouseDown);
        document.removeEventListener('contextmenu', onContextMenu);
    };

    const onMouseDown = (e: MouseEvent) => {
        if (!menu.contains(e.target as Node)) {
            e.preventDefault();
            dismiss();
        }
    };

    // Suppress right-click reopening the menu immediately after dismissing
    const onContextMenu = (e: MouseEvent) => {
        e.preventDefault();
        dismiss();
    };

    const onKeyDown = (e: KeyboardEvent) => {
        if (e.key === 'Escape') {
            e.preventDefault();
            dismiss();
        }
    };

    // Delay listener attachment to avoid catching the right-click that opened the menu
    setTimeout(() => {
        if (!menuRemoved) {
            document.addEventListener('mousedown', onMouseDown);
            document.addEventListener('keydown', onKeyDown);
            document.addEventListener('contextmenu', onContextMenu);
        }
    }, 0);

    log.debug(SEG.GLYPH, `[Canvas] Spawn nebula opened at (${x}, ${y}) with ${entries.length} glyphs`);
}

/** Normalize canvasId for storage: 'canvas-workspace' → '' (root) */
function storageCanvasId(canvasId: string): string {
    return canvasId === 'canvas-workspace' ? '' : canvasId;
}

/** Duration for cursor-to-placed morph animation */
const PLACE_MORPH_DURATION_MS = 350;

/** Spawn a glyph of the given type at pixel position */
async function spawnGlyph(
    x: number,
    y: number,
    canvas: HTMLElement,
    glyphs: Glyph[],
    canvasId: string,
    entry: GlyphTypeEntry,
    cursorElement?: HTMLElement,
    cursorRect?: DOMRect,
    symbolElement?: HTMLElement | null,
    content?: string
): Promise<void> {
    const glyph: Glyph = {
        id: `${entry.label.toLowerCase()}-${crypto.randomUUID()}`,
        title: entry.title,
        symbol: entry.symbol,
        x,
        y,
        content,
        cursorElement,
        symbolElement: symbolElement ?? undefined,
        renderContent: () => {
            const el = document.createElement('div');
            el.textContent = `${entry.title} glyph`;
            return el;
        }
    };

    glyphs.push(glyph);

    // If we have a cursor element, morph the box first, then render content
    if (cursorElement && cursorRect && getMinimizeDuration() > 0) {
        await morphCursorToPlaced(
            cursorElement, cursorRect, canvas, glyph, entry, canvasId, glyphs, symbolElement
        );
    } else {
        // No cursor — immediate spawn (search bar, programmatic)
        const glyphElement = await entry.render(glyph);
        canvas.appendChild(glyphElement);
        persistGlyph(glyphElement, glyph, entry, canvasId);
    }
}

/** Morph cursor box into placed glyph: animate shape, then mount content */
async function morphCursorToPlaced(
    _cursorElement: HTMLElement,
    cursorRect: DOMRect,
    canvas: HTMLElement,
    glyph: Glyph,
    entry: GlyphTypeEntry,
    canvasId: string,
    _glyphs: Glyph[],
    symbolElement?: HTMLElement | null
): Promise<void> {
    // The cursor element is on document.body with position: fixed.
    // Render the glyph content into it (this also sets canvas layout via canvasPlaced).
    const glyphElement = await entry.render(glyph);

    // Temporarily hide all children except the symbol during morph
    const children = Array.from(glyphElement.children);
    for (const child of children) {
        if (child === symbolElement || child.contains(symbolElement as Node)) continue;
        (child as HTMLElement).style.opacity = '0';
    }

    // Append to canvas so it gets canvas layout
    canvas.appendChild(glyphElement);

    // Force layout to get the final rect
    glyphElement.offsetHeight;
    const finalRect = glyphElement.getBoundingClientRect();

    // Animate the entire element from cursor rect to final rect using transform
    const dx = cursorRect.left + cursorRect.width / 2 - (finalRect.left + finalRect.width / 2);
    const dy = cursorRect.top + cursorRect.height / 2 - (finalRect.top + finalRect.height / 2);
    const sx = cursorRect.width / finalRect.width;
    const sy = cursorRect.height / finalRect.height;

    const morph = glyphElement.animate([
        {
            transform: `translate(${dx}px, ${dy}px) scale(${sx}, ${sy})`,
            borderRadius: '6px',
        },
        {
            transform: 'translate(0, 0) scale(1, 1)',
            borderRadius: glyphElement.style.borderRadius || '6px',
        },
    ], {
        duration: PLACE_MORPH_DURATION_MS,
        easing: 'cubic-bezier(0.4, 0, 0.2, 1)',
        fill: 'none',
    });

    // When morph completes, reveal content
    morph.onfinish = () => {
        for (const child of children) {
            if (child === symbolElement || child.contains(symbolElement as Node)) continue;
            (child as HTMLElement).style.opacity = '';
            (child as HTMLElement).animate([
                { opacity: '0' },
                { opacity: '1' },
            ], {
                duration: 150,
                easing: 'ease-out',
                fill: 'none',
            });
        }
        commitCursorPlacement(glyphElement);
    };

    persistGlyph(glyphElement, glyph, entry, canvasId);

    log.debug(SEG.GLYPH, `[Canvas] Morphing ${entry.label} from cursor (${Math.round(cursorRect.left)},${Math.round(cursorRect.top)}) to (${Math.round(finalRect.left)},${Math.round(finalRect.top)}) ${Math.round(finalRect.width)}x${Math.round(finalRect.height)}`);
}

/** Persist glyph to UI state */
function persistGlyph(
    glyphElement: HTMLElement,
    glyph: Glyph,
    entry: GlyphTypeEntry,
    canvasId: string
): void {
    const rect = glyphElement.getBoundingClientRect();
    const width = Math.round(rect.width);
    const height = Math.round(rect.height);

    uiState.addCanvasGlyph({
        id: glyph.id,
        symbol: entry.symbol,
        x: glyph.x!,
        y: glyph.y!,
        width,
        height,
        canvas_id: storageCanvasId(canvasId),
        ...(glyph.content !== undefined ? { content: glyph.content } : entry.defaultContent !== undefined ? { content: entry.defaultContent } : {}),
        ...(entry.pluginName !== undefined && { plugin_name: entry.pluginName }),
    });

    log.debug(SEG.GLYPH, `[Canvas] Spawned ${entry.label} glyph at (${glyph.x}, ${glyph.y}) with size ${width}x${height}`);
}

/** Re-export registry command helpers for existing callers */
export { getMatchingCommandNames as getMatchingCommands, getCommandLabel };

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
