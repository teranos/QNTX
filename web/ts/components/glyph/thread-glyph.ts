/**
 * Thread Glyph (〽) — end marker for a navigational thread (spine).
 *
 * Placed on the canvas to finish building a thread. Pick it up to extend.
 * Small, unobtrusive marker — just the 〽 symbol in thread color.
 */

import type { Glyph } from '@qntx/glyphs';
import { canvasPlaced } from '@qntx/glyphs';
import { log, SEG } from '../../logger';

/** Thread palette — first thread is always red */
const THREAD_COLORS = [
    '#c45454', // red
    '#5478a8', // blue
    '#6aaa5a', // green
    '#c4944a', // amber
    '#8a5aaa', // purple
    '#5aaa98', // teal
    '#c46a8a', // rose
    '#aa8a5a', // bronze
];

/** Get the color for the Nth thread (0-indexed) */
export function getThreadColor(index: number): string {
    return THREAD_COLORS[index % THREAD_COLORS.length];
}

/** Create a Thread glyph for canvas placement */
export function createThreadGlyph(glyph: Glyph): HTMLElement {
    const color = glyph.color ?? THREAD_COLORS[0];

    const { element } = canvasPlaced({
        glyph,
        className: 'canvas-thread-glyph',
        defaults: { x: 200, y: 200, width: 28, height: 28 },
        logLabel: 'ThreadGlyph',
    });

    element.style.backgroundColor = 'transparent';
    element.style.border = 'none';
    element.style.outline = 'none';
    element.style.backdropFilter = 'none';
    element.style.boxShadow = 'none';
    element.style.display = 'flex';
    element.style.alignItems = 'center';
    element.style.justifyContent = 'center';

    const sym = document.createElement('span');
    sym.className = 'glyph-symbol';
    sym.textContent = '\u303D';
    sym.style.fontSize = '20px';
    sym.style.color = color;
    element.appendChild(sym);

    log.debug(SEG.GLYPH, `[ThreadGlyph] Created ${glyph.id} with color ${color}`);
    return element;
}
