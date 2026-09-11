/**
 * Paint survives a change of manifestation because it is on the element.
 *
 * Element Axioma: a glyph is exactly one DOM element for its entire lifetime,
 * and everything about it survives every transition. Inline styles are wiped on
 * a morph because geometry is the manifestation's — a tray dot is not laid out
 * like a canvas frame. Paint is not the manifestation's, so wiping it and then
 * re-reading it from the Glyph datum makes the datum authoritative over the
 * element, which is the thing the axiom denies. A glyph whose datum names no
 * colour used to lose what it was painted and come back as the default.
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { glyphRun } from './run';
import { canvasPlaced } from './manifestations/canvas-placed';
import { resetGlyphElement } from './manifestations/morphology';
import { readPaint, wearPaint } from './paint';
import { DEFAULT_GLYPH_COLOR } from './glyph';
import type { Glyph } from './glyph';

const PAINTED = 'rgb(5, 16, 11)';

function glyph(id: string, overrides: Partial<Glyph> = {}): Glyph {
    return {
        id,
        title: 'CRIER',
        renderContent: () => document.createElement('div'),
        ...overrides,
    };
}

beforeEach(() => {
    document.body.innerHTML = '';
});

describe('paint across a wipe', () => {
    test('what the element wears is what it keeps', () => {
        const el = document.createElement('div');
        el.style.backgroundColor = PAINTED;

        const was = readPaint(el);
        el.style.cssText = '';
        wearPaint(el, was);

        expect(el.style.backgroundColor).toBe(PAINTED);
    });

    test('a datum naming a colour is asking to change it', () => {
        const el = document.createElement('div');
        el.style.backgroundColor = PAINTED;

        const was = readPaint(el);
        el.style.cssText = '';
        wearPaint(el, was, { color: 'rgb(1, 2, 3)' });

        expect(el.style.backgroundColor).toBe('rgb(1, 2, 3)');
    });

    test('an element that was never painted gets the default', () => {
        const el = document.createElement('div');

        wearPaint(el, readPaint(el));

        expect(el.style.backgroundColor).toBe(DEFAULT_GLYPH_COLOR);
    });
});

describe('a glyph whose datum names no colour', () => {
    test('keeps its paint when the tray adopts it', () => {
        const el = document.createElement('div');
        el.style.backgroundColor = PAINTED;
        document.body.appendChild(el);

        glyphRun.adopt(el, glyph('paint-adopt-1'));

        expect(el.style.backgroundColor).toBe(PAINTED);
        glyphRun.remove('paint-adopt-1');
    });

    test('keeps its paint through the minimize reset', () => {
        const item = glyph('paint-reset-1');
        const { element } = canvasPlaced({
            glyph: item,
            className: 'canvas-glyph glyph-crier',
            defaults: { x: 0, y: 0, width: 400, height: 300 },
            logLabel: 'CRIER',
        });
        element.style.backgroundColor = PAINTED;
        document.body.appendChild(element);

        let handed: HTMLElement | null = null;
        resetGlyphElement(element, item, 'CRIER', (el) => { handed = el; });

        expect(handed).not.toBeNull();
        expect(element.style.backgroundColor).toBe(PAINTED);
    });

    test('a canvas frame is painted, so there is something to keep', () => {
        const { element } = canvasPlaced({
            glyph: glyph('paint-frame-1'),
            className: 'canvas-glyph glyph-crier',
            defaults: { x: 0, y: 0, width: 400, height: 300 },
            logLabel: 'CRIER',
        });

        expect(element.style.backgroundColor).toBe(DEFAULT_GLYPH_COLOR);
    });
});
