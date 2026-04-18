/**
 * Focus Manifestation — DAG-aware thread layout.
 *
 * Double-click a glyph to enter its world. The composition DAG becomes
 * a navigable workspace: the vertical chain (bottom edges) stacks as a
 * thread in the center column, horizontal siblings (right edges) appear
 * in flanking columns with their own threads.
 *
 * Scroll: vertical navigates the thread (pans), horizontal shifts which
 * siblings are visible in flanking columns.
 *
 * See docs/glyphs/manifestations/focus.md for the full vision.
 */

import { getLogger, getLogSegment } from '../config';
import { visibleThreadMember } from '../edge-graph';

// ── Column breakpoints (mirrors focus.md) ──
// Always odd: 1, 3, or 5. Focus column is dead center.
const BREAKPOINTS: [number, number][] = [
    [960, 5],
    [720, 3],
];

function defaultColumnCount(viewportWidth: number): number {
    for (const [minWidth, cols] of BREAKPOINTS) {
        if (viewportWidth >= minWidth) return cols;
    }
    return 1;
}

// Center column is ~20% wider than sibling columns.
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

// Horizontal scroll pivot
const COMMIT_THRESHOLD = 150; // px of accumulated horizontal scroll to commit a pivot
const PIVOT_COOLDOWN = 500;   // ms after a pivot before another can fire

// ── Injected dependencies ──

/** Pan/zoom control — how focus reads and writes the canvas transform. */
export interface FocusPanControl {
    getTransform(canvasId: string): { panX: number; panY: number; scale: number };
    setPanZoom(container: HTMLElement, canvasId: string, panX: number, panY: number, scale: number, animate?: boolean): void;
}

/** Persistence for focus state across sessions. */
export interface FocusPersistence {
    get(canvasId: string): { focusedGlyphId: string | null; preFocusScale: number } | null;
    set(canvasId: string, state: { focusedGlyphId: string | null; preFocusScale: number }): void;
}

// ── Types ──

/**
 * DAG subgraph relevant to a focused glyph.
 * The provider walks the composition and returns this structure.
 * focus.ts doesn't know about compositions — it lays out what the provider gives it.
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

/** Returns the focus-relevant DAG subgraph for a glyph. */
export type FocusGraphProvider = (glyphId: string) => FocusGraph;

/** Returns the number of columns for the current viewport width. */
export type ColumnProvider = (viewportWidth: number) => number;

/** All dependencies needed to set up focus on a canvas. */
export interface FocusDeps {
    panControl: FocusPanControl;
    persistence?: FocusPersistence;
    graphProvider?: FocusGraphProvider;
    columnProvider?: ColumnProvider;
}

interface TransformedGlyph {
    element: HTMLElement;
    origLeft: string;
    origTop: string;
    origWidth: string;
    origHeight: string;
}

interface LayoutContext {
    centerColLeft: number;
    threadTop: number;
    threadHeight: number;
    viewW: number;
    viewH: number;
    centerWidth: number;
    siblingWidth: number;
    offsets: number[];
    centerCol: number;
    slotsPerSide: number;
    cols: number;
}

interface FocusState {
    focusedGlyphId: string | null;
    preFocusScale: number;
    threadTransformed: TransformedGlyph[];
    siblingTransformed: TransformedGlyph[];
    hiddenElements: HTMLElement[];
    deps: FocusDeps | null;
    // Scroll state
    container: HTMLElement | null;
    lastGraph: FocusGraph | null;
    lastGlyphElement: HTMLElement | null;
    layoutCtx: LayoutContext | null;
    scrollAccumX: number;
    pivotCooldownUntil: number;
}

// Per-canvas focus state
const focusStates = new Map<string, FocusState>();

function getState(canvasId: string): FocusState {
    if (!focusStates.has(canvasId)) {
        focusStates.set(canvasId, {
            focusedGlyphId: null,
            preFocusScale: 1.0,
            threadTransformed: [],
            siblingTransformed: [],
            hiddenElements: [],
            deps: null,
            container: null,
            lastGraph: null,
            lastGlyphElement: null,
            layoutCtx: null,
            scrollAccumX: 0,
            pivotCooldownUntil: 0,
        });
    }
    return focusStates.get(canvasId)!;
}

// ── Persistence ──

function loadFocusState(canvasId: string): void {
    const state = getState(canvasId);
    const saved = state.deps?.persistence?.get(canvasId);
    if (saved) {
        state.focusedGlyphId = saved.focusedGlyphId;
        state.preFocusScale = saved.preFocusScale;
    }
}

function saveFocusState(canvasId: string): void {
    const state = getState(canvasId);
    state.deps?.persistence?.set(canvasId, {
        focusedGlyphId: state.focusedGlyphId,
        preFocusScale: state.preFocusScale,
    });
}

// ── Transform helpers ──

function restoreTransforms(transforms: TransformedGlyph[], animate = true): void {
    for (const t of transforms) {
        if (animate) t.element.style.transition = TRANSITION;
        t.element.style.left = t.origLeft;
        t.element.style.top = t.origTop;
        t.element.style.width = t.origWidth;
        t.element.style.height = t.origHeight;
        t.element.style.zIndex = '';
        delete t.element.dataset.focusTransformed;
        if (animate) setTimeout(() => { t.element.style.transition = ''; }, 450);
    }
}

function restoreAll(state: FocusState): void {
    restoreTransforms(state.threadTransformed);
    restoreTransforms(state.siblingTransformed);
    showHidden(state);
    state.threadTransformed = [];
    state.siblingTransformed = [];
}

function showHidden(state: FocusState): void {
    for (const el of state.hiddenElements) {
        el.style.visibility = '';
    }
    state.hiddenElements = [];
}

/**
 * Hide all glyph elements in the content layer that aren't placed in a focus slot.
 * Called after layout so only the visible thread + siblings remain.
 */
function hideNonParticipating(state: FocusState, contentLayer: HTMLElement): void {
    showHidden(state);
    const allGlyphs = contentLayer.querySelectorAll('[data-glyph-id]');
    for (const el of allGlyphs) {
        const htmlEl = el as HTMLElement;
        if (!htmlEl.dataset.focusTransformed) {
            htmlEl.style.visibility = 'hidden';
            state.hiddenElements.push(htmlEl);
        }
    }
}

function transformGlyph(el: HTMLElement, left: number, top: number, width: number, height?: number, animate = true): TransformedGlyph {
    const orig: TransformedGlyph = {
        element: el,
        origLeft: el.style.left,
        origTop: el.style.top,
        origWidth: el.style.width,
        origHeight: el.style.height,
    };

    if (animate) el.style.transition = TRANSITION;
    el.style.left = `${left}px`;
    el.style.top = `${top}px`;
    el.style.width = `${width}px`;
    if (height !== undefined) {
        el.style.height = `${height}px`;
    }
    el.style.zIndex = '10';
    el.dataset.focusTransformed = '1';
    if (animate) setTimeout(() => { el.style.transition = ''; }, 450);

    return orig;
}

// ── Layout helpers ──

function findGlyphElement(contentLayer: HTMLElement, glyphId: string, focusedElement?: HTMLElement): HTMLElement | null {
    if (focusedElement && focusedElement.dataset.glyphId === glyphId) return focusedElement;
    return contentLayer.querySelector(`[data-glyph-id="${glyphId}"]`) as HTMLElement | null;
}

/**
 * Lay out a vertical thread: stack glyphs top-to-bottom at the given X position and column width.
 * Each glyph keeps its natural height. Returns the total height of the stack.
 *
 * Two-pass: set widths first so content reflows, then measure actual heights and position.
 * Without this, threadHeight would be computed from pre-reflow heights (wrong clamp bounds).
 */
function layoutThread(
    transforms: TransformedGlyph[],
    contentLayer: HTMLElement,
    thread: string[],
    canvasLeft: number,
    canvasTop: number,
    colWidth: number,
    focusedElement?: HTMLElement,
    animate = true,
): number {
    // Pass 1: collect elements and set widths to trigger reflow
    const elements: HTMLElement[] = [];
    for (const memberId of thread) {
        const el = findGlyphElement(contentLayer, memberId, focusedElement);
        if (!el) continue;
        el.style.width = `${colWidth}px`;
        elements.push(el);
    }

    // Pass 2: measure post-reflow heights and position
    let y = canvasTop;
    for (const el of elements) {
        const height = el.offsetHeight;
        transforms.push(transformGlyph(el, canvasLeft, y, colWidth, undefined, animate));
        y += height + THREAD_GAP;
    }
    return y - canvasTop;
}

/**
 * Lay out sibling columns using the stored layout context and current siblingOffset.
 * Called on initial focus and after horizontal scroll shifts.
 */
function layoutSiblings(state: FocusState, _canvasId: string, animate = true): void {
    const { lastGraph: graph, layoutCtx: ctx, container, lastGlyphElement } = state;
    if (!graph || !ctx || !container || ctx.cols <= 1) return;

    const contentLayer = container.querySelector('.canvas-content-layer') as HTMLElement;
    if (!contentLayer) return;

    for (const memberId of graph.thread) {
        // Compute this member's Y position in the thread
        let memberY = ctx.threadTop;
        for (const tid of graph.thread) {
            if (tid === memberId) break;
            const tel = findGlyphElement(contentLayer, tid, lastGlyphElement ?? undefined);
            if (tel) memberY += tel.offsetHeight + THREAD_GAP;
        }

        // Left siblings — place up to slotsPerSide
        const leftSibs = graph.leftSiblings.get(memberId);
        if (leftSibs) {
            for (let i = 0; i < ctx.slotsPerSide && i < leftSibs.length; i++) {
                const col = ctx.centerCol - 1 - i;
                const colLeft = ctx.centerColLeft + (ctx.offsets[col] - ctx.offsets[ctx.centerCol]);

                const sibId = leftSibs[i];
                const sibThread = graph.siblingThreads.get(sibId);
                if (sibThread && sibThread.length > 0) {
                    layoutThread(state.siblingTransformed, contentLayer, sibThread, colLeft, memberY, ctx.siblingWidth, undefined, animate);
                } else {
                    const sibEl = findGlyphElement(contentLayer, sibId);
                    if (sibEl) {
                        state.siblingTransformed.push(transformGlyph(sibEl, colLeft, memberY, ctx.siblingWidth, undefined, animate));
                    }
                }
            }
        }

        // Right siblings — place up to slotsPerSide
        const rightSibs = graph.rightSiblings.get(memberId);
        if (rightSibs) {
            for (let i = 0; i < ctx.slotsPerSide && i < rightSibs.length; i++) {
                const col = ctx.centerCol + 1 + i;
                const colLeft = ctx.centerColLeft + (ctx.offsets[col] - ctx.offsets[ctx.centerCol]);

                const sibId = rightSibs[i];
                const sibThread = graph.siblingThreads.get(sibId);
                if (sibThread && sibThread.length > 0) {
                    layoutThread(state.siblingTransformed, contentLayer, sibThread, colLeft, memberY, ctx.siblingWidth, undefined, animate);
                } else {
                    const sibEl = findGlyphElement(contentLayer, sibId);
                    if (sibEl) {
                        state.siblingTransformed.push(transformGlyph(sibEl, colLeft, memberY, ctx.siblingWidth, undefined, animate));
                    }
                }
            }
        }
    }
}

// ── Core API ──

/**
 * Focus a glyph — enter its DAG. Canvas zooms to 100%, pans to center the
 * focused glyph. The vertical chain stacks in the center column, siblings
 * fill flanking columns with their own threads.
 */
export function focusGlyph(container: HTMLElement, canvasId: string, glyphElement: HTMLElement): void {
    const log = getLogger();
    const seg = getLogSegment();
    const state = getState(canvasId);
    const glyphId = glyphElement.dataset.glyphId;
    if (!glyphId || !state.deps) return;

    const { panControl, graphProvider, columnProvider } = state.deps;
    const transform = panControl.getTransform(canvasId);

    // Restore any previously transformed glyphs
    restoreAll(state);

    // Save pre-focus scale only on first focus (not when refocusing/pivoting)
    if (!state.focusedGlyphId) {
        state.preFocusScale = transform.scale;
    }

    state.focusedGlyphId = glyphId;
    state.scrollAccumX = 0;

    // Get the DAG subgraph for this glyph
    const graph: FocusGraph = graphProvider
        ? graphProvider(glyphId)
        : { thread: [glyphId], focusIndex: 0, leftSiblings: new Map(), rightSiblings: new Map(), siblingThreads: new Map() };

    // Viewport dimensions
    const rect = container.getBoundingClientRect();
    const viewW = rect.width;
    const viewH = rect.height;

    // Column layout — always odd (1, 3, 5)
    const cols = columnProvider ? columnProvider(viewW) : defaultColumnCount(viewW);
    const { centerWidth, siblingWidth, offsets } = computeColumnWidths(viewW, cols);
    const centerCol = Math.floor(cols / 2);

    // Read focused glyph's current canvas-space position (before transform)
    const glyphCenterX = glyphElement.offsetLeft + glyphElement.offsetWidth / 2;
    const glyphCenterY = glyphElement.offsetTop + glyphElement.offsetHeight / 2;

    // The center column's canvas-space left edge: anchor from focused glyph's center
    const centerColLeft = glyphCenterX - centerWidth / 2;

    // Pre-set widths on thread elements so content reflows before we measure heights.
    const contentLayer = container.querySelector('.canvas-content-layer') as HTMLElement;
    for (const memberId of graph.thread) {
        const el = findGlyphElement(contentLayer, memberId, glyphElement);
        if (el) el.style.width = `${centerWidth}px`;
    }

    // Now measure heights at the final width
    let heightAboveFocus = 0;
    for (let i = 0; i < graph.focusIndex; i++) {
        const el = findGlyphElement(contentLayer, graph.thread[i], glyphElement);
        if (el) heightAboveFocus += el.offsetHeight + THREAD_GAP;
    }

    const focusedHeight = glyphElement.offsetHeight;
    const threadTop = glyphCenterY - focusedHeight / 2 - heightAboveFocus;

    // Pan so the focused glyph's center lands at viewport center
    const targetPanX = viewW / 2 - glyphCenterX;
    const targetPanY = viewH / 2 - glyphCenterY;
    panControl.setPanZoom(container, canvasId, targetPanX, targetPanY, 1.0, true);

    // Lay out center thread
    const threadHeight = layoutThread(state.threadTransformed, contentLayer, graph.thread, centerColLeft, threadTop, centerWidth, glyphElement);

    // Store layout context for scroll re-layout
    const slotsPerSide = Math.floor(cols / 2);
    state.lastGraph = graph;
    state.lastGlyphElement = glyphElement;
    state.container = container;
    state.layoutCtx = { centerColLeft, threadTop, threadHeight, viewW, viewH, centerWidth, siblingWidth, offsets, centerCol, slotsPerSide, cols };

    // Lay out sibling columns
    layoutSiblings(state, canvasId);

    // Hide glyphs not placed in a focus slot
    hideNonParticipating(state, contentLayer);

    saveFocusState(canvasId);
    log.debug(seg, `[Focus] Enter ${glyphId} — ${graph.thread.length} thread, ${cols} cols, scroll ${(viewH * 0.5 - threadTop - threadHeight).toFixed(0)}..${(viewH * 0.5 - threadTop).toFixed(0)}`);
}

/**
 * Unfocus — all glyphs transform back, zoom restores, pan stays where you are.
 */
export function unfocusGlyph(container: HTMLElement, canvasId: string): void {
    const log = getLogger();
    const seg = getLogSegment();
    const state = getState(canvasId);
    if (!state.focusedGlyphId || !state.deps) return;

    const { panControl } = state.deps;
    restoreAll(state);

    // Restore zoom, adjust pan so viewport center stays in place
    const transform = panControl.getTransform(canvasId);
    const oldScale = transform.scale;
    const newScale = state.preFocusScale;

    const rect = container.getBoundingClientRect();
    const centerX = rect.width / 2;
    const centerY = rect.height / 2;
    const scaleFactor = newScale / oldScale;
    const newPanX = centerX - (centerX - transform.panX) * scaleFactor;
    const newPanY = centerY - (centerY - transform.panY) * scaleFactor;

    state.focusedGlyphId = null;
    state.lastGraph = null;
    state.lastGlyphElement = null;
    state.layoutCtx = null;
    state.scrollAccumX = 0;

    panControl.setPanZoom(container, canvasId, newPanX, newPanY, newScale, true);
    saveFocusState(canvasId);

    log.debug(seg, `[Focus] Exit — scale ${newScale.toFixed(2)}`);
}

/** Check if a canvas has a focused glyph */
export function isFocused(canvasId: string): boolean {
    return getState(canvasId).focusedGlyphId !== null;
}

/** Get the focused glyph ID (or null) */
export function getFocusedGlyphId(canvasId: string): string | null {
    return getState(canvasId).focusedGlyphId;
}

/**
 * Initialize focus for a canvas — loads persisted state, registers providers,
 * sets up scroll handling while focused.
 * Call after setupCanvasPan.
 */
export function setupCanvasFocus(
    container: HTMLElement,
    canvasId: string,
    deps: FocusDeps,
): void {
    const log = getLogger();
    const seg = getLogSegment();
    const state = getState(canvasId);
    state.deps = deps;
    state.container = container;
    loadFocusState(canvasId);

    const { panControl } = deps;

    // Pivot to a neighbor in the given direction (-1 = left, +1 = right).
    // Uses whichever thread member is currently centered in the viewport.
    function pivotHorizontal(direction: number): void {
        const graph = state.lastGraph;
        if (!graph || !state.focusedGlyphId) return;

        const contentLayer = container.querySelector('.canvas-content-layer') as HTMLElement;
        if (!contentLayer) return;

        // Determine which thread member is visible based on current pan
        const ctx = state.layoutCtx;
        let focusedId = state.focusedGlyphId;
        if (ctx && graph.thread.length > 1) {
            const transform = panControl.getTransform(canvasId);
            const viewportCenterY = ctx.viewH / 2 - transform.panY - ctx.threadTop;
            const heights: number[] = [];
            for (const memberId of graph.thread) {
                const el = findGlyphElement(contentLayer, memberId, state.lastGlyphElement ?? undefined);
                heights.push(el ? el.offsetHeight : 0);
            }
            focusedId = visibleThreadMember(graph.thread, heights, THREAD_GAP, viewportCenterY);
        }

        // Get the first left or right sibling of the focused glyph
        let targetId: string | null = null;
        if (direction < 0) {
            // Go left — first left sibling of the focused glyph
            const leftSibs = graph.leftSiblings.get(focusedId);
            if (leftSibs && leftSibs.length > 0) targetId = leftSibs[0];
        } else {
            // Go right — first right sibling of the focused glyph
            const rightSibs = graph.rightSiblings.get(focusedId);
            if (rightSibs && rightSibs.length > 0) targetId = rightSibs[0];
        }

        if (!targetId) {
            log.debug(seg, `[Focus] Pivot ${direction < 0 ? 'left' : 'right'} — no neighbor`);
            return;
        }

        const targetEl = findGlyphElement(contentLayer, targetId);
        if (!targetEl) {
            log.debug(seg, `[Focus] Pivot — element not found: ${targetId}`);
            return;
        }

        log.debug(seg, `[Focus] Pivot ${direction < 0 ? 'left' : 'right'} — ${focusedId} → ${targetId}`);
        focusGlyph(container, canvasId, targetEl);
    }

    // Arrow keys need focus — ensure container is focusable
    if (!container.hasAttribute('tabindex')) {
        container.setAttribute('tabindex', '0');
        container.style.outline = 'none';
    }

    // Scroll while focused — vertical pans thread, horizontal pivots
    container.addEventListener('wheel', (e: WheelEvent) => {
        if (!state.focusedGlyphId) return;
        e.preventDefault();

        const ctx = state.layoutCtx;
        if (!ctx) return;

        // Vertical scroll — pan through thread
        if (e.deltaY !== 0 && !e.shiftKey) {
            const transform = panControl.getTransform(canvasId);
            let newPanY = transform.panY - e.deltaY;

            // Clamp: thread top can't go below viewport center, thread bottom can't go above viewport center
            const maxPanY = ctx.viewH * 0.5 - ctx.threadTop;
            const minPanY = ctx.viewH * 0.5 - ctx.threadTop - ctx.threadHeight;
            const clampedMax = Math.max(maxPanY, minPanY);
            newPanY = Math.max(minPanY, Math.min(clampedMax, newPanY));

            panControl.setPanZoom(container, canvasId, transform.panX, newPanY, 1.0);
        }

        // Horizontal scroll — accumulate, commit once past threshold, then cooldown
        const dominantlyHorizontal = Math.abs(e.deltaX) > Math.abs(e.deltaY) * 2;
        const hDelta = dominantlyHorizontal ? e.deltaX : (e.shiftKey ? e.deltaY : 0);
        if (hDelta !== 0 && Date.now() >= state.pivotCooldownUntil) {
            state.scrollAccumX += hDelta;
            if (Math.abs(state.scrollAccumX) >= COMMIT_THRESHOLD) {
                pivotHorizontal(state.scrollAccumX > 0 ? 1 : -1);
                state.scrollAccumX = 0;
                state.pivotCooldownUntil = Date.now() + PIVOT_COOLDOWN;
            }
        }
    }, { passive: false });

    // Arrow keys while focused — left/right pivots
    container.addEventListener('keydown', (e: KeyboardEvent) => {
        if (!state.focusedGlyphId) return;
        if (!state.layoutCtx) return;

        if (e.key === 'ArrowLeft') {
            e.preventDefault();
            pivotHorizontal(-1);
        } else if (e.key === 'ArrowRight') {
            e.preventDefault();
            pivotHorizontal(1);
        }
    });
}

/** Reset focus state (for testing) */
export function resetFocusState(canvasId: string): void {
    focusStates.delete(canvasId);
}
