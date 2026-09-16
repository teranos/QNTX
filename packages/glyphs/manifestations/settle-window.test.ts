/**
 * What a window is once its morph has committed.
 *
 * There are two constructors for the one `window` row in MANIFESTATIONS —
 * a tray dot opening (window.ts) and a canvas glyph being lifted
 * (canvas-window.ts) — and they drifted: 520x420 against a content-sized box,
 * a base z-index that never rose against one that did, a cap on one and none
 * on the other. The table names the state; nothing said what the state is.
 * This is what says it, and both constructors call it.
 *
 * Personas:
 * - Tim: Happy path — the box, the cap, the look
 * - Spike: Edge cases — a content-owned axis, and what a drag reflows from
 * - Jenny: Complex scenarios — stacking through a press, and the way back
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { settleWindow } from './settle-window';
import { WINDOW_STYLE_PROPS } from './canvas-window';
import { resetZOrder } from '../z-order';
import { MAX_VIEWPORT_WIDTH_RATIO, MAX_VIEWPORT_HEIGHT_RATIO } from '../glyph';

const BOX = { x: 40, y: 60, width: 520, height: 420 };

beforeEach(() => {
    resetZOrder();
    document.body.innerHTML = '';
});

function windowEl(): HTMLElement {
    const el = document.createElement('div');
    el.dataset.glyphId = 'tokens-glyph';
    document.body.appendChild(el);
    return el;
}

const z = (el: HTMLElement) => Number(el.style.zIndex);

/** `max-width` → `maxWidth`. No regex (CLAUDE.md); the dash is the seam. */
function camel(property: string): string {
    const parts = property.split('-');
    return parts[0] + parts.slice(1).map(p => p.charAt(0).toUpperCase() + p.slice(1)).join('');
}

describe('Tim: what a settled window is', () => {
    test('sits where the morph left it', () => {
        const el = windowEl();
        settleWindow(el, BOX);

        expect(el.style.position).toBe('fixed');
        expect(el.style.left).toBe('40px');
        expect(el.style.top).toBe('60px');
        expect(el.style.width).toBe('520px');
        expect(el.style.height).toBe('420px');
    });

    test('answers to the screen it landed on', () => {
        const el = windowEl();
        settleWindow(el, BOX);

        expect(el.style.maxWidth).toBe(`${Math.floor(window.innerWidth * MAX_VIEWPORT_WIDTH_RATIO)}px`);
        expect(el.style.maxHeight).toBe(`${Math.floor(window.innerHeight * MAX_VIEWPORT_HEIGHT_RATIO)}px`);
    });

    test('holds its body: a column that clips', () => {
        const el = windowEl();
        settleWindow(el, BOX);

        expect(el.style.display).toBe('flex');
        expect(el.style.flexDirection).toBe('column');
        expect(el.style.overflow).toBe('hidden');
        expect(el.style.borderRadius).not.toBe('');
        expect(el.style.boxShadow).not.toBe('');
    });

    test('comes to the front on arrival', () => {
        const under = windowEl();
        const over = windowEl();

        settleWindow(under, BOX);
        settleWindow(over, BOX);

        expect(z(over)).toBeGreaterThan(z(under));
    });
});

describe('Spike: an axis the content owns', () => {
    test('writes the style the caller asked for instead of the measured box', () => {
        const el = windowEl();
        settleWindow(el, { ...BOX, widthStyle: 'fit-content', heightStyle: 'fit-content' });

        expect(el.style.width).toBe('fit-content');
        expect(el.style.height).toBe('fit-content');
    });

    test('a drag still reflows from the measured width, not from fit-content', () => {
        const el = windowEl();
        settleWindow(el, { ...BOX, widthStyle: 'fit-content' });

        // setNaturalWidth's key — what window-drag asks with every frame.
        expect((el as unknown as Record<string, number>).__glyphNaturalWidth).toBe(520);
    });
});

describe('Jenny: stacking, and the way back', () => {
    // "why doesnt clicking on a glyph move it up its z compared to the other
    //  ones on screen?" — answered for the tray dot's window and not for the
    //  canvas glyph's, because only one of the two constructors raised.
    test('a press raises a settled window above one settled after it', () => {
        const first = windowEl();
        const second = windowEl();
        settleWindow(first, BOX);
        settleWindow(second, BOX);

        const Ev = (globalThis.window as unknown as { Event: typeof Event }).Event;
        first.dispatchEvent(new Ev('mousedown', { bubbles: true }));

        expect(z(first)).toBeGreaterThan(z(second));
    });

    test('settling twice presses once — a reopened glyph is one element, not two listeners', () => {
        const el = windowEl();
        settleWindow(el, BOX);
        settleWindow(el, BOX);

        const before = z(el);
        const Ev = (globalThis.window as unknown as { Event: typeof Event }).Event;
        el.dispatchEvent(new Ev('mousedown', { bubbles: true }));

        expect(z(el)).toBe(before + 1);
    });

    // A style the window adds and the canvas never takes back rides home with
    // the glyph and changes what it is on the canvas. The list is the canvas
    // path's; the writing is the settle's; nothing but a test holds the two
    // together.
    test('the way back clears everything the settle wrote', () => {
        const el = windowEl();
        settleWindow(el, BOX);
        expect(el.style.length).toBeGreaterThan(0);

        // Exactly what morphWindowToCanvasPlaced does on restore.
        for (const property of WINDOW_STYLE_PROPS) {
            (el.style as unknown as Record<string, string>)[property as string] = '';
        }

        const left: string[] = [];
        for (let i = 0; i < el.style.length; i++) left.push(camel(el.style.item(i)));
        expect(left).toEqual([]);
    });
});
