/**
 * Canvas ↔ window morph — glyph-owned styles survive the round trip.
 *
 * The window state suppresses inline styles that bleed through (a note's
 * border, minHeight); the restore paths give them back. Everything about a
 * glyph survives every transition (Element Axioma).
 *
 * Personas:
 * - Tim: happy path — suppress, restore, the border is back
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

describe('Tim: the border survives the window round trip', () => {
    // A note writes its border inline; expanding to a window and placing it
    // back must not strip the post-it look for good.
    test('an inline border is suppressed in window state and restored after', () => {
        element.style.border = '1px solid red';
        element.style.minHeight = '120px';

        suppressGlyphStyles(element);
        expect(element.style.border).toBe('');
        expect(element.style.minHeight).toBe('');

        restoreGlyphStyles(element);
        expect(element.style.border).toBe('1px solid red');
        expect(element.style.minHeight).toBe('120px');
    });
});

describe('Spike: nothing inline stays nothing', () => {
    test('a glyph with no inline border round-trips clean', () => {
        suppressGlyphStyles(element);
        restoreGlyphStyles(element);
        expect(element.style.border).toBe('');
        expect(element.style.minHeight).toBe('');
    });

    test('restore without suppress changes nothing', () => {
        element.style.border = '1px solid red';
        restoreGlyphStyles(element);
        expect(element.style.border).toBe('1px solid red');
    });
});
