/**
 * The lift off the canvas is the canvas's affordance, not each element's.
 *
 * The result element carries a ⬆ that takes it off the canvas and makes it a
 * window; so does the wrapper the loader puts around a bare element. An element
 * module that builds its own frame through ui.element() was the one path that
 * got neither, so it sat on the canvas with no way off it.
 */

import { describe, test, expect } from 'bun:test';
import { createElementUI } from './element-ui';
import type { Element } from '@teranos/elements';

function placed(): Element {
    return {
        id: 'lift-1',
        title: 'Test',
        symbol: '\u{1F4EF}',
        renderContent: () => document.createElement('div'),
    };
}

const defaults = { x: 10, y: 20, width: 400, height: 300 };

describe('ui.element', () => {
    test('an element with a title bar gets the lift', () => {
        const ui = createElementUI(placed(), 'crier');
        const { titleBar } = ui.element({ defaults, titleBar: { label: 'CRIER' }, resizable: true });

        const lift = titleBar?.querySelector('button[aria-label="Expand to window"]');
        expect(lift).not.toBeNull();
    });

    test('the actions an element asked for are kept, with the lift after them', () => {
        const mine = document.createElement('button');
        mine.setAttribute('aria-label', 'Mine');

        const ui = createElementUI(placed(), 'crier');
        const { titleBar } = ui.element({
            defaults,
            titleBar: { label: 'CRIER', actions: [mine] },
        });

        const buttons = Array.from(titleBar?.querySelectorAll('button') ?? []);
        expect(buttons.map(b => b.getAttribute('aria-label'))).toEqual(['Mine', 'Expand to window']);
    });

    test('an element that says lift:false gets none', () => {
        const ui = createElementUI(placed(), 'crier');
        const { titleBar } = ui.element({ defaults, titleBar: { label: 'CRIER' }, lift: false });

        expect(titleBar?.querySelector('button[aria-label="Expand to window"]')).toBeNull();
    });

    test('an element with no title bar has nowhere to put one', () => {
        const ui = createElementUI(placed(), 'crier');
        const { element } = ui.element({ defaults });

        expect(element.querySelector('button[aria-label="Expand to window"]')).toBeNull();
    });

    // The frame paints a background so a placed element is a solid thing on the
    // canvas rather than a border with the workspace showing through.
    test('a placed element is given a background', () => {
        const ui = createElementUI(placed(), 'crier');
        const { element } = ui.element({ defaults, titleBar: { label: 'CRIER' } });

        expect(element.style.backgroundColor).not.toBe('');
    });
});
