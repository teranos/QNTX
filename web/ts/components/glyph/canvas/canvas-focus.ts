/**
 * Canvas Focus — zoom/pan to center a glyph + transform glyph to fill column.
 *
 * Double-click: canvas zooms to 100% and pans to center the glyph.
 * The glyph DOM element transforms to fill its column (viewport / column count).
 * Escape: glyph transforms back, zoom restores to pre-focus level, pan stays.
 *
 * Focus is persisted across sessions via uiState.
 */

import { log, SEG } from '../../../logger';
import { uiState } from '../../../state/ui';
import { getTransform, setPanZoom } from './canvas-pan';

// Column breakpoints (mirrors focus.md)
const BREAKPOINTS: [number, number][] = [
    [960, 4],
    [720, 3],
    [480, 2],
];

function getColumnCount(viewportWidth: number): number {
    for (const [minWidth, cols] of BREAKPOINTS) {
        if (viewportWidth >= minWidth) return cols;
    }
    return 1;
}

const TRANSITION = 'left 0.45s ease-out, top 0.45s ease-out, width 0.45s ease-out, height 0.45s ease-out';

interface FocusState {
    focusedGlyphId: string | null;
    focusedElement: HTMLElement | null;
    preFocusScale: number;
    // Original glyph dimensions (canvas-space) for restore
    origLeft: string;
    origTop: string;
    origWidth: string;
    origHeight: string;
}

// Per-canvas focus state
const focusStates = new Map<string, FocusState>();

function getState(canvasId: string): FocusState {
    if (!focusStates.has(canvasId)) {
        focusStates.set(canvasId, {
            focusedGlyphId: null,
            focusedElement: null,
            preFocusScale: 1.0,
            origLeft: '',
            origTop: '',
            origWidth: '',
            origHeight: '',
        });
    }
    return focusStates.get(canvasId)!;
}

/**
 * Load persisted focus state from uiState
 */
function loadFocusState(canvasId: string): void {
    if (typeof uiState.getCanvasFocus !== 'function') return;
    const saved = uiState.getCanvasFocus(canvasId);
    if (saved) {
        const state = getState(canvasId);
        state.focusedGlyphId = saved.focusedGlyphId;
        state.preFocusScale = saved.preFocusScale;
        // Element ref can't be persisted — will be resolved on next focus or ignored
    }
}

/**
 * Save focus state to uiState
 */
function saveFocusState(canvasId: string): void {
    if (typeof uiState.setCanvasFocus !== 'function') return;
    const state = getState(canvasId);
    uiState.setCanvasFocus(canvasId, {
        focusedGlyphId: state.focusedGlyphId,
        preFocusScale: state.preFocusScale,
    });
}

/**
 * Restore a focused glyph element to its original dimensions
 */
function restoreGlyph(state: FocusState): void {
    const el = state.focusedElement;
    if (!el) return;

    el.style.transition = TRANSITION;
    el.style.left = state.origLeft;
    el.style.top = state.origTop;
    el.style.width = state.origWidth;
    el.style.height = state.origHeight;
    el.style.zIndex = '';

    setTimeout(() => { el.style.transition = ''; }, 450);
}

/**
 * Focus a glyph — zoom canvas to 100%, pan to center, transform glyph to fill column.
 */
export function focusGlyph(container: HTMLElement, canvasId: string, glyphElement: HTMLElement): void {
    const state = getState(canvasId);
    const glyphId = glyphElement.dataset.glyphId;
    if (!glyphId) return;

    const transform = getTransform(canvasId);

    // If refocusing a different glyph, restore the previous one first
    if (state.focusedElement && state.focusedElement !== glyphElement) {
        restoreGlyph(state);
    }

    // Save pre-focus scale only on first focus (not when refocusing)
    if (!state.focusedGlyphId) {
        state.preFocusScale = transform.scale;
    }

    // Save original glyph dimensions
    state.focusedGlyphId = glyphId;
    state.focusedElement = glyphElement;
    state.origLeft = glyphElement.style.left;
    state.origTop = glyphElement.style.top;
    state.origWidth = glyphElement.style.width;
    state.origHeight = glyphElement.style.height;

    // Read glyph center in canvas space (before we transform it)
    const glyphCenterX = glyphElement.offsetLeft + glyphElement.offsetWidth / 2;
    const glyphCenterY = glyphElement.offsetTop + glyphElement.offsetHeight / 2;

    // Canvas zoom to 100%
    const targetScale = 1.0;

    // Pan to center glyph in viewport
    const rect = container.getBoundingClientRect();
    const viewW = rect.width;
    const viewH = rect.height;
    const targetPanX = viewW / 2 - glyphCenterX * targetScale;
    const targetPanY = viewH / 2 - glyphCenterY * targetScale;

    setPanZoom(container, canvasId, targetPanX, targetPanY, targetScale, true);

    // Transform glyph to fill its column
    const cols = getColumnCount(viewW);
    const colWidth = viewW / cols;
    const colIndex = Math.floor(cols / 2); // center column

    // Glyph target position in canvas space: column position adjusted for pan
    const padY = viewH * 0.1; // 10% padding top and bottom
    const targetLeft = (colIndex * colWidth - targetPanX) / targetScale;
    const targetTop = (padY - targetPanY) / targetScale;
    const targetWidth = colWidth / targetScale;
    const targetHeight = (viewH - padY * 2) / targetScale;

    glyphElement.style.transition = TRANSITION;
    glyphElement.style.left = `${targetLeft}px`;
    glyphElement.style.top = `${targetTop}px`;
    glyphElement.style.width = `${targetWidth}px`;
    glyphElement.style.height = `${targetHeight}px`;
    glyphElement.style.zIndex = '10';

    setTimeout(() => { glyphElement.style.transition = ''; }, 450);

    saveFocusState(canvasId);
    log.debug(SEG.GLYPH, '[CanvasFocus] Focus glyph', { canvasId, glyphId, cols, colWidth });
}

/**
 * Unfocus — glyph transforms back, zoom restores, pan stays where you are.
 */
export function unfocusGlyph(container: HTMLElement, canvasId: string): void {
    const state = getState(canvasId);
    if (!state.focusedGlyphId) return;

    // Restore glyph element
    restoreGlyph(state);

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
    state.focusedElement = null;
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
 * Initialize focus for a canvas — loads persisted state.
 * Call after setupCanvasPan.
 */
export function setupCanvasFocus(canvasId: string): void {
    loadFocusState(canvasId);
}

/**
 * Reset focus state (for testing)
 */
export function resetFocusState(canvasId: string): void {
    focusStates.delete(canvasId);
}
