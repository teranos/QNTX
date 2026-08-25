/**
 * Stacking order for manifested glyphs.
 *
 * prepareMorphTo writes one z-index for every window, so which window sits on
 * top is DOM order and clicking cannot change it. This hands out an
 * increasing value instead, so the last one touched is the one in front.
 */

// Above the base every manifestation is given, below the tray at 100002.
const BASE = 1000;

let top = BASE;

/**
 * Bring an element to the front.
 *
 * A plain inline value is enough: the morph class leaves the element at morph
 * commit, so no host rule outranks this.
 */
export function raise(element: HTMLElement): void {
    element.style.zIndex = String(++top);
}

/** Raise on press, before anything else reads the stack. */
export function raiseOnInteract(element: HTMLElement): void {
    element.addEventListener('mousedown', () => raise(element), { capture: true });
    element.addEventListener('touchstart', () => raise(element), { capture: true, passive: true });
}

/** The value the next raise will use. Exposed for tests. */
export function currentTop(): number {
    return top;
}

/** Back to the base. Tests only — nothing in the runtime resets the stack. */
export function resetZOrder(): void {
    top = BASE;
}
