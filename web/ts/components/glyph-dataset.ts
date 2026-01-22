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