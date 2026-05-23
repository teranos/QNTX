/**
 * Cursor manifestation — transient glyph preview during placement mode.
 *
 * A small glyph element that follows the mouse pointer while the user
 * carries a glyph type from the spawn menu to a canvas position.
 * Unlike window/canvas/panel, cursor glyphs are not persisted, have no
 * chrome, and do not participate in the tray morph lifecycle.
 *
 * The cursor element can be reused as the placed canvas glyph's container
 * via canvasPlaced({ element }) to preserve DOM identity.
 */

const CURSOR_CLASS = 'glyph-cursor';

/**
 * Create a cursor glyph element displaying the given symbol.
 * The element is positioned fixed and ignores pointer events.
 */
export function createCursorElement(symbol: string, glyphType: string): HTMLElement {
    const el = document.createElement('div');
    el.className = CURSOR_CLASS;
    el.textContent = symbol;
    el.setAttribute('data-glyph-type', glyphType);
    el.style.position = 'fixed';
    el.style.pointerEvents = 'none';
    el.style.zIndex = '10003';
    return el;
}

/**
 * Attach the cursor element to mouse movement.
 * Returns a cleanup function that removes the listener.
 */
export function attachCursorToMouse(element: HTMLElement): () => void {
    const onMouseMove = (e: MouseEvent) => {
        element.style.left = `${e.clientX}px`;
        element.style.top = `${e.clientY}px`;
    };
    document.addEventListener('mousemove', onMouseMove);
    return () => document.removeEventListener('mousemove', onMouseMove);
}

/**
 * Strip cursor-specific styles and classes so the element can be
 * adopted by canvasPlaced() as a canvas glyph container.
 */
export function prepareCursorForPlacement(element: HTMLElement): void {
    element.classList.remove(CURSOR_CLASS);
    element.textContent = '';
    element.style.position = '';
    element.style.pointerEvents = '';
    element.style.zIndex = '';
    element.style.left = '';
    element.style.top = '';
    element.removeAttribute('data-glyph-type');
}
