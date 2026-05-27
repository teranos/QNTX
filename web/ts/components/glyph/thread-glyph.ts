/**
 * Thread Glyph (〽) — end marker for a navigational thread (spine).
 *
 * The needle of the thread — conceptually a cursor manifestation that gets
 * pinned to the canvas at drop, then unpinned at pickup. ONE element across
 * the lifecycle (see Glyph Axiom in web/CLAUDE.md).
 *
 * Not draggable — pickup is via left-click, which resumes thread building
 * from the existing spine. Invisible by default; reveals on cursor proximity.
 */

import type { Glyph } from '@qntx/glyphs';
import { applyCanvasGlyphLayout, commitCursorPlacement, storeCleanup } from '@qntx/glyphs';
import { log, SEG } from '../../logger';

/** Pixel radius around 〽 within which it reveals on cursor approach */
const REVEAL_RADIUS = 80;

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

/** Create a Thread glyph for canvas placement */
export function createThreadGlyph(glyph: Glyph): HTMLElement {
    const color = glyph.color ?? THREAD_COLORS[0];

    // Reuse cursor element if handed off from thread building mode (preserves DOM identity)
    const element = glyph.cursorElement ?? document.createElement('div');
    if (glyph.cursorElement) {
        commitCursorPlacement(element); // strips position:fixed, pointer-events:none, z-index, glyph-cursor class
    }

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

    // Reuse the cursor's symbol span if handed off; else create one
    let sym = glyph.symbolElement ?? null;
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

    // Invisible by default — reveal only when cursor is near (signals pick-up affordance)
    element.style.opacity = '0';
    element.style.transition = 'opacity 150ms ease';
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
    });

    log.debug(SEG.GLYPH, `[ThreadGlyph] Created ${glyph.id} with color ${color}`);
    return element;
}
