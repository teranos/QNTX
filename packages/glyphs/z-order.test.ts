/**
 * Stacking order.
 *
 * Personas:
 * - Tim: a window comes forward when it is touched
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { raise, raiseOnInteract, currentTop, resetZOrder } from './z-order';

beforeEach(() => {
    resetZOrder();
    document.body.innerHTML = '';
});

function windowEl(): HTMLElement {
    const el = document.createElement('div');
    el.dataset.glyphId = `w-${currentTop()}`;
    document.body.appendChild(el);
    return el;
}

const z = (el: HTMLElement) => Number(el.style.zIndex);

describe('Tim: stacking', () => {
    // "why doesnt clicking on a glyph move it up its z compared to the other
    //  ones on screen?"
    test('a raised window sits above one raised before it', () => {
        const first = windowEl();
        const second = windowEl();

        raise(first);
        raise(second);

        expect(z(second)).toBeGreaterThan(z(first));
    });

    // "why doesnt clicking on a glyph move it up its z compared to the other
    //  ones on screen?" — the buried one is the case that was broken.
    test('raising the one underneath puts it in front', () => {
        const under = windowEl();
        const over = windowEl();

        raise(under);
        raise(over);
        expect(z(over)).toBeGreaterThan(z(under));

        raise(under);
        expect(z(under)).toBeGreaterThan(z(over));
    });

    // Same quote, through the press rather than a direct call — clicking is
    // what the sentence is about.
    test('a press raises it', () => {
        const first = windowEl();
        const second = windowEl();
        raise(first);
        raise(second);

        raiseOnInteract(first);
        // The realm's own Event: test-setup puts happy-dom's document on the
        // global but not its Event, and dispatch ignores a foreign one.
        const Ev = (globalThis.window as unknown as { Event: typeof Event }).Event;
        first.dispatchEvent(new Ev('mousedown', { bubbles: true }));

        expect(z(first)).toBeGreaterThan(z(second));
    });

    // Hosts style the morph class !important, which a plain inline value
    // loses to. Written !important so the raise survives that.
    // JSDOM drops the priority on the way in — getPropertyPriority, cssText
    // and the style attribute all come back without it.
    test.skipIf(process.env.USE_JSDOM === '1')('the z-index is written important', () => {
        const el = windowEl();
        raise(el);
        expect(el.style.getPropertyPriority('z-index')).toBe('important');
    });
});
