/**
 * DOM Content Stash — preserves glyph identity across manifestation morphs.
 *
 * Instead of destroying children with innerHTML = '', manifestations
 * stash them off-DOM in a DocumentFragment. On next maximize, the
 * stashed content is restored — same DOM nodes, same event handlers,
 * same scroll positions.
 *
 * WeakMap: stash is GC'd when the element is GC'd (page refresh, glyph close).
 */

import { removeWindowControls } from './title-bar-controls';

const stash = new WeakMap<HTMLElement, DocumentFragment>();

/**
 * Stash all children off-DOM, stripping manifestation chrome first.
 * After this call the element is empty (ready for tray-dot state).
 */
export function stashContent(element: HTMLElement): void {
    // 1. Disconnect ResizeObserver if present
    const resizeObserver = (element as any).__resizeObserver;
    if (resizeObserver && typeof resizeObserver.disconnect === 'function') {
        resizeObserver.disconnect();
        delete (element as any).__resizeObserver;
    }

    // 2. Strip manifestation-added window controls from any title bar
    const titleBar = element.querySelector('.glyph-title-bar') as HTMLElement | null;
    if (titleBar) {
        removeWindowControls(titleBar);
    }

    // 3. Move all children into a DocumentFragment
    const fragment = document.createDocumentFragment();
    while (element.firstChild) {
        fragment.appendChild(element.firstChild);
    }

    // 4. Store
    stash.set(element, fragment);
}

/**
 * Restore stashed children into the element.
 * Returns true if stash existed and was restored, false otherwise.
 */
export function restoreContent(element: HTMLElement): boolean {
    const fragment = stash.get(element);
    if (!fragment) return false;

    element.appendChild(fragment); // DocumentFragment empties itself on append
    stash.delete(element);
    return true;
}

/** Check whether a stash exists for this element. */
export function hasStash(element: HTMLElement): boolean {
    return stash.has(element);
}
