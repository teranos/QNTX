/**
 * Canvas Spawn Menu
 *
 * Right-click menu for spawning new elements on the canvas.
 * Element types and their spawn configuration come from element-registry.ts.
 */

import type { Element } from '@teranos/elements';
import { log, SEG } from '../../../logger';
import { getRestDuration } from '@teranos/elements';
import { uiState } from '../../../state/ui';
import { getTransform } from './canvas-pan';
import {
    type ElementTypeEntry,
    getAllElementTypes,
    getSpawnableElements,
    getElementTypeBySymbol,
    getCommandEntry,
    getMatchingCommandNames,
    getCommandLabel,
} from '../element-registry';
import { showMenuScrim, removeScrim, enterPlacementMode } from './placement-mode';
import { commitCursorPlacement } from '@teranos/elements';
import { enterThreadBuildingMode } from './thread-line';
import { getThreadColor } from '../thread-element';
import { addSpine } from './spine-renderer';

/** Part Alternation Mark — thread/spine symbol */
const THREAD_SYMBOL = '\u303D'; // 〽

/** Thread entry shown when right-clicking an element symbol */
function getSymbolContextEntries(): ElementTypeEntry[] {
    const entry = getElementTypeBySymbol(THREAD_SYMBOL);
    if (!entry) return [];
    return [{ ...entry, spawnMenuOrder: 0 }];
}

/** Whether the spawn menu is currently open */
let spawnMenuOpen = false;

/** Check if the spawn menu is currently visible */
export function isSpawnMenuOpen(): boolean {
    return spawnMenuOpen;
}

/** Vertical spacing between elements in the list */
const ELEMENT_SPACING = 48;

/** Depth range for subtle Z float */
const FLOAT_DEPTH = 15;

/** Cursor-facing tilt intensity */
const FACE_INTENSITY = 0.08;

/** Element descriptions for context reveal */
const ELEMENT_DESCRIPTIONS: Record<string, { desc: string; hint?: string }> = {
    'AX':        { desc: 'Query the attestation graph', hint: 'subject predicate context actor' },
    'SE':        { desc: 'Semantic similarity search', hint: 'Find attestations by meaning' },
    'Py':        { desc: 'Python code editor', hint: 'Runs in embedded interpreter' },
    'TS':        { desc: 'TypeScript code editor', hint: 'Runs in browser via Bun' },
    'Prompt':    { desc: 'AI prompt with context', hint: 'Attach attestations as grounding' },
    'Note':      { desc: 'Freeform text note', hint: 'Select text → convert to prompt' },
    'Subcanvas': { desc: 'Nested canvas workspace', hint: 'Infinite depth' },
    'Thread':    { desc: 'Navigational thread', hint: 'Connect elements into a path' },
};

function buildContextReveal(entry: ElementTypeEntry): string {
    const info = ELEMENT_DESCRIPTIONS[entry.label];
    if (!info) return `<strong>${entry.title}</strong>`;
    const hint = info.hint ? `<div class="spawn-context-hint">${info.hint}</div>` : '';
    return `<strong>${entry.title}</strong><div class="spawn-context-desc">${info.desc}</div>${hint}`;
}

/**
 * Show right-click spawn nebula — 3D cloud of cursor elements
 *
 * Symbols drift in 3D space around the click point, facing the cursor.
 * Hovering an element brings it forward, flattens it, grows it, and reveals
 * contextual data. Moving into the reveal allows inline interaction.
 * Clicking carries the element to cursor for canvas placement.
 */
export function showSpawnMenu(
    mouseX: number,
    mouseY: number,
    canvas: HTMLElement,
    items: Element[],
    canvasId: string = 'canvas-workspace',
    symbolContext: HTMLElement | null = null
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
    menu.setAttribute('aria-label', 'Spawn new element');
    menu.style.left = `${mouseX}px`;
    menu.style.top = `${mouseY}px`;

    let menuRemoved = false;
    let driftAnimId = 0;

    const removeMenu = (keepScrim = false) => {
        cancelAnimationFrame(driftAnimId);
        const duration = getRestDuration() * 0.4;
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

    const selectEntry = (entry: ElementTypeEntry, btnElement: HTMLElement) => {
        // Detach button from menu before removing menu — preserves DOM identity
        btnElement.remove();
        removeMenu(true);

        // Thread building mode: click symbols to build path, click empty canvas to finish
        if (symbolContext && entry.symbol === THREAD_SYMBOL) {
            const threadColor = getThreadColor(0);
            spawnMenuOpen = false;
            enterThreadBuildingMode(symbolContext, threadColor, (result) => {
                // Place 〽 element at click position
                const cont = canvas.parentElement!;
                const contRect = cont.getBoundingClientRect();
                const t = getTransform(canvasId);
                const px = Math.round((result.placeX - contRect.left - t.panX) / t.scale);
                const py = Math.round((result.placeY - contRect.top - t.panY) / t.scale);

                // Spawn 〽 — reuse the cursor element so the needle keeps DOM identity
                spawnElement(px, py, canvas, items, canvasId, entry, result.cursorElement, undefined, result.symbolElement).then((threadElementId) => {
                    const allNodes = [...result.nodeIds, threadElementId];
                    const spine = {
                        id: `spine-${crypto.randomUUID()}`,
                        color: threadColor,
                        nodes: allNodes,
                    };
                    addSpine(canvasId, canvas, spine);
                    uiState.addCanvasSpine(spine);
                }).catch((err: unknown) => log.error(SEG.ELEMENT, '[SpawnMenu] thread element spawn failed:', err));
            }, () => {
                // Cancelled
            });
            return;
        }

        // Standard placement mode for all other element types
        enterPlacementMode(entry, canvas, (clientX, clientY, cursorElement, cursorRect, symbolElement, content) => {
            const cont = canvas.parentElement!;
            const contRect = cont.getBoundingClientRect();
            const t = getTransform(canvasId);
            const px = Math.round((clientX - contRect.left - t.panX) / t.scale);
            const py = Math.round((clientY - contRect.top - t.panY) / t.scale);
            void spawnElement(px, py, canvas, items, canvasId, entry, cursorElement, cursorRect, symbolElement, content);
        }, btnElement);
    };

    // Collect spawnable entries — context-aware:
    // Right-click on element symbol: show thread actions (〽)
    // Right-click on canvas background: show element types
    const entries = symbolContext
        ? getSymbolContextEntries()
        : [
            ...getSpawnableElements(),
            ...getAllElementTypes().filter(g => g.className.includes('canvas-plugin-element'))
        ];

    // Assign each element a vertical list position with per-element float phase
    interface FloatNode {
        el: HTMLElement;
        entry: ElementTypeEntry;
        baseY: number;       // vertical position in list
        driftPhase: number;  // per-element phase offset for organic float
    }

    const nodes: FloatNode[] = [];
    const totalHeight = (entries.length - 1) * ELEMENT_SPACING;

    for (let i = 0; i < entries.length; i++) {
        const entry = entries[i];
        const baseY = i * ELEMENT_SPACING - totalHeight / 2;

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
            // Skip hovered element — lock it in place, no wobble
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

    log.debug(SEG.ELEMENT, `[Canvas] Spawn nebula opened at (${x}, ${y}) with ${entries.length} elements`);
}

/** Normalize canvasId for storage: 'canvas-workspace' → '' (root) */
function storageCanvasId(canvasId: string): string {
    return canvasId === 'canvas-workspace' ? '' : canvasId;
}

/** Duration for cursor-to-placed morph animation */
const PLACE_MORPH_DURATION_MS = 350;

/** Spawn an element of the given type at pixel position */
async function spawnElement(
    x: number,
    y: number,
    canvas: HTMLElement,
    items: Element[],
    canvasId: string,
    entry: ElementTypeEntry,
    cursorElement?: HTMLElement,
    cursorRect?: DOMRect,
    symbolElement?: HTMLElement | null,
    content?: string
): Promise<string> {
    const elementId = `${entry.label.toLowerCase()}-${crypto.randomUUID()}`;
    const item: Element = {
        id: elementId,
        title: entry.title,
        symbol: entry.symbol,
        x,
        y,
        content,
        cursorElement,
        symbolElement: symbolElement ?? undefined,
        renderContent: () => {
            const el = document.createElement('div');
            el.textContent = `${entry.title} element`;
            return el;
        }
    };

    items.push(item);

    // If we have a cursor element, morph the box first, then render content
    if (cursorElement && cursorRect && getRestDuration() > 0) {
        await morphCursorToPlaced(
            cursorElement, cursorRect, canvas, item, entry, canvasId, items, symbolElement
        );
    } else {
        // No cursor — immediate spawn (search bar, programmatic)
        const elementElement = await entry.render(item);
        canvas.appendChild(elementElement);
        persistElement(elementElement, item, entry, canvasId);
    }
    return elementId;
}

/** Morph cursor box into placed element: animate shape, then mount content */
async function morphCursorToPlaced(
    _cursorElement: HTMLElement,
    cursorRect: DOMRect,
    canvas: HTMLElement,
    item: Element,
    entry: ElementTypeEntry,
    canvasId: string,
    _elements: Element[],
    symbolElement?: HTMLElement | null
): Promise<void> {
    // The cursor element is on document.body with position: fixed.
    // Render the element content into it (this also sets canvas layout via canvasPlaced).
    const elementElement = await entry.render(item);

    // Temporarily hide all children except the symbol during morph
    const children = Array.from(elementElement.children);
    for (const child of children) {
        if (child === symbolElement || child.contains(symbolElement as Node)) continue;
        (child as HTMLElement).style.opacity = '0';
    }

    // Append to canvas so it gets canvas layout
    canvas.appendChild(elementElement);

    // Force layout to get the final rect
    elementElement.offsetHeight;
    const finalRect = elementElement.getBoundingClientRect();

    // Animate the entire element from cursor rect to final rect using transform
    const dx = cursorRect.left + cursorRect.width / 2 - (finalRect.left + finalRect.width / 2);
    const dy = cursorRect.top + cursorRect.height / 2 - (finalRect.top + finalRect.height / 2);
    const sx = cursorRect.width / finalRect.width;
    const sy = cursorRect.height / finalRect.height;

    const morph = elementElement.animate([
        {
            transform: `translate(${dx}px, ${dy}px) scale(${sx}, ${sy})`,
            borderRadius: '6px',
        },
        {
            transform: 'translate(0, 0) scale(1, 1)',
            borderRadius: elementElement.style.borderRadius || '6px',
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
        commitCursorPlacement(elementElement);
    };

    persistElement(elementElement, item, entry, canvasId);

    log.debug(SEG.ELEMENT, `[Canvas] Morphing ${entry.label} from cursor (${Math.round(cursorRect.left)},${Math.round(cursorRect.top)}) to (${Math.round(finalRect.left)},${Math.round(finalRect.top)}) ${Math.round(finalRect.width)}x${Math.round(finalRect.height)}`);
}

/** Persist element to UI state */
function persistElement(
    elementElement: HTMLElement,
    item: Element,
    entry: ElementTypeEntry,
    canvasId: string
): void {
    const rect = elementElement.getBoundingClientRect();
    const width = Math.round(rect.width);
    const height = Math.round(rect.height);

    uiState.addCanvasElement({
        id: item.id,
        symbol: entry.symbol,
        x: item.x!,
        y: item.y!,
        width,
        height,
        canvas_id: storageCanvasId(canvasId),
        ...(item.content !== undefined ? { content: item.content } : entry.defaultContent !== undefined ? { content: entry.defaultContent } : {}),
        ...(entry.pluginName !== undefined && { plugin_name: entry.pluginName }),
    });

    log.debug(SEG.ELEMENT, `[Canvas] Spawned ${entry.label} element at (${item.x}, ${item.y}) with size ${width}x${height}`);
}

/** Re-export registry command helpers for existing callers */
export { getMatchingCommandNames as getMatchingCommands, getCommandLabel };

/**
 * Spawn an element on the active canvas by command name.
 * Returns true if an element was spawned.
 */
export function spawnElementByCommand(command: string): boolean {
    const entry = getCommandEntry(command);
    if (!entry) return false;

    const workspace = document.querySelector('.canvas-workspace') as HTMLElement | null;
    if (!workspace) return false;

    const contentLayer = workspace.querySelector('.canvas-content-layer') as HTMLElement | null;
    if (!contentLayer) return false;

    const items: Element[] = (workspace as any).__elements || [];
    const canvasId = workspace.dataset.canvasId || 'canvas-workspace';

    // Spawn at center of visible canvas
    const rect = workspace.getBoundingClientRect();
    const x = Math.round(rect.width / 2);
    const y = Math.round(rect.height / 2);

    // TODO(#547): Element spawning from search bar needs refinement — ghost preview under cursor, click-to-place, visual distinction in search results
    spawnElement(x, y, contentLayer, items, canvasId, entry)
        .catch(err => log.error(SEG.ELEMENT, `Failed to spawn element "${command}": ${err}`));
    return true;
}
