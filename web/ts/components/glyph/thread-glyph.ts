/**
 * Thread Glyph (〽) — the needle of a navigational thread.
 *
 * The needle is a cursor manifestation that gets pinned to the canvas at
 * drop, then unpinned at pickup, then pinned again at next drop. ONE DOM
 * element across the entire lifecycle (Glyph Axiom — web/CLAUDE.md).
 *
 * - Build mode creates the cursor (createCursorElement) → first drop pins it.
 * - Pickup unpins the placed element → it becomes the cursor again.
 * - Next drop pins it back to the new position.
 *
 * The element you see following the mouse during build/extend is literally
 * the same DOM node that lands on canvas. No new element is ever created
 * to represent the same needle.
 *
 * Not draggable — pickup is via left-click. Invisible by default; reveals
 * on cursor proximity (signals pick-up affordance).
 */

import type { Glyph } from '@qntx/glyphs';
import { applyCanvasGlyphLayout, commitCursorPlacement, storeCleanup } from '@qntx/glyphs';
import { log, SEG } from '../../logger';

/** Pixel radius around 〽 within which it reveals on cursor approach */
const REVEAL_RADIUS = 80;

/** Cursor z-index when 〽 is unpinned and following the mouse */
const CURSOR_Z_INDEX = '10003';

/** Thread palette — red hues only */
const THREAD_COLORS = [
    '#c45454', // crimson
    '#a83232', // dark red
    '#d46a6a', // salmon
    '#8b1a1a', // maroon
    '#cc4444', // bright red
    '#b24a4a', // brick
    '#d45050', // vermillion
    '#943838', // wine
];

/** Get the color for the Nth thread (0-indexed) */
export function getThreadColor(index: number): string {
    return THREAD_COLORS[index % THREAD_COLORS.length];
}

/** Elements that already have the proximity-reveal listener attached. */
const proximityWired = new WeakSet<HTMLElement>();

/** Attach the cursor-proximity reveal listener exactly once per element. */
function wireProximityReveal(element: HTMLElement): void {
    if (proximityWired.has(element)) return;
    proximityWired.add(element);
    const onCursorMove = (e: MouseEvent) => {
        const rect = element.getBoundingClientRect();
        const cx = rect.left + rect.width / 2;
        const cy = rect.top + rect.height / 2;
        const dx = e.clientX - cx;
        const dy = e.clientY - cy;
        element.style.opacity = Math.hypot(dx, dy) < REVEAL_RADIUS ? '1' : '0';
    };
    document.addEventListener('mousemove', onCursorMove);
    storeCleanup(element, () => {
        document.removeEventListener('mousemove', onCursorMove);
        proximityWired.delete(element);
    });
}

/**
 * Apply the placed-thread-glyph state to `element`. Mutates in place — the
 * same DOM node now represents a pinned 〽 instead of a cursor (or freshly
 * created div).
 */
function applyPlacedState(element: HTMLElement, glyph: Glyph): void {
    const color = glyph.color ?? THREAD_COLORS[0];

    // Strip cursor-mode styles if this element was previously a cursor
    commitCursorPlacement(element);

    element.className = 'canvas-thread-glyph canvas-glyph';
    element.dataset.glyphId = glyph.id;
    if (glyph.symbol) element.dataset.glyphSymbol = glyph.symbol;
    applyCanvasGlyphLayout(element, {
        x: glyph.x ?? 200,
        y: glyph.y ?? 200,
        width: glyph.width ?? 28,
        height: glyph.height ?? 28,
    });
    element.style.backgroundColor = 'transparent';
    element.style.border = 'none';
    element.style.outline = 'none';
    element.style.backdropFilter = 'none';
    element.style.boxShadow = 'none';
    element.style.display = 'flex';
    element.style.alignItems = 'center';
    element.style.justifyContent = 'center';
    element.style.opacity = '0';
    element.style.transition = 'opacity 150ms ease';

    // Reuse existing symbol span (whether '.glyph-cursor-symbol' from cursor
    // mode or '.glyph-symbol' from prior placed state); otherwise create one.
    let sym: HTMLElement | null =
        glyph.symbolElement
        ?? element.querySelector('.glyph-cursor-symbol')
        ?? element.querySelector('.glyph-symbol');
    if (sym) {
        sym.classList.remove('glyph-cursor-symbol');
        sym.classList.add('glyph-symbol');
        if (sym.parentElement !== element) element.appendChild(sym);
    } else {
        sym = document.createElement('span');
        sym.className = 'glyph-symbol';
        sym.textContent = '〽';
        element.appendChild(sym);
    }
    sym.style.fontSize = '20px';
    sym.style.color = color;
}

/**
 * Pin the needle to the canvas at (x, y). The element passed in becomes —
 * remains — the placed 〽. No new element is created.
 */
export function pinThreadGlyph(element: HTMLElement, canvas: HTMLElement, x: number, y: number, color: string, glyphId: string): void {
    if (element.parentElement !== canvas) canvas.appendChild(element);
    applyPlacedState(element, { id: glyphId, title: 'Thread', symbol: '〽', x, y, color, renderContent: () => element });
    wireProximityReveal(element);
}

/**
 * Unpin the needle from the canvas — same element, now in cursor mode and
 * parented to document.body so it can follow the mouse during build mode.
 */
export function unpinThreadGlyph(element: HTMLElement): void {
    if (element.parentElement !== document.body) document.body.appendChild(element);
    element.className = 'glyph-cursor';
    element.style.position = 'fixed';
    element.style.pointerEvents = 'none';
    element.style.zIndex = CURSOR_Z_INDEX;
    element.style.opacity = '';
    element.style.transition = '';
    const sym = element.querySelector('.glyph-symbol') as HTMLElement | null;
    if (sym) {
        sym.classList.remove('glyph-symbol');
        sym.classList.add('glyph-cursor-symbol');
    }
}

/**
 * Initial creation of a thread glyph for canvas placement.
 *
 * If `glyph.cursorElement` is provided (the cursor handed off from build
 * mode), it is reused — preserving DOM identity. Otherwise a new div is
 * created (e.g., during canvas restore from persistence).
 */
export function createThreadGlyph(glyph: Glyph): HTMLElement {
    const element = glyph.cursorElement ?? document.createElement('div');
    applyPlacedState(element, glyph);
    wireProximityReveal(element);
    log.debug(SEG.GLYPH, `[ThreadGlyph] Created ${glyph.id} with color ${glyph.color ?? THREAD_COLORS[0]}`);
    return element;
}
