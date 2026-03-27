/**
 * Canvas Focus — zoom/pan to center a glyph, restore on unfocus.
 *
 * Focus is persisted across sessions via uiState.
 * Pan/zoom while focused is allowed — focus persists.
 * Escape unfocuses: zoom restores to pre-focus level, pan stays where you are.
 */

import { log, SEG } from '../../../logger';
import { uiState } from '../../../state/ui';
import { getTransform, setPanZoom } from './canvas-pan';

interface FocusState {
    focusedGlyphId: string | null;
    preFocusScale: number;
}

// Per-canvas focus state
const focusStates = new Map<string, FocusState>();

function getState(canvasId: string): FocusState {
    if (!focusStates.has(canvasId)) {
        focusStates.set(canvasId, {
            focusedGlyphId: null,
            preFocusScale: 1.0,
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
 * Focus a glyph — zoom/pan canvas to center it at readable scale.
 * Saves pre-focus scale so unfocus can restore zoom level.
 */
export function focusGlyph(container: HTMLElement, canvasId: string, glyphElement: HTMLElement): void {
    const state = getState(canvasId);
    const glyphId = glyphElement.dataset.glyphId;
    if (!glyphId) return;

    const transform = getTransform(canvasId);

    // Save pre-focus scale only on first focus (not when refocusing a different glyph)
    if (!state.focusedGlyphId) {
        state.preFocusScale = transform.scale;
    }

    state.focusedGlyphId = glyphId;

    // Read glyph position in canvas space
    const glyphX = glyphElement.offsetLeft;
    const glyphY = glyphElement.offsetTop;
    const glyphW = glyphElement.offsetWidth;
    const glyphH = glyphElement.offsetHeight;

    // Target scale: at least 1.0 (readable), keep current if already zoomed in more
    const targetScale = Math.max(1.0, transform.scale);

    // Target pan: center glyph in viewport
    const rect = container.getBoundingClientRect();
    const targetPanX = rect.width / 2 - (glyphX + glyphW / 2) * targetScale;
    const targetPanY = rect.height / 2 - (glyphY + glyphH / 2) * targetScale;

    setPanZoom(container, canvasId, targetPanX, targetPanY, targetScale, true);
    saveFocusState(canvasId);

    log.debug(SEG.GLYPH, '[CanvasFocus] Focus glyph', { canvasId, glyphId, scale: targetScale });
}

/**
 * Unfocus — restore pre-focus zoom level, keep pan where you are.
 */
export function unfocusGlyph(container: HTMLElement, canvasId: string): void {
    const state = getState(canvasId);
    if (!state.focusedGlyphId) return;

    const transform = getTransform(canvasId);
    const oldScale = transform.scale;
    const newScale = state.preFocusScale;

    // Adjust pan so viewport center stays in place during zoom change
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
