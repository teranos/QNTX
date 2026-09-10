/**
 * The lift off the canvas is the canvas's affordance, not each glyph's.
 *
 * The result glyph carries a ⬆ that takes it off the canvas and makes it a
 * window; so does the wrapper the loader puts around a bare element. A glyph
 * module that builds its own frame through ui.glyph() was the one path that
 * got neither, so it sat on the canvas with no way off it.
 */

import { describe, test, expect } from 'bun:test';
import { createGlyphUI } from './glyph-ui';
import type { Glyph } from '@qntx/glyphs';

function placed(): Glyph {
    return {
        id: 'lift-1',
        title: 'Test',
        symbol: '\u{1F4EF}',
        renderContent: () => document.createElement('div'),
    };
}

const defaults = { x: 10, y: 20, width: 400, height: 300 };

describe('ui.glyph', () => {
    test('a glyph with a title bar gets the lift', () => {
        const ui = createGlyphUI(placed(), 'crier');
        const { titleBar } = ui.glyph({ defaults, titleBar: { label: 'CRIER' }, resizable: true });

        const lift = titleBar?.querySelector('button[aria-label="Expand to window"]');
        expect(lift).not.toBeNull();
    });

    test('the actions a glyph asked for are kept, with the lift after them', () => {
        const mine = document.createElement('button');
        mine.setAttribute('aria-label', 'Mine');

        const ui = createGlyphUI(placed(), 'crier');
        const { titleBar } = ui.glyph({
            defaults,
            titleBar: { label: 'CRIER', actions: [mine] },
        });

        const buttons = Array.from(titleBar?.querySelectorAll('button') ?? []);
        expect(buttons.map(b => b.getAttribute('aria-label'))).toEqual(['Mine', 'Expand to window']);
    });

    test('a glyph that says lift:false gets none', () => {
        const ui = createGlyphUI(placed(), 'crier');
        const { titleBar } = ui.glyph({ defaults, titleBar: { label: 'CRIER' }, lift: false });

        expect(titleBar?.querySelector('button[aria-label="Expand to window"]')).toBeNull();
    });

    test('a glyph with no title bar has nowhere to put one', () => {
        const ui = createGlyphUI(placed(), 'crier');
        const { element } = ui.glyph({ defaults });

        expect(element.querySelector('button[aria-label="Expand to window"]')).toBeNull();
    });

    // The frame paints a background so a placed glyph is a solid thing on the
    // canvas rather than a border with the workspace showing through.
    test('a placed glyph is given a background', () => {
        const ui = createGlyphUI(placed(), 'crier');
        const { element } = ui.glyph({ defaults, titleBar: { label: 'CRIER' } });

        expect(element.style.backgroundColor).not.toBe('');
    });
});
