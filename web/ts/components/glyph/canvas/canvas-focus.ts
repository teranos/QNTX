/**
 * Canvas Focus — DAG-aware thread layout.
 *
 * Double-click a glyph to enter its world. The composition DAG becomes
 * a navigable workspace: the vertical chain (bottom edges) stacks as a
 * thread in the center column, horizontal siblings (right edges) appear
 * in flanking columns with their own threads.
 *
 * See docs/glyphs/manifestations/focus.md for the full vision.
 */

import { log, SEG } from '../../../logger';
import { uiState } from '../../../state/ui';
import { getTransform, setPanZoom } from './canvas-pan';

// ── Column breakpoints (mirrors focus.md) ──
// Always odd: 1, 3, or 5. Focus column is dead center.
const BREAKPOINTS: [number, number][] = [
    [960, 5],
    [720, 3],
];

function getColumnCount(viewportWidth: number): number {
    for (const [minWidth, cols] of BREAKPOINTS) {
        if (viewportWidth >= minWidth) return cols;
    }
    return 1;
}

// Center column is ~20% wider than sibling columns.
// Given C columns total, center gets weight 1.2, each sibling gets weight 1.0.
// Total weight = (C - 1) * 1.0 + 1.2. Each column's width = (weight / totalWeight) * viewportWidth.
const CENTER_WEIGHT = 1.2;
const SIBLING_WEIGHT = 1.0;

function computeColumnWidths(viewW: number, cols: number): { centerWidth: number; siblingWidth: number; offsets: number[] } {
    if (cols === 1) {
        return { centerWidth: viewW, siblingWidth: 0, offsets: [0] };
    }
    const siblingCount = cols - 1;
    const totalWeight = siblingCount * SIBLING_WEIGHT + CENTER_WEIGHT;
    const siblingWidth = (SIBLING_WEIGHT / totalWeight) * viewW;
    const centerWidth = (CENTER_WEIGHT / totalWeight) * viewW;
    const centerCol = Math.floor(cols / 2);

    const offsets: number[] = [];
    let x = 0;
    for (let i = 0; i < cols; i++) {
        offsets.push(x);
        x += i === centerCol ? centerWidth : siblingWidth;
    }
    return { centerWidth, siblingWidth, offsets };
}

const TRANSITION = 'left 0.45s ease-out, top 0.45s ease-out, width 0.45s ease-out, height 0.45s ease-out';
const THREAD_GAP = 6; // px between thread members

// ── Types ──

/**
 * DAG subgraph relevant to a focused glyph.
 * The provider walks the composition and returns this structure.
 * canvas-focus.ts doesn't know about compositions — it lays out what the provider gives it.
 */
export interface FocusGraph {
    /** Vertical chain (root to leaf via bottom edges) containing the focused glyph */
    thread: string[];
    /** Index of the focused glyph within thread */
    focusIndex: number;
    /** For each glyph in the thread, siblings to the LEFT (ordered outward: closest to center first) */
    leftSiblings: Map<string, string[]>;
    /** For each glyph in the thread, siblings to the RIGHT (ordered outward: closest to center first) */
    rightSiblings: Map<string, string[]>;
    /** For each sibling, its own vertical thread (bottom-chain) */
    siblingThreads: Map<string, string[]>;
}

/**
 * Returns the focus-relevant DAG subgraph for a glyph.
 * When not provided, each glyph is its own single-member thread.
 */
export type FocusGraphProvider = (glyphId: string) => FocusGraph;

/**
 * Returns the number of columns for the current viewport width.
 * When not provided, uses the default breakpoint-based column count.
 */
export type ColumnProvider = (viewportWidth: number) => number;

interface TransformedGlyph {
    element: HTMLElement;
    origLeft: string;
    origTop: string;
    origWidth: string;
    origHeight: string;
}

interface FocusState {
    focusedGlyphId: string | null;
    preFocusScale: number;
    transformed: TransformedGlyph[];
    graphProvider: FocusGraphProvider | null;
    columnProvider: ColumnProvider | null;
}

// Per-canvas focus state
const focusStates = new Map<string, FocusState>();

function getState(canvasId: string): FocusState {
    if (!focusStates.has(canvasId)) {
        focusStates.set(canvasId, {
            focusedGlyphId: null,
            preFocusScale: 1.0,
            transformed: [],
            graphProvider: null,
            columnProvider: null,
        });
    }
    return focusStates.get(canvasId)!;
}

// ── Persistence ──

function loadFocusState(canvasId: string): void {
    if (typeof uiState.getCanvasFocus !== 'function') return;
    const saved = uiState.getCanvasFocus(canvasId);
    if (saved) {
        const state = getState(canvasId);
        state.focusedGlyphId = saved.focusedGlyphId;
        state.preFocusScale = saved.preFocusScale;
    }
}

function saveFocusState(canvasId: string): void {
    if (typeof uiState.setCanvasFocus !== 'function') return;
    const state = getState(canvasId);
    uiState.setCanvasFocus(canvasId, {
        focusedGlyphId: state.focusedGlyphId,
        preFocusScale: state.preFocusScale,
    });
}

// ── Transform helpers ──

function restoreAll(state: FocusState): void {
    for (const t of state.transformed) {
        t.element.style.transition = TRANSITION;
        t.element.style.left = t.origLeft;
        t.element.style.top = t.origTop;
        t.element.style.width = t.origWidth;
        t.element.style.height = t.origHeight;
        t.element.style.zIndex = '';
        setTimeout(() => { t.element.style.transition = ''; }, 450);
    }
    state.transformed = [];
}

function transformGlyph(el: HTMLElement, left: number, top: number, width: number, height?: number): TransformedGlyph {
    const orig: TransformedGlyph = {
        element: el,
        origLeft: el.style.left,
        origTop: el.style.top,
        origWidth: el.style.width,
        origHeight: el.style.height,
    };

    el.style.transition = TRANSITION;
    el.style.left = `${left}px`;
    el.style.top = `${top}px`;
    el.style.width = `${width}px`;
    if (height !== undefined) {
        el.style.height = `${height}px`;
    }
    el.style.zIndex = '10';
    setTimeout(() => { el.style.transition = ''; }, 450);

    return orig;
}

// ── Layout helpers ──

/**
 * Find a glyph element by ID within the content layer.
 */
function findGlyphElement(contentLayer: HTMLElement, glyphId: string, focusedElement?: HTMLElement): HTMLElement | null {
    if (focusedElement && focusedElement.dataset.glyphId === glyphId) return focusedElement;
    return contentLayer.querySelector(`[data-glyph-id="${glyphId}"]`) as HTMLElement | null;
}

/**
 * Lay out a vertical thread: stack glyphs top-to-bottom at the given X position and column width.
 * Each glyph keeps its natural height. Returns the total height of the stack.
 */
function layoutThread(
    state: FocusState,
    contentLayer: HTMLElement,
    thread: string[],
    canvasLeft: number,
    canvasTop: number,
    colWidth: number,
    focusedElement?: HTMLElement,
): number {
    let y = canvasTop;
    for (const memberId of thread) {
        const el = findGlyphElement(contentLayer, memberId, focusedElement);
        if (!el) continue;

        const naturalHeight = el.offsetHeight;
        state.transformed.push(transformGlyph(el, canvasLeft, y, colWidth));
        y += naturalHeight + THREAD_GAP;
    }
    return y - canvasTop;
}

// ── Core API ──

/**
 * Focus a glyph — enter its DAG. Canvas zooms to 100%, pans to center the
 * focused glyph. The vertical chain stacks in the center column, siblings
 * fill flanking columns with their own threads.
 */
export function focusGlyph(container: HTMLElement, canvasId: string, glyphElement: HTMLElement): void {
    const state = getState(canvasId);
    const glyphId = glyphElement.dataset.glyphId;
    if (!glyphId) return;

    const transform = getTransform(canvasId);

    // Restore any previously transformed glyphs
    restoreAll(state);

    // Save pre-focus scale only on first focus (not when refocusing/pivoting)
    if (!state.focusedGlyphId) {
        state.preFocusScale = transform.scale;
    }

    state.focusedGlyphId = glyphId;

    // Get the DAG subgraph for this glyph
    const graph: FocusGraph = state.graphProvider
        ? state.graphProvider(glyphId)
        : { thread: [glyphId], focusIndex: 0, leftSiblings: new Map(), rightSiblings: new Map(), siblingThreads: new Map() };

    // Viewport dimensions
    const rect = container.getBoundingClientRect();
    const viewW = rect.width;
    const viewH = rect.height;

    // Column layout — always odd (1, 3, 5)
    const cols = state.columnProvider ? state.columnProvider(viewW) : getColumnCount(viewW);
    const { centerWidth, siblingWidth, offsets } = computeColumnWidths(viewW, cols);
    const centerCol = Math.floor(cols / 2);

    // Read focused glyph's current canvas-space position (before transform)
    const glyphCenterX = glyphElement.offsetLeft + glyphElement.offsetWidth / 2;
    const glyphCenterY = glyphElement.offsetTop + glyphElement.offsetHeight / 2;

    // The center column's canvas-space left edge: anchor from focused glyph's center
    const centerColLeft = glyphCenterX - centerWidth / 2;

    // Compute canvas-space top for the thread so the focused glyph lands at viewport center.
    // First, measure where the focused glyph sits within its thread (cumulative height above it).
    let heightAboveFocus = 0;
    const contentLayer = container.querySelector('.canvas-content-layer') as HTMLElement;

    for (let i = 0; i < graph.focusIndex; i++) {
        const el = findGlyphElement(contentLayer, graph.thread[i], glyphElement);
        if (el) heightAboveFocus += el.offsetHeight + THREAD_GAP;
    }

    // The focused glyph should be vertically centered in the viewport.
    // Thread starts at: glyphCenterY - focusedGlyph.height/2 - heightAboveFocus
    const focusedHeight = glyphElement.offsetHeight;
    const threadTop = glyphCenterY - focusedHeight / 2 - heightAboveFocus;

    // Pan so the focused glyph's center lands at viewport center
    const targetPanX = viewW / 2 - glyphCenterX;
    const targetPanY = viewH / 2 - glyphCenterY;

    setPanZoom(container, canvasId, targetPanX, targetPanY, 1.0, true);

    // Lay out center thread
    layoutThread(state, contentLayer, graph.thread, centerColLeft, threadTop, centerWidth, glyphElement);

    // Lay out sibling columns — left siblings go left of center, right siblings go right
    if (cols > 1) {
        const slotsPerSide = Math.floor(cols / 2); // e.g. 5 cols → 2 slots per side

        for (const memberId of graph.thread) {
            // Compute this member's Y position in the thread
            let memberY = threadTop;
            for (const tid of graph.thread) {
                if (tid === memberId) break;
                const tel = findGlyphElement(contentLayer, tid, glyphElement);
                if (tel) memberY += tel.offsetHeight + THREAD_GAP;
            }

            // Left siblings: placed in columns left of center (closest first)
            const leftSibs = graph.leftSiblings.get(memberId);
            if (leftSibs) {
                for (let i = 0; i < leftSibs.length && i < slotsPerSide; i++) {
                    const col = centerCol - 1 - i; // closest to center first
                    const colLeft = centerColLeft + (offsets[col] - offsets[centerCol]);

                    const sibThread = graph.siblingThreads.get(leftSibs[i]);
                    if (sibThread && sibThread.length > 0) {
                        layoutThread(state, contentLayer, sibThread, colLeft, memberY, siblingWidth);
                    } else {
                        const sibEl = findGlyphElement(contentLayer, leftSibs[i]);
                        if (sibEl) {
                            state.transformed.push(transformGlyph(sibEl, colLeft, memberY, siblingWidth));
                        }
                    }
                }
            }

            // Right siblings: placed in columns right of center (closest first)
            const rightSibs = graph.rightSiblings.get(memberId);
            if (rightSibs) {
                for (let i = 0; i < rightSibs.length && i < slotsPerSide; i++) {
                    const col = centerCol + 1 + i; // closest to center first
                    const colLeft = centerColLeft + (offsets[col] - offsets[centerCol]);

                    const sibThread = graph.siblingThreads.get(rightSibs[i]);
                    if (sibThread && sibThread.length > 0) {
                        layoutThread(state, contentLayer, sibThread, colLeft, memberY, siblingWidth);
                    } else {
                        const sibEl = findGlyphElement(contentLayer, rightSibs[i]);
                        if (sibEl) {
                            state.transformed.push(transformGlyph(sibEl, colLeft, memberY, siblingWidth));
                        }
                    }
                }
            }
        }
    }

    saveFocusState(canvasId);
    log.debug(SEG.GLYPH, '[CanvasFocus] Focus glyph', {
        canvasId, glyphId, cols,
        threadSize: graph.thread.length,
        transformed: state.transformed.length,
    });
}

/**
 * Unfocus — all glyphs transform back, zoom restores, pan stays where you are.
 */
export function unfocusGlyph(container: HTMLElement, canvasId: string): void {
    const state = getState(canvasId);
    if (!state.focusedGlyphId) return;

    restoreAll(state);

    // Restore zoom, adjust pan so viewport center stays in place
    const transform = getTransform(canvasId);
    const oldScale = transform.scale;
    const newScale = state.preFocusScale;

    const rect = container.getBoundingClientRect();
    const centerX = rect.width / 2;
    const centerY = rect.height / 2;
    const scaleFactor = newScale / oldScale;
    const newPanX = centerX - (centerX - transform.panX) * scaleFactor;
    const newPanY = centerY - (centerY - transform.panY) * scaleFactor;

    state.focusedGlyphId = null;
    setPanZoom(container, canvasId, newPanX, newPanY, newScale, true);
    saveFocusState(canvasId);

    log.debug(SEG.GLYPH, '[CanvasFocus] Unfocus', { canvasId, scale: newScale });
}

/**
 * Check if a canvas has a focused glyph
 */
export function isFocused(canvasId: string): boolean {
    return getState(canvasId).focusedGlyphId !== null;
}

/**
 * Get the focused glyph ID (or null)
 */
export function getFocusedGlyphId(canvasId: string): string | null {
    return getState(canvasId).focusedGlyphId;
}

/**
 * Initialize focus for a canvas — loads persisted state, registers providers.
 * Call after setupCanvasPan.
 */
export function setupCanvasFocus(canvasId: string, graphProvider?: FocusGraphProvider, columnProvider?: ColumnProvider): void {
    loadFocusState(canvasId);
    const state = getState(canvasId);
    if (graphProvider) {
        state.graphProvider = graphProvider;
    }
    if (columnProvider) {
        state.columnProvider = columnProvider;
    }
}

/**
 * Reset focus state (for testing)
 */
export function resetFocusState(canvasId: string): void {
    focusStates.delete(canvasId);
}
