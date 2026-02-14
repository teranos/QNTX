/**
 * Type-safe dataset attribute helpers for glyph DOM elements
 *
 * These helpers ensure dataset attributes are accessed and modified
 * with proper type safety and validation.
 */

/**
 * Check if glyph is in window state
 */
export function isInWindowState(element: HTMLElement): boolean {
    return element.dataset.windowState === 'true';
}

/**
 * Set glyph window state
 */
export function setWindowState(element: HTMLElement, isWindow: boolean): void {
    if (isWindow) {
        element.dataset.windowState = 'true';
    } else {
        delete element.dataset.windowState;
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