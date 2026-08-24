/**
 * Canvas ↔ window morph — what the glyph wrote on itself is the glyph.
 *
 * A note keeps its background color when it becomes a window; the border is
 * the same: inherently part of the DOM element, never touched by the morph.
 * Only minHeight is suspended — the window owns its box — and given back on
 * return (Element Axioma: everything about a glyph survives every transition).
 *
 * Personas:
 * - Tim: happy path — the border rides through the window state untouched
 * - Spike: a glyph with nothing inline round-trips to nothing inline
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { suppressGlyphStyles, restoreGlyphStyles } from './canvas-window';

let element: HTMLElement;

beforeEach(() => {
    document.body.innerHTML = '';
    element = document.createElement('div');
    document.body.appendChild(element);
});

describe('Tim: the border is inherently part of the element', () => {
    // "Look at how the note glyph that is canvas placed, retains it’s
    //  background color. I want the same for border, not suppressed but
    //  inherently part of the DOM element"
    test('the window state never touches an inline border', () => {
        element.style.border = '1px solid red';
        element.style.backgroundColor = 'rgb(212, 197, 154)';

        suppressGlyphStyles(element);
        expect(element.style.border).toBe('1px solid red');
        expect(element.style.backgroundColor).toBe('rgb(212, 197, 154)');

        restoreGlyphStyles(element);
        expect(element.style.border).toBe('1px solid red');
        expect(element.style.backgroundColor).toBe('rgb(212, 197, 154)');
    });

    // The window owns its box: a canvas minHeight would override the
    // window's explicit height, so that one is suspended and given back.
    test('minHeight is suspended in window state and restored after', () => {
        element.style.minHeight = '120px';

        suppressGlyphStyles(element);
        expect(element.style.minHeight).toBe('');

        restoreGlyphStyles(element);
        expect(element.style.minHeight).toBe('120px');
    });
});

describe('Spike: nothing inline stays nothing', () => {
    test('a glyph with no inline styles round-trips clean', () => {
        suppressGlyphStyles(element);
        restoreGlyphStyles(element);
        expect(element.style.border).toBe('');
        expect(element.style.minHeight).toBe('');
    });

    test('restore without suppress changes nothing', () => {
        element.style.minHeight = '80px';
        restoreGlyphStyles(element);
        expect(element.style.minHeight).toBe('80px');
    });
});
