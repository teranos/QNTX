/**
 * Type-safe dataset attribute helpers for glyph DOM elements
 *
 * These helpers ensure dataset attributes are accessed and modified
 * with proper type safety and validation.
 */

import { isManifestation, type Manifestation } from './manifestation';

/**
 * Record which manifestation a glyph is in.
 *
 * AXIOMAS.md: a morph is a transition between manifestations, and a glyph is in
 * one at any time. This is where that is written down, so it can be read.
 */
export function setManifestation(element: HTMLElement, manifestation: Manifestation): void {
    element.dataset.manifestation = manifestation;
}

/**
 * Which manifestation a glyph is in, or null if nothing has said.
 *
 * Null for a name the table does not have: an element carrying one is not in
 * some eighth manifestation, it is carrying a word.
 */
export function getManifestation(element: HTMLElement): Manifestation | null {
    const name = element.dataset.manifestation;
    return name !== undefined && isManifestation(name) ? name : null;
}

/**
 * @deprecated Use {@link getManifestation}. One bit cannot hold a seven-name
 * list: this is true for `window` and for `canvasExpanded`, and false for
 * `panel`, `workspace` and a dot alike — so it can say what a glyph is not far
 * better than what it is.
 *
 * It reads the same store {@link setManifestation} writes, so it stays correct
 * about what it could ever say.
 */
export function isInWindowState(element: HTMLElement): boolean {
    const m = getManifestation(element);
    return m === 'window' || m === 'canvasExpanded';
}

/**
 * @deprecated Use {@link setManifestation}, which takes the name instead of a
 * bit. `false` here meant "not a window" and left three different destinations
 * — dot, canvasPlaced, workspace — indistinguishable at the far end of a morph.
 */
export function setWindowState(element: HTMLElement, isWindow: boolean): void {
    if (isWindow) {
        setManifestation(element, 'window');
    } else {
        delete element.dataset.manifestation;
    }
}

/**
 * Get last saved position of window
 */
export function getLastPosition(element: HTMLElement): { x: number, y: number } | null {
    const x = parseFloat(element.dataset.lastX ?? '');
    const y = parseFloat(element.dataset.lastY ?? '');
    return isNaN(x) || isNaN(y) ? null : { x, y };
}

/**
 * Save window position for next restore
 */
export function setLastPosition(element: HTMLElement, x: number, y: number): void {
    element.dataset.lastX = String(x);
    element.dataset.lastY = String(y);
}

/**
 * Check if glyph has proximity text showing
 */
export function hasProximityText(element: HTMLElement): boolean {
    return element.dataset.hasText === 'true';
}

/**
 * Set proximity text visibility flag
 */
export function setProximityText(element: HTMLElement, hasText: boolean): void {
    if (hasText) {
        element.dataset.hasText = 'true';
    } else {
        delete element.dataset.hasText;
    }
}

/**
 * Get glyph ID from element
 */
export function getGlyphId(element: HTMLElement): string | null {
    return element.getAttribute('data-glyph-id');
}

/**
 * Set glyph ID on element
 */
export function setGlyphId(element: HTMLElement, id: string): void {
    element.setAttribute('data-glyph-id', id);
}

/**
 * Store canvas-placed origin coordinates for morph return
 * Coordinates are canvas-local (not screen) — use canvasToScreen() at morph time
 */
export function setCanvasOrigin(
    element: HTMLElement,
    origin: { x: number; y: number; width: number; height: number; canvasId: string }
): void {
    element.dataset.canvasOriginX = String(origin.x);
    element.dataset.canvasOriginY = String(origin.y);
    element.dataset.canvasOriginW = String(origin.width);
    element.dataset.canvasOriginH = String(origin.height);
    element.dataset.canvasOriginId = origin.canvasId;
}

/**
 * Get canvas-placed origin coordinates for morph return
 */
export function getCanvasOrigin(
    element: HTMLElement
): { x: number; y: number; width: number; height: number; canvasId: string } | null {
    const x = parseFloat(element.dataset.canvasOriginX ?? '');
    const y = parseFloat(element.dataset.canvasOriginY ?? '');
    const w = parseFloat(element.dataset.canvasOriginW ?? '');
    const h = parseFloat(element.dataset.canvasOriginH ?? '');
    const canvasId = element.dataset.canvasOriginId;
    if (isNaN(x) || isNaN(y) || isNaN(w) || isNaN(h) || !canvasId) return null;
    return { x, y, width: w, height: h, canvasId };
}

/**
 * Clear canvas-placed origin coordinates
 */
export function clearCanvasOrigin(element: HTMLElement): void {
    delete element.dataset.canvasOriginX;
    delete element.dataset.canvasOriginY;
    delete element.dataset.canvasOriginW;
    delete element.dataset.canvasOriginH;
    delete element.dataset.canvasOriginId;
}

/**
 * Get glyph symbol from element
 */
export function getGlyphSymbol(element: HTMLElement): string | undefined {
    return element.dataset.glyphSymbol;
}

/**
 * Set glyph symbol on element
 */
export function setGlyphSymbol(element: HTMLElement, symbol: string | undefined): void {
    if (symbol !== undefined) {
        element.dataset.glyphSymbol = symbol;
    } else {
        delete element.dataset.glyphSymbol;
    }
}
