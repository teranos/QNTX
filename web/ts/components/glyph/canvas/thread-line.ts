/**
 * Thread Building Mode — interactive path construction through glyph symbols.
 *
 * Flow:
 * 1. Enter mode: scrim dims, 〽 cursor follows mouse, symbols glow
 * 2. Click a glyph symbol: adds it to the thread path, Bezier extends
 * 3. Click empty canvas: places the 〽 glyph, thread is finished
 * 4. Escape: cancels thread building
 *
 * The SVG overlay draws flowing Bezier curves through all connected symbols,
 * with a live segment from the last symbol to the cursor.
 */

import { createCursorElement, attachCursorToMouse } from '@qntx/glyphs';
import { showMenuScrim, removeScrim } from './placement-mode';
import { log, SEG } from '../../../logger';

/** Opacity of the thread line during creation */
const LINE_OPACITY = 0.6;

/** Stroke width of the thread line */
const LINE_WIDTH = 2.5;

/** Symbol for thread glyph */
const THREAD_SYMBOL = '\u303D'; // 〽

/**
 * Build a cubic Bezier `d` attribute between two screen-space points.
 * Curves perpendicular to the line, bowing outward.
 */
function bezierPath(x1: number, y1: number, x2: number, y2: number, side: number = 1): string {
    const dx = x2 - x1;
    const dy = y2 - y1;
    const dist = Math.sqrt(dx * dx + dy * dy);

    // Perpendicular direction
    const bow = Math.min(dist * 0.25, 60) * side;
    const nx = -dy / (dist || 1);
    const ny = dx / (dist || 1);

    const mx = (x1 + x2) / 2 + nx * bow;
    const my = (y1 + y2) / 2 + ny * bow;

    const cx1 = x1 + (mx - x1) * 0.6;
    const cy1 = y1 + (my - y1) * 0.6;
    const cx2 = x2 + (mx - x2) * 0.6;
    const cy2 = y2 + (my - y2) * 0.6;

    return `M ${x1} ${y1} C ${cx1} ${cy1}, ${cx2} ${cy2}, ${x2} ${y2}`;
}

/** Get center of an element in screen coordinates */
function centerOf(el: HTMLElement): { x: number; y: number } {
    const rect = el.getBoundingClientRect();
    return { x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 };
}

/** Build full SVG path `d` through all nodes + cursor position */
function buildFullPath(nodes: HTMLElement[], cursorX: number, cursorY: number): string {
    if (nodes.length === 0) return '';

    const segments: string[] = [];
    for (let i = 0; i < nodes.length - 1; i++) {
        const a = centerOf(nodes[i]);
        const b = centerOf(nodes[i + 1]);
        const side = i % 2 === 0 ? 1 : -1;
        segments.push(bezierPath(a.x, a.y, b.x, b.y, side));
    }

    // Live segment from last node to cursor
    const last = centerOf(nodes[nodes.length - 1]);
    const liveSide = (nodes.length - 1) % 2 === 0 ? 1 : -1;
    segments.push(bezierPath(last.x, last.y, cursorX, cursorY, liveSide));

    return segments.join(' ');
}

export interface ThreadBuildResult {
    /** Glyph IDs in thread order (from data-glyph-id on parent .canvas-glyph) */
    nodeIds: string[];
    /** Screen position where 〽 was placed */
    placeX: number;
    placeY: number;
    /** The cursor element that was following the mouse — handed off to become the placed 〽 */
    cursorElement: HTMLElement;
    /** The cursor's symbol span (.glyph-cursor-symbol) — to be reused as the placed .glyph-symbol */
    symbolElement: HTMLElement | null;
}

/** Snap radius in pixels — clicks within this distance of a symbol snap to it */
const SNAP_RADIUS = 40;

/** Find the nearest .glyph-symbol within SNAP_RADIUS, excluding already-added nodes */
function findNearestSymbol(cx: number, cy: number, exclude: HTMLElement[]): HTMLElement | null {
    const symbols = document.querySelectorAll('.glyph-symbol');
    let best: HTMLElement | null = null;
    let bestDist = SNAP_RADIUS;

    for (const sym of symbols) {
        const el = sym as HTMLElement;
        if (exclude.includes(el)) continue;
        const rect = el.getBoundingClientRect();
        const sx = rect.left + rect.width / 2;
        const sy = rect.top + rect.height / 2;
        const dx = cx - sx;
        const dy = cy - sy;
        const dist = Math.sqrt(dx * dx + dy * dy);
        if (dist < bestDist) {
            bestDist = dist;
            best = el;
        }
    }
    return best;
}

/**
 * Enter thread building mode.
 *
 * @param originSymbol    The .glyph-symbol element that was right-clicked (or last symbol when extending)
 * @param color           Thread color (e.g. red for first thread)
 * @param onComplete      Called with the built thread when user places 〽
 * @param onCancel        Called if user presses Escape
 * @param existingNodeIds Pre-existing glyph IDs when extending a thread (excludes 〽)
 */
export function enterThreadBuildingMode(
    originSymbol: HTMLElement,
    color: string,
    onComplete: (result: ThreadBuildResult) => void,
    onCancel: () => void,
    existingNodeIds?: string[],
): void {
    // Track connected symbol elements — resolve existing nodes to their .glyph-symbol elements
    const nodes: HTMLElement[] = [];
    if (existingNodeIds && existingNodeIds.length > 0) {
        for (const id of existingNodeIds) {
            const glyphEl = document.querySelector(`[data-glyph-id="${id}"]`) as HTMLElement | null;
            const sym = glyphEl?.querySelector('.glyph-symbol') as HTMLElement | null;
            if (sym) nodes.push(sym);
        }
    } else {
        nodes.push(originSymbol);
    }

    // Scrim
    showMenuScrim();
    const scrim = document.querySelector('.placement-scrim') as HTMLElement;
    if (scrim) scrim.className = 'placement-scrim placement-scrim--carrying';

    // Cursor — 〽 following the mouse
    const cursor = createCursorElement(THREAD_SYMBOL, 'Thread');
    cursor.style.color = color;
    document.body.appendChild(cursor);
    const cleanupCursorMove = attachCursorToMouse(cursor);

    // SVG overlay for thread lines
    const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    svg.style.position = 'fixed';
    svg.style.inset = '0';
    svg.style.width = '100vw';
    svg.style.height = '100vh';
    svg.style.pointerEvents = 'none';
    svg.style.zIndex = '10002';

    const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
    path.setAttribute('fill', 'none');
    path.setAttribute('stroke', color);
    path.setAttribute('stroke-width', String(LINE_WIDTH));
    path.setAttribute('stroke-opacity', String(LINE_OPACITY));
    path.setAttribute('stroke-linecap', 'round');
    svg.appendChild(path);

    document.body.appendChild(svg);

    // Symbol glow + hide system cursor
    document.body.classList.add('thread-building-active');

    // Track mouse for live line + snap preview
    let mouseX = 0;
    let mouseY = 0;
    let snapTarget: HTMLElement | null = null;
    const onMouseMove = (e: MouseEvent) => {
        mouseX = e.clientX;
        mouseY = e.clientY;

        // Update snap preview
        const nearest = findNearestSymbol(mouseX, mouseY, nodes);
        if (nearest !== snapTarget) {
            if (snapTarget) snapTarget.classList.remove('thread-snap-target');
            snapTarget = nearest;
            if (snapTarget) snapTarget.classList.add('thread-snap-target');
        }

        // Draw line to snap target center if snapping, otherwise to cursor
        if (snapTarget) {
            const sc = centerOf(snapTarget);
            path.setAttribute('d', buildFullPath(nodes, sc.x, sc.y));
        } else {
            path.setAttribute('d', buildFullPath(nodes, mouseX, mouseY));
        }
    };
    document.addEventListener('mousemove', onMouseMove);

    // Initial draw
    const origin = centerOf(originSymbol);
    path.setAttribute('d', bezierPath(origin.x, origin.y, origin.x, origin.y));

    const cleanup = (opts: { keepCursor?: boolean } = {}) => {
        if (snapTarget) snapTarget.classList.remove('thread-snap-target');
        document.body.classList.remove('thread-building-active');
        document.removeEventListener('mousemove', onMouseMove);
        document.removeEventListener('mousedown', onMouseDown, true);
        document.removeEventListener('keydown', onKeyDown);
        document.removeEventListener('contextmenu', onContextMenu);
        cleanupCursorMove();
        if (!opts.keepCursor) cursor.remove();
        svg.remove();
        removeScrim();
    };

    const onMouseDown = (e: MouseEvent) => {
        if (e.button !== 0) return;
        e.preventDefault();
        e.stopPropagation();

        // Hit-test through cursor and scrim
        cursor.style.display = 'none';
        if (scrim) scrim.style.display = 'none';
        const target = document.elementFromPoint(e.clientX, e.clientY) as HTMLElement | null;
        cursor.style.display = '';
        if (scrim) scrim.style.display = '';

        // Check if clicking on a glyph symbol
        const symbolTarget = target?.closest('.glyph-symbol') as HTMLElement | null;
        const glyphTarget = target?.closest('.canvas-glyph') as HTMLElement | null;

        if (symbolTarget && glyphTarget) {
            // Add this symbol to the thread path (skip if already added)
            const glyphId = glyphTarget.dataset.glyphId;
            if (glyphId && !nodes.includes(symbolTarget)) {
                nodes.push(symbolTarget);
                // Redraw with new node
                path.setAttribute('d', buildFullPath(nodes, mouseX, mouseY));
                log.debug(SEG.GLYPH, `[Thread] Added node ${glyphId} (${nodes.length} nodes)`);
            }
        } else {
            // No direct hit — snap to nearest symbol within radius
            const snapped = findNearestSymbol(e.clientX, e.clientY, nodes);
            if (snapped) {
                const glyphId = snapped.closest('.canvas-glyph')?.getAttribute('data-glyph-id');
                if (glyphId) {
                    nodes.push(snapped);
                    path.setAttribute('d', buildFullPath(nodes, mouseX, mouseY));
                    log.debug(SEG.GLYPH, `[Thread] Snapped to node ${glyphId} (${nodes.length} nodes)`);
                    return;
                }
            }

            // Clicked empty canvas — finish thread
            const nodeIds: string[] = [];
            for (const node of nodes) {
                const glyph = node.closest('.canvas-glyph') as HTMLElement | null;
                if (glyph?.dataset.glyphId) nodeIds.push(glyph.dataset.glyphId);
            }

            // Hand off the cursor element to become the placed 〽 (preserves DOM identity)
            const cursorSymbol = cursor.querySelector('.glyph-cursor-symbol') as HTMLElement | null;
            cleanup({ keepCursor: true });
            onComplete({
                nodeIds,
                placeX: e.clientX,
                placeY: e.clientY,
                cursorElement: cursor,
                symbolElement: cursorSymbol,
            });
            log.debug(SEG.GLYPH, `[Thread] Completed with ${nodeIds.length} nodes`);
        }
    };

    const onKeyDown = (e: KeyboardEvent) => {
        if (e.key === 'Escape') {
            e.preventDefault();
            cleanup();
            onCancel();
        }
    };

    const onContextMenu = (e: MouseEvent) => {
        e.preventDefault();
        cleanup();
        onCancel();
    };

    document.addEventListener('mousedown', onMouseDown, { capture: true });
    document.addEventListener('keydown', onKeyDown);
    document.addEventListener('contextmenu', onContextMenu);

    log.debug(SEG.GLYPH, `[Thread] Entered building mode from ${originSymbol.textContent}`);
}
